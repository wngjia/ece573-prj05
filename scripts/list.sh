#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "Usage: $0 <seller_id> <title> <credit_cost>" >&2
  echo "Example: $0 Alice 'Used textbook' 25" >&2
  exit 2
fi

headers=()
if [[ -n "${TOKEN:-}" ]]; then
  headers=(-H "Authorization: Bearer $TOKEN")
fi

curl -sS "${headers[@]}" -X POST http://127.0.0.1:8080/listings \
  -H 'Content-Type: application/json' \
  --data "{\"seller_id\":\"$1\",\"title\":\"$2\",\"credit_cost\":$3}"
printf '\n'
