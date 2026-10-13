#!/bin/bash
# Usage: render-caddyfile.sh malls.txt > Caddyfile
set -euo pipefail

MALLS_FILE="${1:?usage: render-caddyfile.sh <malls-file>}"

cat <<'EOF'
{
	acme_ca {$ACME_CA:https://acme-v02.api.letsencrypt.org/directory}
}

(mall) {
	tls {
		dns cloudflare {env.CF_API_TOKEN}
	}
	reverse_proxy app:8080
}

EOF

count=0
while IFS= read -r line || [ -n "$line" ]; do
  domain="$(printf '%s' "${line%%#*}" | tr -d '[:space:]' | tr 'A-Z' 'a-z')"
  [ -z "$domain" ] && continue
  if ! printf '%s' "$domain" | grep -Eq '^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$'; then
    echo "invalid domain in $MALLS_FILE: $domain" >&2
    exit 1
  fi
  printf '%s, *.%s {\n\timport mall\n}\n\n' "$domain" "$domain"
  count=$((count + 1))
done < "$MALLS_FILE"

if [ "$count" -eq 0 ]; then
  echo "no domains found in $MALLS_FILE" >&2
  exit 1
fi