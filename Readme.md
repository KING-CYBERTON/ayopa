# Ayopa: multi-tenant subdomain platform

[![pipeline](https://github.com/KING-CYBERTON/ayopa/actions/workflows/pipeline.yml/badge.svg)](https://github.com/KING-CYBERTON/ayopa/actions/workflows/pipeline.yml)

Every customer who registers gets their own subdomain (`alice.ayopa.co.ke`) with
isolated data, its own login and its own HTTPS, with no per-tenant infrastructure.
The whole stack is defined as code and deployed by pushing to `main`.

Built as a capstone to demonstrate system design and operational practice:
infrastructure as code, a secretless CI/CD pipeline, tested tenant isolation,
and documented trade-offs (14 Architecture Decision Records).

<!-- Add screenshots to docs/images/ and link them here:
![Signup](docs/images/signup.png)
![Dashboard](docs/images/dashboard.png)
-->

## Contents

- [Ayopa: multi-tenant subdomain platform](#ayopa-multi-tenant-subdomain-platform)
  - [Contents](#contents)
  - [What it does](#what-it-does)
  - [Architecture](#architecture)
  - [Tech stack](#tech-stack)
  - [Repository layout](#repository-layout)
  - [Design decisions](#design-decisions)
  - [Security](#security)
  - [Testing](#testing)
  - [Local development](#local-development)
  - [Deploy your own copy](#deploy-your-own-copy)
    - [1. Make it yours](#1-make-it-yours)
    - [2. Cloudflare](#2-cloudflare)
    - [3. Bootstrap (the only manual AWS work)](#3-bootstrap-the-only-manual-aws-work)
    - [4. Build the infrastructure](#4-build-the-infrastructure)
    - [5. Point DNS at the server](#5-point-dns-at-the-server)
    - [6. Connect GitHub](#6-connect-github)
    - [7. Ship](#7-ship)
    - [8. Verify](#8-verify)
  - [CI/CD](#cicd)
  - [Operations](#operations)
  - [Production readiness](#production-readiness)
  - [Cost](#cost)
  - [Roadmap](#roadmap)
  - [License](#license)

## What it does

- **Self-service signup.** Choose a subdomain, email and password. The tenant and
  its first user are created in one database transaction.
- **Tenant isolation.** Each tenant's users, sessions and data are separate, even
  when two tenants use the same email address.
- **Per-tenant login and sessions** bound to the tenant's own host.
- **A small workspace feature (notes)** to show tenant-scoped data end to end.
- **Instant provisioning.** Creating a tenant is a database row. Wildcard DNS and
  a wildcard certificate already cover every possible subdomain.

## Architecture

```mermaid
flowchart LR
  U["Browser"] -->|"*.ayopa.co.ke"| DNS["Cloudflare DNS (DNS only)"]
  DNS --> EIP["Elastic IP"]
  subgraph VPC["AWS VPC 10.0.0.0/22, eu-central-1"]
    subgraph PUB["Public subnet"]
      EC2["EC2 (Docker Compose)<br/>Caddy :443 to Go app :8080"]
    end
    subgraph PRIV["Private subnets, 2 AZs, no internet route"]
      RDS[("RDS PostgreSQL 16")]
    end
  end
  EIP --> EC2
  EC2 --> RDS
```

**Request flow**

1. DNS resolves the apex and every subdomain to the server's Elastic IP.
2. Caddy terminates TLS with one wildcard certificate and proxies to the app.
3. The app reads the `Host` header, validates the subdomain, looks the tenant up
   once, and stores it in the request context. Handlers take the tenant from
   there and never from user input.
4. Every tenant query filters on `tenant_id`.

**Secrets and access.** No SSH and no static cloud keys. Shell access uses SSM
Session Manager, deployment uses SSM Run Command, and CI authenticates to AWS with
short-lived OIDC credentials. Secrets live in SSM Parameter Store.

## Tech stack

| Layer | Choice | Why (ADR) |
|---|---|---|
| Language | Go (standard library routing, `log/slog`, `html/template`) | Small, fast, one static binary |
| Reverse proxy and TLS | Caddy with the Cloudflare DNS plugin | Automatic wildcard certificates (0001, 0010) |
| DNS | Cloudflare (free plan, DNS only) | No hosted-zone cost (0002) |
| Compute | AWS EC2 (Amazon Linux 2023) | Cloud skills and a scaling path (0003) |
| Database | Amazon RDS PostgreSQL 16, private subnets | Managed backups, no internet exposure (0004, 0009) |
| Containers | Docker Compose on the instance, images in ECR | Immutable, rollback by tag (0006) |
| IaC | Terraform, S3 remote state with native locking | Reproducible environment (0005) |
| CI/CD | GitHub Actions, OIDC, SSM Run Command | No stored keys, no open SSH port (0007) |
| Secrets | SSM Parameter Store (SecureString) | Free, IAM-scoped (0008) |
| Auth | argon2id, server-side sessions, `__Host-` cookies | Tenant-bound sessions (0014) |
| Migrations | Embedded SQL run by `app migrate` at deploy | Repeatable, aborts bad deploys (0013) |

## Repository layout

```
.
├── cmd/server/            # entrypoint: `server` runs the app, `server migrate` migrates
├── internal/
│   ├── auth/              # argon2id hashing, session tokens
│   ├── db/                # migration runner (advisory lock, per-file transactions)
│   ├── store/             # all SQL; tenant-scoped queries
│   ├── tenant/            # subdomain rules and reserved names
│   └── web/               # routing, tenant resolution, handlers, middleware, templates
├── migrations/            # numbered, forward-only SQL, embedded in the binary
├── deploy/                # Caddyfile, compose.yml, deploy.sh, Caddy Dockerfile
├── infra/                 # Terraform
├── scripts/               # test.sh, verify.sh
├── docs/
│   ├── adr/               # architecture decision records
│   └── runbooks/          # rebuild-demo.md
├── .github/workflows/     # pipeline.yml
├── Dockerfile             # app image (multi-stage, distroless, non-root)
└── docker-compose.dev.yml # throwaway Postgres for tests
```

## Design decisions

Each decision is recorded with the options considered, the trade-offs accepted,
and what would make me revisit it.

| ADR | Title | Status |
|-----|-------|--------|
| [ADR-0001](docs/adr/ADR_001-Caddy.md) | Use Caddy as reverse proxy and TLS terminator | Accepted |
| [ADR-0002](docs/adr/ADR_002-Cloudflare.md) | Use Cloudflare DNS instead of Route 53 | Accepted |
| [ADR-0003](docs/adr/ADR_003-AWS_EC2.md) | Host on AWS EC2 instead of a generic VPS | Accepted |
| [ADR-0004](docs/adr/ADR_004-RDS.md) | Use Amazon RDS for PostgreSQL instead of self-managed Postgres | Accepted |
| [ADR-0005](docs/adr/ADR_005-terraform.md) | Provision infrastructure with Terraform and S3 remote state | Accepted |
| [ADR-0006](docs/adr/ADR_006-docker.md) | Run Caddy and the app as containers with Docker Compose | Accepted |
| [ADR-0007](docs/adr/ADR_007-deploy.md) | Deploy with SSM Run Command and GitHub OIDC | Accepted |
| [ADR-0008](docs/adr/ADR_008-secrets.md) | Store secrets in SSM Parameter Store | Accepted |
| [ADR-0009](docs/adr/ADR_009-public.md) | Public EC2 subnet, private RDS subnets, no NAT gateway | Accepted |
| [ADR-0010](docs/adr/ADR_010-wildcard.md) | Wildcard certificate (DNS-01) instead of on-demand TLS | Accepted |
| [ADR-0011](docs/adr/ADR_011-schema.md) | Shared database schema with a tenant_id column | Accepted |
| [ADR-0012](docs/adr/ADR_012-subdomain.md) | Subdomain naming rules and reserved names | Accepted |
| [ADR-0013](docs/adr/ADR_013_migrations.md) | Subdomain naming rules and reserved names | Accepted |
| [ADR-0014](docs/adr/ADR_014_authentication.md) | Subdomain naming rules and reserved names | Accepted |

## Security

| Threat | Mitigation |
|---|---|
| Cross-tenant data access | Tenant resolved once from the host; every query filtered by `tenant_id`; composite constraints; an integration test proves tenant A cannot read or write tenant B's data |
| Session reuse across tenants | Sessions store `tenant_id` and are accepted only on that tenant's host |
| Cookie tossing from a sibling subdomain | `__Host-session` cookie: `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, no `Domain` |
| Subdomain impersonation or DNS shadowing | Strict format, `xn--` rejected, reserved list covering every name with its own DNS record, enforced in the app and by a database `CHECK` |
| Signup races | Uniqueness enforced by a database constraint, never check-then-insert |
| Credential theft from the database | argon2id passwords; only the SHA-256 of each session token is stored |
| Account enumeration | Same generic error and equal work for unknown emails and wrong passwords |
| Brute force and signup abuse | Per-IP rate limits (5 signups per hour, 10 logins per 15 minutes) |
| CSRF | `Origin` or `Referer` must match the host on every state-changing request, plus `SameSite=Lax` |
| Browser attacks | Strict CSP (no scripts), `nosniff`, `X-Frame-Options: DENY`, HSTS, 1 MiB request cap, server timeouts |
| Leaked deploy credentials | OIDC trust limited to one repository and the `main` branch; no static keys anywhere |
| Leaked DNS token | Scoped to DNS edit on one zone and restricted by source IP |
| Network exposure | No SSH rule; database has no public access and accepts connections only from the app's security group; IMDSv2 required; encrypted volumes and database |

## Testing

```bash
./scripts/test.sh
```

The script starts a throwaway Postgres 16 and runs `go test ./... -race -count=1`
against it. The same tests run in CI against a Postgres service container.

Covered: subdomain validation (including every reserved name), password hashing,
the rate limiter, signup, duplicate subdomains, login and logout, session expiry,
CSRF rejection, unknown hosts, **cross-tenant isolation**, migration idempotence,
and adopting a database that was built by hand.

Database tests refuse to run unless the database name contains `_test`, because
they truncate tables.

## Local development

Requirements: Go, Docker.

```bash
docker compose -f docker-compose.dev.yml up -d --wait
docker compose -f docker-compose.dev.yml exec postgres createdb -U app ayopa_dev

export DATABASE_URL='postgres://app:postgres@localhost:5433/ayopa_dev?sslmode=disable'
export BASE_DOMAIN=localhost PUBLIC_SCHEME=http PUBLIC_PORT=:8080
export COOKIE_SECURE=false ADDR=127.0.0.1:8080

go run ./cmd/server migrate
go run ./cmd/server
```

Open `http://localhost:8080`, sign up, and you are redirected to
`http://<name>.localhost:8080/login`. Browsers resolve `*.localhost` to your own
machine. Use a separate `ayopa_dev` database for manual work, never `ayopa_test`.

## Deploy your own copy

**You need:** an AWS account (use an admin IAM user or SSO role with MFA, never the
root user), a domain whose DNS is on Cloudflare, a GitHub repository, and the AWS
CLI v2, Terraform 1.10 or newer, Docker with buildx, and Go.

### 1. Make it yours

Replace these values (search for `ayopa`):

- `deploy/deploy.sh`: `BASE_DOMAIN` and `REGION`
- `infra/versions.tf`: the state bucket name
- `infra/terraform.tfvars` (git-ignored): `github_repo`, `budget_email`, `budget_usd`
- `infra/variables.tf`: the `github_oidc_subject_prefix` default (step 6)

### 2. Cloudflare

Add your zone and switch the registrar's nameservers to Cloudflare (disable DNSSEC
at the registrar first). Create an API token with **Zone: DNS: Edit** and
**Zone: Zone: Read**, limited to your one zone.

### 3. Bootstrap (the only manual AWS work)

```bash
BUCKET=<unique-name>-tfstate
aws s3api create-bucket --bucket $BUCKET --region eu-central-1 \
  --create-bucket-configuration LocationConstraint=eu-central-1
aws s3api put-bucket-versioning --bucket $BUCKET --versioning-configuration Status=Enabled
aws s3api put-public-access-block --bucket $BUCKET --public-access-block-configuration \
  BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true

aws ssm put-parameter --name /ayopa/cloudflare_token --type SecureString \
  --region eu-central-1 --value '<cloudflare-token>'
```

The token is created outside Terraform so it never lands in Terraform state.

### 4. Build the infrastructure

```bash
cd infra
terraform init
terraform plan -out=plan.out     # read it
terraform apply plan.out
terraform output
```

This creates the VPC, subnets, security groups, EC2, Elastic IP, RDS, ECR, the
artifacts bucket, IAM roles, the GitHub OIDC provider and a budget alert. New AWS
free-plan accounts limit RDS backup retention to 1 day.

### 5. Point DNS at the server

In Cloudflare, create `A @` and `A *` records pointing to the `public_ip` output,
**DNS only (grey cloud)**, so Caddy can terminate TLS itself. If your token has IP
restrictions, add the new address to its allowlist, or certificate issuance fails
with Cloudflare error 9109.

### 6. Connect GitHub

Add three repository variables (Settings > Secrets and variables > Actions >
Variables). None are secrets.

| Variable | Value |
|---|---|
| `AWS_DEPLOY_ROLE_ARN` | the `gha_role_arn` output |
| `ARTIFACT_BUCKET` | the `artifacts_bucket` output |
| `BASE_DOMAIN` | your domain |

The IAM role trusts one exact `sub` claim. GitHub may issue it as
`repo:OWNER/REPO:...` or in an ID-based form such as
`repo:OWNER@<id>/REPO@<id>:...`. If the pipeline fails with
`Not authorized to perform sts:AssumeRoleWithWebIdentity`, print the real claim
from a temporary workflow step, set `github_oidc_subject_prefix` to it, and apply
again.

### 7. Ship

While rehearsing, avoid Let's Encrypt production limits by using the staging CA:

```bash
aws ssm put-parameter --name /ayopa/acme_ca --type String --region eu-central-1 \
  --value https://acme-staging-v02.api.letsencrypt.org/directory
```

Push to `main`. The pipeline builds both images, uploads the deploy files, runs
migrations, starts the containers and smoke-tests `/healthz`. If the first deploy
fails because the instance is still bootstrapping, re-run the failed job.

When staging works:

```bash
aws ssm delete-parameter --name /ayopa/acme_ca --region eu-central-1
```

Then re-run the deploy job to obtain production certificates.

### 8. Verify

```bash
./scripts/verify.sh yourdomain.com <public-ip>
```

It checks DNS, wildcard DNS, the certificate issuer (fails on staging), `/healthz`,
and that unknown tenants return 404.

To prove the environment is reproducible, follow
[`docs/runbooks/rebuild-demo.md`](docs/runbooks/rebuild-demo.md): destroy
everything, rebuild it, and push a commit.

## CI/CD

```mermaid
flowchart LR
  A["push to main"] --> B["test: gofmt, vet, go test with Postgres, terraform validate"]
  B --> C["assume AWS role via OIDC"]
  C --> D["build amd64 images, push to ECR tagged with commit SHA"]
  D --> E["sync deploy files to S3"]
  E --> F["SSM Run Command: deploy.sh"]
  F --> G["migrate, then docker compose up"]
  G --> H["smoke test /healthz"]
```

- Pull requests run the `test` job only. Deploys happen only from `main`.
- Images are tagged with the commit SHA, so rollback means deploying an older SHA.
- A failed migration stops the deploy before containers are replaced, so the
  previous version keeps serving. Migrations must stay backward compatible with the
  previous app version for this to be safe.
- Protect `main`: require a pull request and a passing `test` check.

## Operations

| Task | How |
|---|---|
| Shell on the server | `aws ssm start-session --target $(terraform -chdir=infra output -raw instance_id)` |
| App and proxy logs | `sudo docker compose -f /opt/ayopa/compose.yml logs app --tail 100` (or `caddy`) |
| Roll back | Re-run an older successful workflow, or `sudo bash /opt/ayopa/deploy.sh <old-sha>` (ECR keeps the last 10 images) |
| Add a migration | New numbered file in `migrations/`. Forward-only, additive, never edit an applied file |
| Rotate the Cloudflare token | Roll it in Cloudflare, `aws ssm put-parameter --overwrite`, then redeploy. Update the token's IP allowlist if it has one |
| Certificates | Issued and renewed automatically by Caddy, stored in the `caddy_data` volume |
| Reserve a new name | Add it to `internal/tenant/validate.go` whenever you create a DNS record for it |
| Tear down | `terraform destroy`, then remove the state bucket and the SSM parameters |

Do not paste logs or `docker compose config` output anywhere public without
checking for secrets. Never run `caddy --environ`; it prints the environment,
including the DNS token.

## Production readiness

This is a deliberately small, single-instance design. The table states what is
done and what is not.

| Area | Status | Notes |
|---|---|---|
| Infrastructure as code | Done | Everything except the state bucket and the DNS token is Terraform |
| Deployments | Done | Automated, secretless, smoke-tested, rollback by SHA |
| Secrets management | Done | SSM SecureString, no secrets in Git, images or CI logs |
| TLS | Done | Wildcard certificate, automatic renewal |
| Tenant isolation | Done | Tested at the HTTP and data layers |
| Schema management | Done | Automated, locked, tested against a hand-built database |
| Request hardening | Done | CSRF, CSP, HSTS, body limits, timeouts, rate limits |
| Logging | Partial | Structured JSON with request IDs, but only on the instance. No central log store |
| Metrics and alerting | Not yet | Only a cost budget alert exists. No uptime or resource alarms |
| Backups | Partial | 1-day automated retention (free-plan limit). Restore procedure not yet exercised |
| Availability | Partial | Single instance, single-AZ database. A host or AZ failure means downtime until rebuilt |
| Rate limiting | Partial | In memory, per process, per IP. Resets on restart and does not scale past one instance |
| Account lifecycle | Not yet | No email verification, password reset or MFA |
| Supply-chain checks | Not yet | No vulnerability scanning gate in CI |
| DNS and token automation | Partial | A rebuild gets a new IP, so DNS records and the token allowlist need manual updates unless the Elastic IP is kept stable |

## Cost

No NAT gateway, no load balancer, free DNS and certificates, and free-tier SSM
parameters. The recurring charges are the RDS instance, the EC2 instance, its public
IPv4 address, and EBS storage, with ECR and S3 minor. A Terraform-managed budget
alert emails you at a threshold. Pricing and free-plan terms change, so check the
current AWS pricing pages and your billing console. Destroy the stack when you are
not using it.

## Roadmap

1. CI: `govulncheck` and container image scanning as gates
2. Observability: central logs, CloudWatch alarms, an external uptime check
3. Tested database restore runbook
4. Email verification, password reset, then MFA
5. Postgres row-level security as a second isolation layer
6. Separate migration and runtime database roles
7. Scope SSM `SendCommand` to the instance by tag
8. Manage Cloudflare DNS and the token with Terraform
9. Automatic rollback when the post-deploy smoke test fails
10. At scale: ALB with ACM, multiple instances, shared rate limiting, Multi-AZ RDS

## License

Add a license before sharing the repository publicly.