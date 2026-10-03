#!/bin/bash
set -euo pipefail
cd /opt/ayopa
TAG="$1"
REGION=eu-central-1
BASE_DOMAIN=ayopa.co.ke
REGISTRY="$(aws sts get-caller-identity --query Account --output text).dkr.ecr.${REGION}.amazonaws.com"

get() { aws ssm get-parameter --name "/ayopa/$1" --with-decryption \
          --query Parameter.Value --output text --region "$REGION"; }

umask 077
cat > .env <<EOF
REGISTRY=${REGISTRY}
TAG=${TAG}
BASE_DOMAIN=${BASE_DOMAIN}
DATABASE_URL=$(get database_url)
CF_API_TOKEN=$(get cloudflare_token)
EOF

aws ecr get-login-password --region "$REGION" | docker login --username AWS --password-stdin "$REGISTRY"
docker compose pull
docker compose up -d --remove-orphans
docker image prune -f