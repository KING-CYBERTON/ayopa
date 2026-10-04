#!/usr/bin/env bash
set -euo pipefail
docker compose -f docker-compose.dev.yml up -d --wait
export TEST_DATABASE_URL='postgres://app:postgres@localhost:5433/ayopa_test?sslmode=disable'
go test ./... -race -count=1
