#!/bin/bash
set -euo pipefail
cd /opt/ayopa

TAG="$1"
REGION=eu-central-1
BASE_DOMAIN=ayopa.co.ke
REGISTRY="$(aws sts get-caller-identity --query Account --output text).dkr.ecr.${REGION}.amazonaws.com"

get() {
  aws ssm get-parameter --name "/ayopa/$1" --with-decryption \
    --query Parameter.Value --output text --region "$REGION"
}

# Optional. Create /ayopa/acme_ca (staging URL) to rehearse rebuilds without
# using up Let's Encrypt's production limits. Delete it for real certificates.
ACME_CA="$(get acme_ca 2>/dev/null || true)"

umask 077
cat > .env <<EOF
REGISTRY=${REGISTRY}
TAG=${TAG}
BASE_DOMAIN=${BASE_DOMAIN}
DATABASE_URL=$(get database_url)
CF_API_TOKEN=$(get cloudflare_token)
ACME_CA=${ACME_CA}
EOF

aws ecr get-login-password --region "$REGION" | docker login --username AWS --password-stdin "$REGISTRY"
docker compose pull

# Migrate first. If a migration fails, set -e stops the deploy here and the
# previous version keeps serving traffic.
docker compose run --rm app migrate

docker compose up -d --remove-orphans
docker image prune -f