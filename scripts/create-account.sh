#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 <account_id>" >&2
  echo "Example: $0 Alice" >&2
  echo "Password is prompted on a terminal or read from stdin." >&2
  exit 2
fi

if [[ -t 0 ]]; then
  read -rs -p "Password: " password
  printf '\n' >&2
else
  IFS= read -r password
fi

jq -nc --arg account_id "$1" --arg password "$password" \
  '{account_id:$account_id,password:$password}' |
  curl -sS -X POST http://127.0.0.1:8081/accounts \
    -H 'Content-Type: application/json' \
    --data-binary @-
printf '\n'
