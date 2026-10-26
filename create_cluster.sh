#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 0 ]]; then
  echo "Usage: $0" >&2
  exit 2
fi

cd "$(dirname "$0")"
for tool in docker kind kubectl openssl; do
  command -v "$tool" >/dev/null || { echo "Missing $tool" >&2; exit 1; }
done

cluster=ece573-prj05
if kind get clusters | grep -Fxq "$cluster"; then
  for file in private.pem public.pem admin-password; do
    if [[ ! -s "secrets/$file" ]]; then
      echo "Missing secrets/$file. Restore it, or run ./destroy_cluster.sh then ./create_cluster.sh (deletes database data)." >&2
      exit 1
    fi
  done
  if [[ "$(docker port "$cluster-control-plane" 30080/tcp)" != "127.0.0.1:8080" ]] ||
     [[ "$(docker port "$cluster-control-plane" 30081/tcp)" != "127.0.0.1:8081" ]]; then
    echo "Existing ece573-prj05 cluster uses different host ports. Run ./destroy_cluster.sh before recreating it." >&2
    exit 1
  fi
  kind export kubeconfig --name "$cluster"
else
  (
    umask 077
    mkdir -p secrets
    chmod 700 secrets
    staging_dir="$(mktemp -d secrets/.staging.XXXXXX)"
    trap 'rm -rf "$staging_dir"' EXIT
    openssl genpkey -algorithm ED25519 -out "$staging_dir/private.pem"
    openssl pkey -in "$staging_dir/private.pem" -pubout -out "$staging_dir/public.pem"
    head -c 24 /dev/urandom | base64 > "$staging_dir/admin-password"
    mv "$staging_dir/private.pem" secrets/private.pem
    mv "$staging_dir/public.pem" secrets/public.pem
    mv "$staging_dir/admin-password" secrets/admin-password
  )
  kind create cluster --name "$cluster" --config kind-cluster.yaml
fi
kubectl cluster-info >/dev/null

if ! kubectl get statefulset/postgres statefulset/cassandra >/dev/null 2>&1; then
  # Bootstrap databases once. Their Pods and volumes stay in place on later starts.
  docker image inspect postgres:16-alpine >/dev/null 2>&1 || docker pull postgres:16-alpine
  docker image inspect cassandra:4.1.7 >/dev/null 2>&1 || docker pull cassandra:4.1.7
  image_archive="$(mktemp /tmp/ece573-prj05-image.XXXXXX.tar)"
  trap 'rm -f "$image_archive"' EXIT
  for image in postgres:16-alpine cassandra:4.1.7; do
    docker image save --platform linux/amd64 -o "$image_archive" "$image"
    kind load image-archive --name "$cluster" "$image_archive"
  done
  kubectl create configmap shopping-postgres-schema \
    --from-file=schema.sql=database/schema.sql --dry-run=client -o yaml |
    kubectl apply -f -
  kubectl apply -f k8s/databases.yaml
fi

replicas=$(kubectl get statefulset/cassandra -o jsonpath='{.spec.replicas}')
if [[ "$replicas" != 3 ]]; then
  echo "Existing Cassandra has $replicas Pods; this project requires 3. Run ./destroy_cluster.sh before recreating it." >&2
  exit 1
fi

kubectl rollout status statefulset/postgres --timeout=5m
kubectl rollout status statefulset/cassandra --timeout=12m
