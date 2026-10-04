# Multi-Tenant Subdomain Platform

A small multi-tenant SaaS where every user receives their own subdomain
(`alice.yourapp.com`) on registration, with isolated data per tenant.
Built as a capstone project to demonstrate system design and
enterprise-grade engineering practice.

**Stack:** Go · Caddy · PostgreSQL · AWS EC2 · Cloudflare DNS · Terraform ·
Docker · GitHub Actions

## Architecture

```
Registrar (domain ownership)
   │ nameservers → Cloudflare
   ▼
Cloudflare DNS: yourapp.com, *.yourapp.com → EC2 Elastic IP
   ▼
EC2: Caddy :443 (wildcard TLS) → Go app :8080 → PostgreSQL
```

Wildcard DNS plus a wildcard certificate means a new subdomain is just a
database row. No DNS or certificate action happens at signup.

## Architecture Decision Records

Significant technical decisions are documented as ADRs in
[`docs/adr/`](docs/adr/).

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

### Featured: ADR-0001, Caddy as reverse proxy and TLS terminator

**Decision:** Use Caddy, built with the Cloudflare DNS plugin, to terminate
TLS with a single wildcard certificate (DNS-01 challenge) and reverse-proxy
to the Go app.

**Why:** Automatic certificate issuance and renewal, minimal configuration,
no certificate cost, and it keeps TLS out of the application code.

**Alternatives rejected:** Nginx + Certbot (more moving parts), AWS ALB +
ACM (hourly cost), TLS inside the Go app (couples concerns).

**Trade-off:** One server and a stored DNS API token, mitigated by a
least-privilege token and systemd restarts.

👉 [Read the full ADR-0001](docs/adr/0001-use-caddy-as-reverse-proxy.md)

## Repository layout (planned)

```
.
├── cmd/                 # application entrypoint
├── internal/            # handlers, services, repositories
├── migrations/          # database migrations
├── deploy/              # Caddyfile, systemd units, Terraform
├── docs/
│   └── adr/             # architecture decision records
└── README.md
```

## Getting started
_To be completed: local setup, running tests, and deployment steps._