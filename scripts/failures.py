"""Black-box tests: real binaries, real process death, temporary SQLite and receipt log."""
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
ENV = dict(os.environ, HOOKRELAY_TOKEN="failure-token-local", HOOKRELAY_SECRET="failure-secret-local")


def address():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return f"127.0.0.1:{sock.getsockname()[1]}"


def request(base, path, data=None):
    req = urllib.request.Request(base + path, data=data, headers={
        "Authorization": "Bearer " + ENV["HOOKRELAY_TOKEN"],
        "Idempotency-Key": "failure-test", "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=2) as response:
        raw = response.read()
        return response.status, json.loads(raw) if raw else None


def wait_until(check, seconds, label):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        result = check()
        if result:
            return result
        time.sleep(0.1)
    raise AssertionError(f"timeout waiting for {label}")


def ready(base, path, process):
    if process.poll() is not None:
        raise AssertionError(f"process exited early: {process.returncode}")
    try:
        return request(base, path)[0] in (200, 204)
    except (urllib.error.URLError, TimeoutError):
        return False


def receipts(path):
    if not path.exists():
        return []
    # Ignore an incomplete last line while the receiver is appending.
    return [json.loads(line) for line in path.read_text().splitlines(keepends=True) if line.endswith("\n")]


def scenario(mode, kill=False, exhaust=False):
    with tempfile.TemporaryDirectory(prefix="hookrelay-failure-") as tmp:
        folder = Path(tmp)
        api, receiver_addr = address(), address()
        base = "http://" + api
        ledger = folder / "receipts.jsonl"
        processes = []
        logs = []

        def start(name, args):
            log = (folder / f"{name}-{len(processes)}.log").open("wb")
            logs.append(log)
            process = subprocess.Popen([str(ROOT / "bin" / name), *args], env=ENV, stdout=log, stderr=log)
            processes.append(process)
            return process

        dispatcher_args = ["--listen", api, "--endpoint", "http://" + receiver_addr + "/webhook",
                           "--allow-loopback-test", "--db", str(folder / "test.db")]
        try:
            receiver = start("receiver", ["--listen", receiver_addr, "--mode", mode,
                             "--fail-first", "5" if exhaust else "1", "--delay", "60s" if kill else "7s",
                             "--ledger", str(ledger)])
            wait_until(lambda: ready("http://" + receiver_addr, "/healthz", receiver), 5, "receiver")
            dispatcher = start("dispatcher", dispatcher_args)
            wait_until(lambda: ready(base, "/readyz", dispatcher), 5, "dispatcher")
            code, publication = request(base, "/api/v1/publish", b'{"endpoint_id":"test","event_type":"test.failure","payload":{"test":true}}')
            assert code == 202, code
            path = "/api/v1/deliveries/" + publication["delivery_id"]
            if kill:
                wait_until(lambda: receipts(ledger), 5, "first receipt before response")
                assert request(base, path)[1]["status"] == "PROCESSING"
                dispatcher.kill()  # SIGKILL on Linux: no graceful cleanup.
                dispatcher.wait(timeout=5)
                new_api = address()
                base = "http://" + new_api
                dispatcher_args = ["--listen", new_api, "--endpoint", "http://" + receiver_addr + "/webhook",
                                   "--allow-loopback-test", "--db", str(folder / "test.db")]
                dispatcher = start("dispatcher", dispatcher_args)
                wait_until(lambda: ready(base, "/readyz", dispatcher), 5, "restarted dispatcher")
                assert request(base, path)[1]["status"] == "PROCESSING", "lease recovered prematurely"
            target = "DEAD" if exhaust else "SUCCESS"

            def completed():
                ready(base, "/readyz", dispatcher)
                status = request(base, path)[1]
                return status if status["status"] == target else None

            status = wait_until(completed, 40 if kill else 25, target)
            _, attempts = request(base, path + "/attempts")
            entries = receipts(ledger)
            assert all(e["event_id"] == publication["event_id"] and e["delivery_id"] == publication["delivery_id"] for e in entries)
            assert len(attempts) == status["attempts"]
            if kill:
                assert len(entries) == 2 and status["attempts"] == 1, (entries, status)
                assert sum(e["processed"] for e in entries) == 2
                assert attempts[0]["http_status"] == 204
            elif exhaust:
                assert len(entries) == 5 and status["attempts"] == 5
                assert all(a["http_status"] == int(mode) for a in attempts)
            else:
                assert len(entries) == 2 and status["attempts"] == 2, (entries, status)
                expected = 0 if mode in ("drop", "slow") else int(mode)
                assert attempts[0]["http_status"] == expected and attempts[1]["http_status"] == 204
                if mode == "429":
                    assert entries[1]["received_unix_ms"] - entries[0]["received_unix_ms"] >= 1000, "Retry-After ignored"
                if mode in ("drop", "slow"):
                    assert sum(e["processed"] for e in entries) == 2, "duplicate side effect not reproduced"
            print(json.dumps({"scenario": "SIGKILL/restart" if kill else mode + ("/exhaust" if exhaust else ""),
                              "status": status["status"], "persisted_attempts": len(attempts),
                              "receipts": len(entries), "business_effects": sum(e["processed"] for e in entries)}), flush=True)
            if exhaust:
                code, replay = request(base, path + "/replay", b'{"expected_cycle":1,"reason":"receiver recovered"}')
                assert code == 202 and replay["cycle"] == 2
                final = wait_until(lambda: (value if (value := request(base, path)[1])["status"] == "SUCCESS" else None), 5, "replay success")
                _, history = request(base, path + "/attempts")
                _, audit = request(base, path + "/replays")
                assert final["attempts"] == 6 and final["cycle_attempts"] == 1
                assert len(history) == 6 and history[-1]["cycle"] == 2
                assert len(audit) == 1 and audit[0]["actor"] == "local-client"
                assert len(receipts(ledger)) == 6
                print(json.dumps({"scenario": "DEAD/replay", "status": final["status"], "cycle": final["cycle"], "persisted_attempts": len(history), "audit_entries": len(audit)}), flush=True)
        except Exception:
            for log in folder.glob("*.log"):
                print(log.name + ":\n" + log.read_text(), flush=True)
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
    for mode in ("503", "429", "slow", "drop"):
        scenario(mode)
    scenario("503", exhaust=True)
    scenario("slow", kill=True)
