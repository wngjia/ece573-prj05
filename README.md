# ECE 473/573 Project 5: Container Orchestration with Kubernetes

Project 5 moves the Project 4 shopping marketplace from Docker Compose to a
local Kubernetes cluster created with `kind`.

The starter provides the cluster configuration, Kubernetes manifests, scripts,
and application source. This project focuses on bounded retries for transient
Cassandra write failures. The baseline checks a completed order and the
Cassandra-backed views. The retry experiment deletes two Cassandra Pods during
a listing workload and compares four retry settings.

Optionally, refer to Project 4 instruction if you would like to
incorporate updates from Project 3.

## Ubuntu 24.04 VM Setup

This project requires an Ubuntu 24.04 LTS `amd64` VM and uses Go 1.26.
Use a fresh VM because the cluster needs ports 8080 and 8081.

From the `ece573-prj05` directory root, provision the VM with:

```bash
sudo ./setup_vm.sh
newgrp docker
```

The script installs and verifies Go 1.26.6, Git, `jq`, OpenSSL, Python 3,
Docker Engine, Buildx, `kind`, and `kubectl`. It also sets the host inotify
instance limit needed by the six kind nodes. `newgrp docker` activates the
Docker-group membership in the current terminal.

Verify the environment with:

```bash
go version
docker --version
docker buildx version
kind version
kubectl version --client
python3 --version
```

## Quick start

From the `ece573-prj05` project root, run:

```bash
./create_cluster.sh
./deploy_apps.sh
./run_baseline.sh
```

`create_cluster.sh` creates one control-plane node and five worker nodes,
loads the database images, deploys Postgres and three Cassandra Pods, and
waits for the databases. `deploy_apps.sh` builds and loads the login and
marketplace images, deploys one login Pod and two marketplace Pods, and waits
for readiness. Both scripts select the Project 5 `kubectl` context.

The application is available on the VM at `127.0.0.1:8080` (marketplace) and
`127.0.0.1:8081` (login). The cluster maps these host ports to Kubernetes
NodePort Services. Postgres and Cassandra Services are internal to the cluster.

The baseline creates uniquely named buyer and seller accounts, grants buyer
credit, lists an item, completes an order, and checks the account and listing
views. It also checks that the item cannot be ordered twice. Responses are
saved under ignored `out/`. The baseline does not reset the databases; each
run uses new account names.

## Individual Operations

Project 5 keeps the Project 4 request scripts. Please refer to Project 3 for
details on enabling authentication and authorization.

- `./create_cluster.sh` creates the cluster and databases.
- `./deploy_apps.sh` rebuilds and restarts the application Pods after a code
  change while preserving database data.
- `./scripts/create-account.sh Alice` creates a regular account with zero
  credits. It prompts for a password or reads one from stdin.
- `./scripts/login.sh Alice` prints a JSON response containing a JWT after
  reading Alice's password. It does not save the token to a file.
- `./scripts/admin-login.sh` prints an admin login response using the
  generated password.
- `./scripts/grant-credits.sh Bob 100` grants 100 credits to Bob.
- `./scripts/list.sh Alice "Used textbook" 25` creates a listing.
- `./scripts/browse.sh` returns available listings; an optional seller ID
  filters the result. Browsing remains public.
- `./scripts/order.sh listing-1 Bob` places an order. Use the ID returned by
  `list.sh`.
- `./scripts/account.sh Bob 2000-01-01 2000-01-02` reads Bob's balance and
  activity, optionally within a time range.
- `./destroy_cluster.sh` deletes the Project 5 cluster and all database data,
  then removes local signing keys and the admin password.

## Source Structure

```text
kind-cluster.yaml                      one control-plane node, five workers, host ports
k8s/databases.yaml                     Postgres and Cassandra Services and StatefulSets
k8s/apps.yaml                          login and marketplace Services and Deployments
database/schema.sql                    authoritative account/listing/order tables
create_cluster.sh                      create cluster and deploy databases
deploy_apps.sh                         build images and deploy applications
run_baseline.sh                         end-to-end marketplace check
run_retry.py                            Cassandra failure and retry workload
login-service/                         registration, login, and admin bootstrap
auth/                                  copied JWT helpers
marketplace-service/main.go            startup and route registrations
marketplace-service/authorization.go   optional Project 3 authorization TODO
marketplace-service/handlers.go        listing, checkout, and account HTTP handlers
marketplace-service/request_limit.go   per-Pod request admission limit
marketplace-service/model/             listing and order types shared by both stores
marketplace-service/history/           Cassandra tables, writes, reads, and retry TODO
marketplace-service/marketplace/       Postgres marketplace logic
scripts/                               host-side request helpers
```

## REST API

Project 5 keeps the Project 4 routes and response shapes: `POST /listings`,
`GET /listings?seller_id=...`, `POST /orders`, `GET /accounts/{account_id}`,
and `POST /accounts/{account_id}/credits`. Login owns `POST /accounts` and
`POST /login`.

`GET /accounts/{account_id}` returns the current Postgres balance together
with Cassandra-backed `listings`, `orders`, and `sales` arrays. With no query
parameters it returns all account activity. Optional `from` and `to` values
filter orders and sales by inclusive UTC order dates; they do not filter
listings. Either bound may be omitted. Invalid dates or a reversed range
return HTTP 400. Cassandra read failures return HTTP 503.

## Cassandra Retry Experiments

First, run the zero-retry case with the starter code.
```bash
./run_retry.py --retries 0
```
It submits 100 uniquely titled listings at 10 new requests per second.
After the 50th submission, it force-deletes `cassandra-1` and `cassandra-2`.
The StatefulSet replaces them, but writes cannot meet `QUORUM` while only
`cassandra-0` is available.

Then complete the TODO in `marketplace-service/history/history.go`:
retry transient Cassandra write errors for at most `s.writeRetries`
additional attempts, waiting `s.retryInterval` between attempts.
Rebuild with `./deploy_apps.sh` before running the remaining cases.
Each invocation of `run_retry.py` sets the retry count
on the marketplace Deployment and waits for its rollout.

```bash
./deploy_apps.sh                 # after completing the retry TODO
./run_retry.py --retries 1
./run_retry.py --retries 3
./run_retry.py --retries 5
```

Each run prints `expected_listings`, `rest_created`, `rest_failures`, and
`cassandra_matching_rows`. It also saves marketplace Pod logs to
`out/retry-N-marketplace.log`, where `N` is the retry count. Running the same
case again replaces its log file. The runner waits 30 seconds after client
requests finish before counting Cassandra rows and collecting logs. Results
depend on Pod recovery and VM performance, so compare observed trends rather
than expecting fixed counts.

For the five-retry run, inspect `event=listing_projection_failed`
in `out/retry-5-marketplace.log`. A failed projection
event means the final attempt failed; a retry that later succeeds does not
produce one. The log also records `event=request_rejected` for marketplace
requests rejected with HTTP 503 by the per-Pod request limit.

## Useful Inspection and Troubleshooting Commands

- `kind get nodes --name ece573-prj05` shows the kind nodes.
- `kubectl get pods,services` shows Pods and Services.
- `kubectl exec cassandra-0 -- nodetool status` shows Cassandra ring status.
- `kubectl exec cassandra-0 -- cqlsh` opens the Cassandra database.
- `kubectl exec postgres-0 -- psql -U marketplace -d marketplace` opens Postgres.
- `kubectl logs deployment/marketplace-service --all-pods=true --prefix=true`
  shows logs from both marketplace Pods.
- `docker exec ece573-prj05-control-plane crictl ps` shows the Kubernetes
  containers running inside the control-plane node.
