#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 0 ]]; then
  echo "Usage: $0" >&2
  exit 2
fi

cd "$(dirname "$0")"
kind delete cluster --name ece573-prj05
rm -f secrets/private.pem secrets/public.pem secrets/admin-password

echo "Project 5 cluster, database data, and local credentials removed."
