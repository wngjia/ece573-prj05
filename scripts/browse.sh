#!/usr/bin/env bash
set -euo pipefail

if [[ $# -gt 1 ]]; then
  echo "Usage: $0 [seller_id]" >&2
  echo "Example: $0 Alice" >&2
  exit 2
fi

if [[ $# -eq 1 ]]; then
  curl -sS --get http://127.0.0.1:8080/listings \
    --data-urlencode "seller_id=$1"
else
  curl -sS http://127.0.0.1:8080/listings
fi
printf '\n'

