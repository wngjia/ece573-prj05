#!/usr/bin/env python3
"""Run one Cassandra write-retry trial with a fixed listing workload."""

import argparse
import concurrent.futures
import json
import random
import secrets
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path


class TrialError(Exception):
    """A trial setup or service failure, separate from invalid CLI arguments."""


APP_SETUP_HINT = ("Check the kubectl context and run ./deploy_apps.sh "
                  "(after ./create_cluster.sh if needed).")


def log_progress(message):
    print(message, file=sys.stderr, flush=True)


def request_json(url, timeout, payload=None):
    headers = {"Content-Type": "application/json"} if payload is not None else {}
    body = json.dumps(payload).encode() if payload is not None else None
    request = urllib.request.Request(url, data=body, headers=headers)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, None


def submit_listing(index, args, prefix):
    payload = {
        "seller_id": args.seller,
        "title": f"{prefix}{index:05d}",
        "credit_cost": 1,
    }
    try:
        status, _ = request_json(args.url + "/listings", args.client_timeout, payload)
        return str(status)
    except (OSError, ValueError) as error:
        return "transport:" + type(error).__name__


def disrupt_cassandra(args, started, midway, events):
    rng = random.Random(args.seed)
    if args.fault_at is None:
        midway.wait()
        first_fault_time = time.monotonic()
    else:
        first_fault_time = started + args.fault_at
    for number in range(args.fault_count):
        target_time = first_fault_time + number * args.fault_interval
        if target_time > time.monotonic():
            time.sleep(target_time - time.monotonic())
        pods = rng.sample(args.fault_targets, args.fault_pods)
        command = [
            "kubectl", "delete", "pod",
            *pods, "--grace-period=0", "--force", "--wait=false",
        ]
        log_progress(f"Fault {number + 1}/{args.fault_count}: deleting {', '.join(pods)}")
        try:
            result = subprocess.run(command, capture_output=True, text=True, check=False)
            exit_code = result.returncode
            message = (result.stderr or result.stdout).strip()
        except OSError as error:
            exit_code = 1
            message = str(error)
        events.append({"seconds": round(time.monotonic() - started, 2),
                       "pods": pods, "exit_code": exit_code, "message": message})
        log_progress(f"Fault {number + 1}/{args.fault_count}: kubectl exit {exit_code}")


def count_run_listings(args, prefix):
    deadline = time.monotonic() + args.settle_timeout
    last_error = ""
    attempts = 0
    while time.monotonic() < deadline:
        attempts += 1
        try:
            status, payload = request_json(args.url + "/listings", args.client_timeout)
            if status == 200 and isinstance(payload, dict) and isinstance(payload.get("listings"), list):
                titles = [item.get("title") for item in payload["listings"]
                          if isinstance(item, dict) and
                          isinstance(item.get("title"), str) and
                          item["title"].startswith(prefix)]
                return len(titles), None
            last_error = f"browse HTTP {status}"
        except (OSError, ValueError) as error:
            last_error = f"browse {type(error).__name__}: {error}"
        if attempts == 1 or attempts % 5 == 0:
            log_progress(f"Waiting for Cassandra-backed browse: {last_error}")
        time.sleep(2)
    return None, last_error or "browse unavailable"


def wait_for_settle(seconds):
    if seconds == 0:
        return
    log_progress(f"Waiting {seconds:g}s for server requests to settle")
    deadline = time.monotonic() + seconds
    while True:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            break
        time.sleep(min(15, remaining))
        log_progress(f"Settle wait: {max(0, deadline - time.monotonic()):.0f}s remaining")


def save_marketplace_logs(retries, since_time):
    log_path = Path(__file__).resolve().parent / "out" / f"retry-{retries}-marketplace.log"
    log_path.unlink(missing_ok=True)
    command = ["kubectl", "logs", "deployment/marketplace-service",
               "--all-pods=true", "--prefix=true",
               f"--since-time={since_time}"]
    try:
        result = subprocess.run(command, capture_output=True, text=True, check=False)
    except OSError as error:
        raise TrialError(f"cannot collect marketplace logs: {error}") from error
    if result.returncode:
        raise TrialError(f"cannot collect marketplace logs: {result.stderr.strip()}")
    log_path.parent.mkdir(parents=True, exist_ok=True)
    try:
        log_path.write_text(result.stdout)
    except OSError:
        log_path.unlink(missing_ok=True)
        raise
    return log_path


def check_project_resources():
    resources = [
        "statefulset/postgres", "statefulset/cassandra",
        "deployment/login-service", "deployment/marketplace-service",
        "service/postgres", "service/cassandra",
        "service/login-service", "service/marketplace-service",
    ]
    try:
        subprocess.run(["kubectl", "get", *resources], check=True,
                       stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True)
    except subprocess.CalledProcessError as error:
        detail = (error.stderr or "").strip()
        raise TrialError(f"project resources are missing or unavailable: {detail}. {APP_SETUP_HINT}") from error
    except OSError as error:
        raise TrialError(f"cannot check project resources: {error}. {APP_SETUP_HINT}") from error


def prepare_trial(retries):
    commands = [
        (["kubectl", "rollout", "status", "statefulset/postgres", "--timeout=5m"],
         "waiting for Postgres"),
        (["kubectl", "rollout", "status", "statefulset/cassandra", "--timeout=12m"],
         "waiting for Cassandra"),
        (["kubectl", "set", "env", "deployment/marketplace-service",
          f"CASSANDRA_WRITE_RETRIES={retries}"],
         "setting marketplace write retries"),
        (["kubectl", "rollout", "status", "deployment/marketplace-service",
          "--timeout=5m"],
         "waiting for marketplace"),
        (["kubectl", "rollout", "status", "deployment/login-service",
          "--timeout=5m"],
         "waiting for login"),
    ]
    for command, action in commands:
        print(f"{action}: {' '.join(command)}", file=sys.stderr)
        try:
            result = subprocess.run(command, stdout=sys.stderr)
        except OSError as error:
            raise TrialError(f"cannot run kubectl: {error}") from error
        if result.returncode:
            return result.returncode
    return 0


def check_health(name, url, timeout):
    print(f"Checking {name} health at {url}/healthz", file=sys.stderr)
    for attempt in range(1, 11):
        try:
            with urllib.request.urlopen(url + "/healthz", timeout=min(timeout, 3)) as response:
                if response.status == 204:
                    return
                failure = f"HTTP {response.status}"
        except (OSError, ValueError) as error:
            failure = str(error)
        if attempt < 10:
            if attempt in (1, 5):
                log_progress(f"{name} health check failed: {failure}; retrying")
            time.sleep(2)
    raise TrialError(f"{name} is not healthy at {url}/healthz after 10 checks: "
                     f"{failure}. {APP_SETUP_HINT}")


def create_seller(args):
    seller = "seller_" + uuid.uuid4().hex[:12]
    payload = {"account_id": seller, "password": secrets.token_urlsafe(24)}
    last_failure = ""
    for attempt in range(15):
        try:
            status, response = request_json(
                args.login_url + "/accounts", args.client_timeout, payload)
        except (OSError, ValueError) as error:
            last_failure = str(error)
            status = None
        if status == 201:
            if not isinstance(response, dict) or response.get("account_id") != seller:
                raise TrialError("account creation returned an unexpected response")
            print(f"Created seller {seller}", file=sys.stderr)
            return seller
        if status == 409 and attempt > 0:
            # The prior POST may have committed before its connection reset.
            print(f"Seller {seller} already exists after a retry", file=sys.stderr)
            return seller
        if status == 409:
            raise TrialError(f"generated seller ID {seller} already exists; rerun the trial")
        if status not in (None, 500, 502, 503, 504):
            raise TrialError(f"cannot create seller account: HTTP {status}")
        if status is not None:
            last_failure = f"HTTP {status}"
        if attempt < 14:
            time.sleep(2)
    raise TrialError(f"cannot create seller account at {args.login_url} after 15 attempts: {last_failure}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--seller", help="existing seller account ID (default: create one)")
    parser.add_argument("--retries", required=True, type=int,
                        help="additional Cassandra write attempts for this trial (0-20)")
    parser.add_argument("--url", default="http://127.0.0.1:8080")
    parser.add_argument("--login-url", default="http://127.0.0.1:8081")
    parser.add_argument("--count", type=int, default=100)
    parser.add_argument("--rate", type=float, default=10.0, help="new requests per second")
    parser.add_argument("--concurrency", type=int, default=24)
    parser.add_argument("--client-timeout", type=float, default=15.0)
    parser.add_argument("--fault-at", type=float, default=None,
                        help="seconds after workload starts (default: halfway through submissions)")
    parser.add_argument("--fault-count", type=int, default=1)
    parser.add_argument("--fault-interval", type=float, default=30.0)
    parser.add_argument("--fault-pods", type=int, default=None, choices=(1, 2))
    parser.add_argument("--seed", type=int, default=473)
    parser.add_argument("--compact", action="store_true", help="print one JSON line")
    parser.add_argument("--settle-seconds", type=float, default=30.0)
    parser.add_argument("--settle-timeout", type=float, default=180.0)
    args = parser.parse_args()
    if (not 0 <= args.retries <= 20 or args.count < 1 or
            args.concurrency < 1 or args.rate <= 0 or
            args.client_timeout <= 0 or
            (args.fault_at is not None and args.fault_at < 0) or
            args.fault_count < 0 or args.fault_interval <= 0 or
            args.settle_seconds < 0 or args.settle_timeout <= 0):
        parser.error("retries must be 0-20; counts and durations must be positive "
                     "(fault count/at and settle seconds may be zero)")
    args.url = args.url.rstrip("/")
    args.login_url = args.login_url.rstrip("/")
    check_project_resources()

    if args.fault_count:
        args.fault_targets = ["cassandra-1", "cassandra-2"]
        if args.fault_pods is None:
            args.fault_pods = len(args.fault_targets)
        if args.fault_pods > len(args.fault_targets):
            parser.error("--fault-pods exceeds the Cassandra fault targets")

    setup_status = prepare_trial(args.retries)
    if setup_status:
        return setup_status
    check_health("login service", args.login_url, args.client_timeout)
    check_health("marketplace service", args.url, args.client_timeout)

    if args.seller is None:
        args.seller = create_seller(args)

    # A rollout can briefly reset a connection after Kubernetes marks the new
    # Pod ready. Wait for a usable account read before starting the workload.
    account_url = args.url + "/accounts/" + urllib.parse.quote(args.seller, safe="")
    for attempt in range(15):
        try:
            status, _ = request_json(account_url, args.client_timeout)
            if status == 200:
                break
            if status == 404:
                raise TrialError("seller account not found; create the account first")
            failure = f"HTTP {status}"
        except (OSError, ValueError) as error:
            failure = str(error)
        if attempt == 14:
            raise TrialError(f"cannot check seller account after rollout: {failure}")
        time.sleep(2)

    prefix = "prj05-" + uuid.uuid4().hex[:12] + "-"
    events = []
    statuses = Counter()
    midway = threading.Event()
    log_since_time = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ")
    started = time.monotonic()
    progress_step = max(1, args.count//5)
    log_progress(f"Starting retry trial: {args.count} listings at {args.rate:g}/s, "
                 f"{args.retries} write retries")
    fault_thread = threading.Thread(target=disrupt_cassandra,
                                    args=(args, started, midway, events), daemon=True)
    if args.fault_count:
        fault_thread.start()
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrency) as pool:
        futures = []
        for index in range(args.count):
            target_time = started + index / args.rate
            if target_time > time.monotonic():
                time.sleep(target_time - time.monotonic())
            futures.append(pool.submit(submit_listing, index, args, prefix))
            submitted = index + 1
            if submitted == max(1, args.count // 2):
                midway.set()
            if submitted % progress_step == 0 or submitted == args.count:
                log_progress(f"Submitted {submitted}/{args.count} listings")
        for completed, future in enumerate(concurrent.futures.as_completed(futures), 1):
            statuses[future.result()] += 1
            if completed % progress_step == 0 or completed == args.count:
                log_progress(f"Completed {completed}/{args.count} client requests")
    completion_seconds = round(time.monotonic() - started, 2)
    if args.fault_count:
        fault_thread.join()
    wait_for_settle(args.settle_seconds)
    log_progress("Counting this trial's Cassandra-backed listings")
    rows, read_error = count_run_listings(args, prefix)
    try:
        log_path = save_marketplace_logs(args.retries, log_since_time)
        log_progress(f"Saved marketplace logs to {log_path}")
        log_error = None
    except (OSError, TrialError) as error:
        log_path = None
        log_error = str(error)
        log_progress(f"Could not save marketplace logs: {log_error}")
    result = {
        "run_prefix": prefix,
        "seller_id": args.seller,
        "submitted": args.count,
        "retry_count": args.retries,
        "client_concurrency": args.concurrency,
        "client_timeout_seconds": args.client_timeout,
        "send_rate_per_second": args.rate,
        "completion_seconds": completion_seconds,
        "rest_created": statuses["201"],
        "rest_failures": args.count - statuses["201"],
        "rest_statuses": dict(statuses),
        "cassandra_matching_rows": rows,
        "expected_listings": args.count,
        "cassandra_shortfall_vs_submitted": args.count - rows if rows is not None else None,
        "cassandra_read_error": read_error,
        "marketplace_log": str(log_path) if log_path else None,
        "marketplace_log_error": log_error,
        "faults": events,
    }
    if args.compact:
        print(json.dumps(result, sort_keys=True))
    else:
        for key in ("expected_listings", "rest_created", "rest_failures",
                    "cassandra_matching_rows"):
            value = result[key]
            print(f"{key}: {'unavailable' if value is None else value}")
    if read_error or log_error or any(event["exit_code"] != 0 for event in events):
        return 1
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except TrialError as error:
        print(f"run_retry.py: error: {error}", file=sys.stderr)
        sys.exit(1)
