#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "Usage: $0 <listing_id> <buyer_id>" >&2
  echo "Example: $0 listing-1 Bob" >&2
  exit 2
fi

headers=()
if [[ -n "${TOKEN:-}" ]]; then
  headers=(-H "Authorization: Bearer $TOKEN")
fi

curl -sS "${headers[@]}" -X POST http://127.0.0.1:8080/orders \
  -H 'Content-Type: application/json' \
  --data "{\"listing_id\":\"$1\",\"buyer_id\":\"$2\"}"
printf '\n'
