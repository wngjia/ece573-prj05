#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 3 ]]; then
  echo "Usage: $0 <account_id> [from_YYYY-MM-DD] [to_YYYY-MM-DD]" >&2
  echo "Example: $0 Bob 2026-10-01 2026-10-31" >&2
  exit 2
fi

headers=()
if [[ -n "${TOKEN:-}" ]]; then
  headers=(-H "Authorization: Bearer $TOKEN")
fi

query=()
if [[ -n "${2:-}" ]]; then
  query+=(--data-urlencode "from=$2")
fi
if [[ -n "${3:-}" ]]; then
  query+=(--data-urlencode "to=$3")
fi

curl -sS -G "${headers[@]}" "${query[@]}" "http://127.0.0.1:8080/accounts/$1"
printf '\n'
