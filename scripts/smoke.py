"""Isolated end-to-end check of publication, delivery, history and protected metrics."""
import json
import os
import subprocess
import tempfile
import urllib.error
import urllib.request
from pathlib import Path

from failures import address, wait_until

ROOT = Path(__file__).resolve().parents[1]
ENV = dict(os.environ, HOOKRELAY_TOKEN="smoke-token-local",
           HOOKRELAY_SECRET="smoke-secret-local", HOOKRELAY_METRICS_TOKEN="smoke-metrics-local")
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def alive(process):
    if process.poll() is not None:
        raise AssertionError(f"child exited early: {process.returncode}")


def ready(base, path, process):
    alive(process)
    try:
        with OPENER.open(base + path, timeout=2) as response:
            healthy = response.status in (200, 204)
        alive(process)
        return healthy
    except (urllib.error.URLError, TimeoutError):
        alive(process)
        return False


def request(base, path, data=None):
    req = urllib.request.Request(base + path, data=data, headers={
        "Authorization": "Bearer " + ENV["HOOKRELAY_TOKEN"],
        "Idempotency-Key": "smoke", "Content-Type": "application/json"})
    with OPENER.open(req, timeout=2) as response:
        return response.status, json.load(response)


def main():
    with tempfile.TemporaryDirectory(prefix="hookrelay-smoke-") as tmp:
        folder = Path(tmp)
        api, receiver_addr = address(), address()
        base = "http://" + api
        processes, logs = [], []

        def start(name, args):
            log = (folder / f"{name}.log").open("wb")
            logs.append(log)
            process = subprocess.Popen([str(ROOT / "bin" / name), *args], env=ENV,
                                       stdout=log, stderr=log)
            processes.append(process)
            return process

        try:
            receiver = start("receiver", ["--listen", receiver_addr, "--fail-first", "0"])
            wait_until(lambda: ready("http://" + receiver_addr, "/healthz", receiver), 5, "receiver")
            dispatcher = start("dispatcher", ["--listen", api, "--allow-loopback-test",
                               "--endpoint", "http://" + receiver_addr + "/webhook",
                               "--db", str(folder / "test.db")])
            wait_until(lambda: ready(base, "/readyz", dispatcher), 5, "dispatcher")
            for process in processes:
                alive(process)
            code, result = request(base, "/api/v1/publish",
                b'{"endpoint_id":"test","event_type":"test.sent","payload":{"hello":"world"}}')
            assert code == 202

            def success():
                for process in processes:
                    alive(process)
                _, status = request(base, "/api/v1/deliveries/" + result["delivery_id"])
                return status if status["status"] == "SUCCESS" else None

            status = wait_until(success, 5, "successful delivery")
            _, attempts = request(base, "/api/v1/deliveries/" + result["delivery_id"] + "/attempts")
            assert len(attempts) == 1 and attempts[0]["http_status"] == 204, attempts
            req = urllib.request.Request(base + "/metrics", headers={
                "Authorization": "Bearer " + ENV["HOOKRELAY_METRICS_TOKEN"]})
            with OPENER.open(req, timeout=3) as response:
                metrics = response.read().decode()
            for expected in ('hookrelay_queue_depth 0\n', 'hookrelay_events_retained 1\n',
                             'hookrelay_worker_running 1\n',
                             'hookrelay_publications_total{result="new"} 1\n',
                             'hookrelay_delivery_results_total{result="SUCCESS"} 1\n'):
                assert expected in metrics, expected
            for sensitive in (result["delivery_id"], result["event_id"], ENV["HOOKRELAY_TOKEN"],
                              ENV["HOOKRELAY_SECRET"], ENV["HOOKRELAY_METRICS_TOKEN"]):
                assert sensitive not in metrics
            req = urllib.request.Request(base + "/metrics", headers={
                "Authorization": "Bearer " + ENV["HOOKRELAY_TOKEN"]})
            try:
                with OPENER.open(req, timeout=3):
                    raise AssertionError("business token accessed global metrics")
            except urllib.error.HTTPError as error:
                assert error.code == 401
            print(json.dumps({"publish": code, "result": status, "history": attempts,
                              "metrics": "verified and protected"}))
        finally:
            for process in processes:
                process.terminate()
            for process in processes:
                try:
                    process.wait(timeout=8)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
            for log in logs:
                log.close()
                print(Path(log.name).read_text())


if __name__ == "__main__":
    main()
