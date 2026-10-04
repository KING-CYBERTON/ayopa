# Runbook: destroy and rebuild the platform

Proves the environment is reproducible: the whole stack is destroyed, rebuilt
from code and serving again, with no console clicks and no SSH.

## What survives and what is recreated

| Survives | Recreated |
|---|---|
| AWS account, Terraform state bucket | VPC, subnets, security groups |
| SSM parameter `/ayopa/cloudflare_token` | EC2 instance and Elastic IP (**new address**) |
| Cloudflare zone and records | RDS database (**data is lost**) |
| GitHub repo and variables | ECR repositories (**images are deleted**) |
| Source code and Terraform code | Artifacts bucket, IAM roles, OIDC provider, budget, `/ayopa/database_url` |

The role name stays `ayopa-gha-deploy`, so its ARN and the GitHub variables remain valid.

## Pre-flight

- [ ] `main` is green in GitHub Actions and `git status` is clean
- [ ] The data is demo data only (the database is destroyed)
- [ ] Screen recording is ready
- [ ] Rehearsing more than once a week? Use the staging CA to avoid
      Let's Encrypt's duplicate-certificate limit (5 per week for the same names):
      `aws ssm put-parameter --name /ayopa/acme_ca --type String --region eu-central-1 --value https://acme-staging-v02.api.letsencrypt.org/directory`
      Delete it with `aws ssm delete-parameter --name /ayopa/acme_ca --region eu-central-1` for the final, real run.

## Steps

Note the time at each step.

### 1. Destroy

```bash
cd infra
terraform plan -destroy -out=destroy.out
terraform apply destroy.out
```

Read the plan first. It should list only resources Terraform created.

### 2. Rebuild

```bash
terraform plan -out=plan.out
terraform apply plan.out
terraform output public_ip
```

### 3. Point the world at the new IP

- Cloudflare DNS: set the `@` and `*` A records to the new IP (DNS only, grey cloud).
- Cloudflare token: edit the Caddy token and replace the old address in
  **Client IP Address Filtering** with the new one. Without this, Caddy's
  certificate request fails with error 9109.

### 4. Wait until the instance is ready

```bash
ID=$(terraform output -raw instance_id)
until [ "$(aws ssm describe-instance-information --region eu-central-1 --filters Key=InstanceIds,Values=$ID --query 'InstanceInformationList[0].PingStatus' --output text 2>/dev/null)" = "Online" ]; do sleep 10; done; echo online
```

Then wait about two more minutes for `user_data` to finish installing Docker.

### 5. Deploy by pushing a commit

```bash
cd ..
git commit --allow-empty -m "Rebuild demo"
git push
```

The pipeline rebuilds both images (ECR is empty), uploads the deploy files,
migrates the new database and starts the containers. If the deploy step fails
because Docker was not ready yet, use **Re-run failed jobs**.

### 6. Verify

```bash
./scripts/verify.sh ayopa.co.ke <new-ip>
```

Then sign up for a workspace in the browser, log in, and add a note.

## Checklist for the recording
- [ ] `terraform destroy` finishes
- [ ] `terraform apply` finishes, new IP shown
- [ ] DNS and token updated
- [ ] Pipeline green, with the "migrations complete" line in the deploy output
- [ ] `verify.sh` all PASS
- [ ] Signup, login and a note on a fresh subdomain

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Caddy log shows 403 / code 9109 | Token IP filter does not include the new IP |
| TLS alert from curl | Caddy has no certificate yet (usually the token above) |
| Deploy step fails right after apply | SSM agent or Docker not ready, wait and re-run the job |
| Rate-limit error from Let's Encrypt | Use the staging CA parameter, retry later for real certificates |
| `/healthz` returns 503 | App cannot reach RDS, check the `/ayopa/database_url` parameter |

## Optional: keep the IP stable across rebuilds

Allocating the Elastic IP outside Terraform makes steps 3's DNS and token
changes unnecessary.

1. Find and tag the existing address:
   `aws ec2 describe-addresses --region eu-central-1 --query 'Addresses[].[AllocationId,PublicIp]' --output text`
   `aws ec2 create-tags --region eu-central-1 --resources eipalloc-XXXX --tags Key=Name,Value=ayopa-static-ip`
2. Stop Terraform managing it: `terraform -chdir=infra state rm aws_eip.app`
3. In `infra/ec2.tf`, replace the `aws_eip` resource with:

```hcl
data "aws_eip" "static" {
  tags = {
    Name = "ayopa-static-ip"
  }
}

resource "aws_eip_association" "app" {
  instance_id   = aws_instance.app.id
  allocation_id = data.aws_eip.static.id
}
```

4. In `infra/outputs.tf`, set `public_ip` to `data.aws_eip.static.public_ip`.
5. Run `terraform plan` and read it before applying. The address now survives
   `destroy` and costs a small hourly fee while unattached.