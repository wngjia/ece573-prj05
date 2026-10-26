#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "Usage: $0 <account_id> <positive_amount>" >&2
  echo "Example: $0 Bob 100 (set TOKEN to an admin JWT when auth is enabled)" >&2
  exit 2
fi

headers=()
if [[ -n "${TOKEN:-}" ]]; then
  headers=(-H "Authorization: Bearer $TOKEN")
fi

jq -nc --argjson amount "$2" '{amount:$amount}' |
  curl -sS -X POST "http://127.0.0.1:8080/accounts/$1/credits" \
    -H 'Content-Type: application/json' \
    "${headers[@]}" \
    --data-binary @-
printf '\n'
