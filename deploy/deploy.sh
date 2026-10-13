#!/bin/bash
set -euo pipefail
cd /opt/ayopa

TAG="$1"
REGION=eu-central-1
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
DATABASE_URL=$(get database_url)
CF_API_TOKEN=$(get cloudflare_token)
ACME_CA=${ACME_CA}
EOF

aws ecr get-login-password --region "$REGION" | docker login --username AWS --password-stdin "$REGISTRY"
docker compose pull

# One certificate block per mall, generated from malls.txt.
bash ./render-caddyfile.sh malls.txt > Caddyfile.new
cat Caddyfile.new > Caddyfile
rm Caddyfile.new

# Reject a broken Caddyfile before touching the running proxy, because one
# typo would otherwise take every mall offline.
docker compose run --rm --no-deps caddy caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile

# If a migration fails, set -e stops here and the old version keeps serving.
docker compose run --rm app migrate

docker compose up -d --remove-orphans
docker image prune -f