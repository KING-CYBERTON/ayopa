#!/usr/bin/env bash
set -euo pipefail

DOMAIN="${1:?usage: verify.sh <domain> [expected-ip]}"
EXPECTED_IP="${2:-}"

pass() { printf 'PASS  %s\n' "$1"; }
fail() { printf 'FAIL  %s\n' "$1"; exit 1; }

ip="$(dig +short "$DOMAIN" @1.1.1.1 | tail -n1)"
[ -n "$ip" ] || fail "no A record for $DOMAIN"
if [ -n "$EXPECTED_IP" ] && [ "$ip" != "$EXPECTED_IP" ]; then
  fail "DNS points to $ip, expected $EXPECTED_IP"
fi
pass "DNS $DOMAIN -> $ip"

wild="$(dig +short "verify-$RANDOM.$DOMAIN" @1.1.1.1 | tail -n1)"
[ "$wild" = "$ip" ] || fail "wildcard DNS returned '$wild', expected $ip"
pass "wildcard DNS"

issuer="$(echo | openssl s_client -connect "$DOMAIN:443" -servername "$DOMAIN" 2>/dev/null | openssl x509 -noout -issuer)"
case "$issuer" in
  *STAGING*) fail "staging certificate: $issuer" ;;
  *"Let's Encrypt"*) pass "certificate: $issuer" ;;
  *) fail "unexpected certificate: $issuer" ;;
esac

[ "$(curl -fsS "https://$DOMAIN/healthz")" = "ok" ] || fail "/healthz did not return ok"
pass "/healthz ok (app and database reachable)"

code="$(curl -s -o /dev/null -w '%{http_code}' "https://nobody-$RANDOM.$DOMAIN/")"
[ "$code" = "404" ] || fail "unknown tenant returned $code, expected 404"
pass "unknown tenant returns 404"

printf '\nAll checks passed.\n'