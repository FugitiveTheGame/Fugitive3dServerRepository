#!/usr/bin/env bash
# Acceptance check for the deployed server repository.
#
# Run this from OFF the box, so it exercises DNS, nginx and the proxy path
# rather than talking to the service directly. Running it on hydra will pass
# the wrong things: loopback is a trusted proxy, so a local caller can set its
# own forwarded address.

set -uo pipefail

HOST="${1:-repository.fugitivethegame.online}"
BASE="http://${HOST}"

failures=0

pass() { printf '  ok    %s\n' "$1"; }
fail() { printf '  FAIL  %s\n' "$1"; failures=$((failures + 1)); }

printf 'Checking %s\n\n' "$BASE"

# 1. DNS resolves at all. Everything else is meaningless without it.
if getent hosts "$HOST" >/dev/null 2>&1 || host "$HOST" >/dev/null 2>&1; then
  pass "$HOST resolves"
else
  fail "$HOST does not resolve - an A record is needed before anything else works"
  printf '\nStopping: nothing below can pass without DNS.\n'
  exit 1
fi

# 2. The list endpoint answers with JSON. This is the one that keeps working
#    even when the proxy is misconfigured, so passing it proves little on its
#    own.
servers="$(curl -sS --max-time 10 "${BASE}/servers" 2>/dev/null)"
if printf '%s' "$servers" | grep -qE '^\[.*\]$'; then
  pass "GET /servers returns a JSON list: ${servers:0:60}"
else
  fail "GET /servers did not return a JSON list, got: ${servers:0:120}"
fi

# 3. No redirect hop. certbot --nginx inserts a 301 to https, which breaks
#    shipped clients that hardcode http.
redirects="$(curl -sS -o /dev/null --max-time 10 -w '%{num_redirects}' "${BASE}/servers" 2>/dev/null)"
if [ "$redirects" = "0" ]; then
  pass "no redirect hop on /servers"
else
  fail "/servers redirects ${redirects} time(s) - shipped clients speak plain HTTP and may not follow"
fi

# 4. The proxy forwards the real client address. This is the check that
#    actually distinguishes a working deployment from a broken one: if it
#    reports 127.0.0.1, every registration will 403 and the browser stays
#    empty while everything above still looks fine.
reflected="$(curl -sS --max-time 10 "${BASE}/reflection/ip" 2>/dev/null)"
case "$reflected" in
  *127.0.0.1*)
    fail "/reflection/ip returned 127.0.0.1 - nginx is not passing X-Forwarded-For, or the service is not trusting it. Game servers cannot register."
    ;;
  *'"ip"'*)
    pass "/reflection/ip returned a real address: ${reflected}"
    ;;
  *)
    fail "/reflection/ip gave an unexpected response: ${reflected:0:120}"
    ;;
esac

printf '\n'
if [ "$failures" -eq 0 ]; then
  printf 'All checks passed.\n'
  printf 'Now load https://fugitivethegame.online/gamestats and confirm the\n'
  printf '"Servers online" tile shows a number rather than an em dash.\n'
  exit 0
fi

printf '%d check(s) failed.\n' "$failures"
exit 1
