#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 0 ]]; then
  echo "Usage: $0" >&2
  exit 2
fi

cd "$(dirname "$0")"
for tool in kubectl curl jq openssl; do
  command -v "$tool" >/dev/null || { echo "Missing $tool" >&2; exit 1; }
done

if ! kubectl get \
  statefulset/postgres statefulset/cassandra \
  service/postgres service/cassandra >/dev/null; then
  echo "Database resources are missing. Run ./create_cluster.sh first." >&2
  exit 1
fi
if ! kubectl get \
  deployment/login-service deployment/marketplace-service \
  service/login-service service/marketplace-service >/dev/null; then
  echo "Application resources are missing. Run ./deploy_apps.sh first (after ./create_cluster.sh if needed)." >&2
  exit 1
fi
for db in postgres cassandra; do
  kubectl rollout status "statefulset/$db" --timeout=12m >/dev/null
done
for app in login-service marketplace-service; do
  kubectl rollout status "deployment/$app" --timeout=5m >/dev/null
done
for url in http://127.0.0.1:8080/healthz http://127.0.0.1:8081/healthz; do
  echo "Checking application endpoint $url..." >&2
  healthy=false
  for attempt in $(seq 1 10); do
    if curl -fsS --max-time 3 "$url" >/dev/null 2>&1; then
      healthy=true
      break
    fi
    if (( attempt < 10 )); then
      if (( attempt == 1 || attempt == 5 )); then
        echo "Endpoint $url is not ready; retrying..." >&2
      fi
      sleep 2
    fi
  done
  if [[ "$healthy" != true ]]; then
    echo "Application endpoint $url is unavailable after 10 checks. Check the Pods and run ./deploy_apps.sh if needed." >&2
    exit 1
  fi
done
mkdir -p out

suffix="$(openssl rand -hex 8)"
alice="Alice_$suffix"
bob="Bob_$suffix"
password="$(openssl rand -hex 16)"
printf '%s\n' "$password" | ./scripts/create-account.sh "$alice" > out/alice.json
jq -e --arg id "$alice" '.account_id == $id' out/alice.json >/dev/null
printf '%s\n' "$password" | ./scripts/create-account.sh "$bob" > out/bob.json
jq -e --arg id "$bob" '.account_id == $id' out/bob.json >/dev/null
./scripts/grant-credits.sh "$bob" 100 > out/bob-grant.json

./scripts/list.sh "$alice" "Used textbook" 25 > out/listing.json
listing_id="$(jq -er '.listing_id' out/listing.json)"
./scripts/browse.sh > out/browse.json
jq -e --arg listing_id "$listing_id" \
  '[.listings[].listing_id] | index($listing_id) != null' \
  out/browse.json >/dev/null

./scripts/order.sh "$listing_id" "$bob" > out/order.json
jq -e '.status == "completed"' out/order.json >/dev/null
order_id="$(jq -er '.order_id' out/order.json)"
day="$(jq -er '.ordered_at | split("T")[0]' out/order.json)"

./scripts/order.sh "$listing_id" "$bob" > out/repeat.json
jq -e '.error == "listing is no longer available"' out/repeat.json >/dev/null
./scripts/account.sh "$bob" > out/bob-account.json
./scripts/account.sh "$alice" > out/alice-account.json
./scripts/browse.sh > out/browse-after-order.json

jq -e --arg order_id "$order_id" \
  '.balance == 75 and (.orders | length) == 1 and
   .orders[0].order_id == $order_id and (.sales | length) == 0' \
  out/bob-account.json >/dev/null
jq -e --arg order_id "$order_id" --arg listing_id "$listing_id" \
  '.balance == 25 and (.sales | length) == 1 and
   .sales[0].order_id == $order_id and (.listings | length) == 1 and
   .listings[0].listing_id == $listing_id and .listings[0].status == "sold"' \
  out/alice-account.json >/dev/null
jq -e --arg listing_id "$listing_id" \
  '(.listings | type) == "array" and all(.listings[]; .listing_id != $listing_id)' \
  out/browse-after-order.json >/dev/null

./scripts/account.sh "$bob" "$day" "$day" > out/bob-day.json
jq -e --arg order_id "$order_id" \
  '(.orders | length) == 1 and .orders[0].order_id == $order_id' \
  out/bob-day.json >/dev/null
./scripts/account.sh "$bob" 2000-01-01 2000-01-02 > out/bob-empty-range.json
jq -e '.balance == 75 and (.orders | length) == 0' \
  out/bob-empty-range.json >/dev/null

echo "Baseline passed with seller $alice and buyer $bob. Responses are saved in out/."
