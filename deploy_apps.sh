#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 0 ]]; then
  echo "Usage: $0" >&2
  exit 2
fi

cd "$(dirname "$0")"
for tool in docker kind kubectl curl; do
  command -v "$tool" >/dev/null || { echo "Missing $tool" >&2; exit 1; }
done

cluster=ece573-prj05
if ! kind get clusters | grep -Fxq "$cluster"; then
  echo "Cluster $cluster does not exist. Run ./create_cluster.sh first." >&2
  exit 1
fi
kind export kubeconfig --name "$cluster"
kubectl cluster-info >/dev/null
kubectl rollout status statefulset/postgres --timeout=5m
kubectl rollout status statefulset/cassandra --timeout=12m
for file in private.pem public.pem admin-password; do
  if [[ ! -s "secrets/$file" ]]; then
    echo "Missing secrets/$file. Restore it, or run ./destroy_cluster.sh then ./create_cluster.sh (deletes database data)." >&2
    exit 1
  fi
done

image_archive="$(mktemp /tmp/ece573-prj05-image.XXXXXX.tar)"
trap 'rm -f "$image_archive"' EXIT

for target in login marketplace; do
  docker build --target "$target" -t "ece573-prj05-$target:v1" .
done
for image in ece573-prj05-login:v1 ece573-prj05-marketplace:v1; do
  docker image save --platform linux/amd64 -o "$image_archive" "$image"
  kind load image-archive --name "$cluster" "$image_archive"
done

kubectl create secret generic shopping-credentials \
  --from-file=private.pem=secrets/private.pem \
  --from-file=public.pem=secrets/public.pem \
  --from-file=admin-password=secrets/admin-password \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f k8s/apps.yaml
kubectl rollout restart deployment/login-service deployment/marketplace-service

kubectl rollout status deployment/login-service --timeout=5m
kubectl rollout status deployment/marketplace-service --timeout=5m
for url in http://127.0.0.1:8080/healthz http://127.0.0.1:8081/healthz; do
  ready=false
  for _ in $(seq 1 30); do
    if curl --fail --silent "$url" >/dev/null; then
      ready=true
      break
    fi
    sleep 1
  done
  if [[ "$ready" != true ]]; then
    echo "Host endpoint did not become ready: $url" >&2
    exit 1
  fi
done
echo "Ready: marketplace http://127.0.0.1:8080; login http://127.0.0.1:8081"
