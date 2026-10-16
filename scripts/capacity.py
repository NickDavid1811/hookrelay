"""Real processes: full queue, restart, retention, then restored capacity."""
import json
import sqlite3
import subprocess
import tempfile
from pathlib import Path
import urllib.error
import urllib.request
from failures import ROOT, ENV, address, wait_until, ready


def run():
    with tempfile.TemporaryDirectory(prefix="hookrelay-capacity-") as tmp:
        folder = Path(tmp)
        api, receiver_addr = address(), address()
        base = "http://" + api
        processes, logs = [], []

        def start(name, args):
            log = (folder / f"{name}-{len(processes)}.log").open("wb")
            logs.append(log)
            process = subprocess.Popen([str(ROOT / "bin" / name), *args], env=ENV, stdout=log, stderr=log)
            processes.append(process)
            return process

        def call(method, path, key="one", body=None):
            request = urllib.request.Request(base + path, method=method, data=body,
                headers={"Authorization": "Bearer " + ENV["HOOKRELAY_TOKEN"], "Idempotency-Key": key,
                         "Content-Type": "application/json"})
            try:
                with urllib.request.urlopen(request, timeout=2) as response:
                    return response.status, json.load(response)
            except urllib.error.HTTPError as error:
                return error.code, json.load(error)

        args = ["--listen", api, "--endpoint", "http://" + receiver_addr + "/webhook", "--allow-loopback-test",
                "--db", str(folder / "test.db"), "--max-queue", "1", "--max-client-queue", "1",
                "--max-events", "1", "--success-retention", "2s", "--cleanup-interval", "1s", "--cleanup-batch", "1"]
        payload = b'{"endpoint_id":"test","event_type":"capacity","payload":{"test":true}}'
        try:
            receiver = start("receiver", ["--listen", receiver_addr, "--mode", "slow", "--delay", "60s", "--fail-first", "100"])
            wait_until(lambda: ready("http://" + receiver_addr, "/healthz", receiver), 5, "receiver")
            dispatcher = start("dispatcher", args)
            wait_until(lambda: ready(base, "/readyz", dispatcher), 5, "dispatcher")
            code, publication = call("POST", "/api/v1/publish", body=payload)
            assert code == 202
            path = "/api/v1/deliveries/" + publication["delivery_id"]
            assert call("POST", "/api/v1/publish", body=payload)[0] == 202, "duplicate blocked by full queue"
            assert call("POST", "/api/v1/publish", key="two", body=payload)[0] == 429, "queue cap ignored"
            dispatcher.terminate()
            dispatcher.wait(timeout=8)
            receiver.terminate()
            receiver.wait(timeout=5)
            # Retention must not discard the unfinished delivery, even at a short age.
            new_receiver_addr = address()
            new_api = address()
            base = "http://" + new_api
            with sqlite3.connect(folder / "test.db") as db:
                db.execute("UPDATE endpoints SET url = ? WHERE id = 'test'", ("http://" + new_receiver_addr + "/webhook",))
                db.commit()
            receiver = start("receiver", ["--listen", new_receiver_addr])
            wait_until(lambda: ready("http://" + new_receiver_addr, "/healthz", receiver), 5, "normal receiver")
            args = ["--listen", new_api, "--endpoint", "http://" + new_receiver_addr + "/webhook", "--allow-loopback-test",
                    "--db", str(folder / "test.db"), "--max-queue", "1", "--max-client-queue", "1",
                    "--max-events", "1", "--success-retention", "2s", "--cleanup-interval", "1s", "--cleanup-batch", "1"]
            dispatcher = start("dispatcher", args)
            wait_until(lambda: ready(base, "/readyz", dispatcher), 5, "restart")
            assert call("GET", path)[0] == 200, "unfinished work removed during restart"
            wait_until(lambda: call("GET", path)[1].get("status") == "SUCCESS", 40, "recovered SUCCESS")
            assert call("POST", "/api/v1/publish", key="two", body=payload)[0] == 503, "retained event cap ignored"
            wait_until(lambda: call("GET", path)[0] == 404, 6, "expired SUCCESS cleanup")
            code, fresh = call("POST", "/api/v1/publish", body=payload)
            assert code == 202 and fresh["delivery_id"] != publication["delivery_id"], "expired idempotency key not reclaimed"
            print(json.dumps({"scenario": "queue/retention/restart", "queue_rejection": 429,
                              "retained_rejection": 503, "unfinished_work_preserved": True,
                              "expired_key_reused": True}), flush=True)
        except Exception:
            for log in folder.glob("*.log"):
                print(log.name + ":\n" + log.read_text())
            raise
        finally:
            for process in processes:
                if process.poll() is None:
                    process.terminate()
            for process in processes:
                try:
                    process.wait(timeout=8)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
            for log in logs:
                log.close()


if __name__ == "__main__":
    run()
