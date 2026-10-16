"""Real processes: two clients, distinct HMAC keys, owned endpoints and restart."""
import json
from pathlib import Path
import subprocess
import tempfile
import urllib.error
import urllib.request
from failures import ROOT, ENV, address, wait_until, ready


def run():
    env = dict(ENV, A_TOKEN="token-A-123456789", B_TOKEN="token-B-123456789",
               A_SECRET="secret-A-123456789", B_SECRET="secret-B-123456789")
    with tempfile.TemporaryDirectory(prefix="hookrelay-tenants-") as tmp:
        folder = Path(tmp)
        api, dest_a, dest_b = address(), address(), address()
        base = "http://" + api
        processes, logs = [], []

        def start(name, args, variables=env):
            log = (folder / f"{name}-{len(processes)}.log").open("wb")
            logs.append(log)
            process = subprocess.Popen([str(ROOT / "bin" / name), *args], env=variables, stdout=log, stderr=log)
            processes.append(process)
            return process

        def call(token, method, path, body=None):
            request = urllib.request.Request(base + path, method=method, data=json.dumps(body).encode() if body is not None else None,
                headers={"Authorization": "Bearer " + token, "Content-Type": "application/json", "Idempotency-Key": "same-key"})
            try:
                with urllib.request.urlopen(request, timeout=2) as response:
                    return response.status, json.load(response)
            except urllib.error.HTTPError as error:
                return error.code, json.load(error)

        cfg = {"clients": [
            {"id": "A", "token_env": "A_TOKEN", "endpoints": [{"id": "ep-A", "url": "http://" + dest_a + "/webhook", "secret_env": "A_SECRET", "allow_loopback_test": True}]},
            {"id": "B", "token_env": "B_TOKEN", "endpoints": [{"id": "ep-B", "url": "http://" + dest_b + "/webhook", "secret_env": "B_SECRET", "allow_loopback_test": True}]}]}
        config = folder / "clients.json"
        config.write_text(json.dumps(cfg))
        args = ["--config", str(config), "--db", str(folder / "test.db"), "--listen", api]
        try:
            for dest, name in ((dest_a, "A"), (dest_b, "B")):
                receiver = start("receiver", ["--listen", dest], dict(env, HOOKRELAY_SECRET=env[name + "_SECRET"]))
                wait_until(lambda: ready("http://" + dest, "/healthz", receiver), 5, "receiver")
            dispatcher = start("dispatcher", args)
            wait_until(lambda: ready(base, "/readyz", dispatcher), 5, "dispatcher")
            results = {}
            for name in ("A", "B"):
                body = {"endpoint_id": "ep-" + name, "event_type": "test.tenant", "payload": {"owner": name}}
                code, publication = call(env[name + "_TOKEN"], "POST", "/api/v1/publish", body)
                assert code == 202, publication
                results[name] = publication
                path = "/api/v1/deliveries/" + publication["delivery_id"]
                wait_until(lambda: call(env[name + "_TOKEN"], "GET", path)[1]["status"] == "SUCCESS", 5, "SUCCESS")
            assert results["A"]["delivery_id"] != results["B"]["delivery_id"]
            foreign = "/api/v1/deliveries/" + results["A"]["delivery_id"]
            for suffix in ("", "/attempts", "/replays", "/replay"):
                code, _ = call(env["B_TOKEN"], "POST" if suffix == "/replay" else "GET", foreign + suffix,
                               {"expected_cycle": 1} if suffix == "/replay" else None)
                assert code == 404, (suffix, code)
            code, _ = call(env["B_TOKEN"], "POST", "/api/v1/publish", {"endpoint_id": "ep-A", "event_type": "test", "payload": {}})
            assert code in (404, 409)  # Existing own key may produce an idempotency conflict first.
            dispatcher.terminate()
            dispatcher.wait(timeout=8)
            new_api = address()
            base = "http://" + new_api
            args = ["--config", str(config), "--db", str(folder / "test.db"), "--listen", new_api]
            dispatcher = start("dispatcher", args)
            wait_until(lambda: ready(base, "/readyz", dispatcher), 5, "restart")
            for name in ("A", "B"):
                code, status = call(env[name + "_TOKEN"], "GET", "/api/v1/deliveries/" + results[name]["delivery_id"])
                assert code == 200 and status["status"] == "SUCCESS"
                _, endpoints = call(env[name + "_TOKEN"], "GET", "/api/v1/endpoints")
                assert endpoints == [{"id": "ep-" + name, "url": "http://" + (dest_a if name == "A" else dest_b) + "/webhook"}]
            print(json.dumps({"scenario": "two clients/restart", "deliveries": 2, "signatures_verified": True,
                              "cross_owner_access": "denied", "idempotency_scope": "per-client"}), flush=True)
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
