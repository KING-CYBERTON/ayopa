Use an IAM Identity Center or admin IAM user with MFA, not the root account.
# Terraform state bucket (pick a unique name)
```
aws s3api create-bucket --bucket ayopa-tfstate-<unique> --region eu-central-1 \
  --create-bucket-configuration LocationConstraint=eu-central-1
aws s3api put-bucket-versioning --bucket ayopa-tfstate-<unique> \
  --versioning-configuration Status=Enabled
aws s3api put-public-access-block --bucket ayopa-tfstate-<unique> \
  --public-access-block-configuration BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
  ```

# Cloudflare token: created outside Terraform so it never lands in state
```
aws ssm put-parameter --name /ayopa/cloudflare_token --type SecureString \
  --value 'NEW-ROLLED-TOKEN' --region eu-central-1
  ```