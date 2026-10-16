"""Real lab processes + HTTP: scenarios, receiver evidence, export and replay."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import urllib.request
import urllib.error
from failures import ROOT, address


def run():
    with tempfile.TemporaryDirectory(prefix="hookrelay-playground-") as tmp:
        folder = Path(tmp)
        ui, receiver = address(), address()
        base = "http://" + ui
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        def request(path, body=None, marker=True):
            headers = {"Content-Type": "application/json"}
            if marker:
                headers["X-Playground-Request"] = "1"
            req = urllib.request.Request(base + path, data=json.dumps(body).encode() if body is not None else None, headers=headers)
            try:
                with opener.open(req, timeout=5) as response:
                    raw = response.read()
                    return response.status, json.loads(raw) if path.startswith('/play/api/') else raw
            except urllib.error.HTTPError as error:
                return error.code, error.read().decode()
        def wait(check, timeout=30):
            until = time.monotonic() + timeout
            while time.monotonic() < until:
                if process.poll() is not None:
                    raise AssertionError('playground exited')
                value = check()
                if value:
                    return value
                time.sleep(.15)
            raise AssertionError('scenario timed out')
        with (folder / 'lab.log').open('wb') as log:
            process = subprocess.Popen([str(ROOT / 'bin/playground'), '--listen', ui, '--receiver-listen', receiver,
                '--data-dir', str(folder / 'data'), '--web-dir', str(ROOT / 'web/playground/dist')], stdout=log, stderr=log)
            try:
                def ready():
                    try:
                        return request('/play/api/state')[0] == 200
                    except urllib.error.URLError:
                        return False
                wait(ready, 10)
                assert request('/')[0] == 200
                assert request('/play/api/publish', {}, marker=False)[0] == 403
                ids = {}
                for scenario in ('success', 'slow', 'temporary', 'rate_limit', 'drop', 'exhausted'):
                    code, publication = request('/play/api/publish', {'scenario': scenario, 'event_type': 'test.' + scenario, 'payload': {'test': True}, 'count': 1})
                    assert code == 202, publication
                    ids[scenario] = publication['ids'][0]
                def all_terminal():
                    snapshot = request('/play/api/state')[1]
                    return snapshot if len(snapshot['runs']) == 6 and all(r['status'] in ('SUCCESS','DEAD') for r in snapshot['runs']) else None
                state = wait(all_terminal, 45)
                runs = {r['scenario']: r for r in state['runs']}
                assert all(runs[s]['status'] == 'SUCCESS' for s in ids if s != 'exhausted')
                assert runs['exhausted']['status'] == 'DEAD' and runs['exhausted']['attempts'] == 5
                assert runs['temporary']['attempts'] == 3 and runs['rate_limit']['attempts'] == 2
                assert sum(r['processed'] for r in runs['drop']['receipts']) == 2
                rate = runs['rate_limit']['receipts']
                assert rate[1]['at'] - rate[0]['at'] >= 3000
                exhausted = runs['exhausted']
                code, _ = request('/play/api/replay/' + exhausted['id'], {'expected_cycle': 1})
                assert code == 202
                def replayed():
                    current = request('/play/api/state')[1]
                    return current if any(r['id']==exhausted['id'] and r['status']=='SUCCESS' and r['cycle']==2 for r in current['runs']) else None
                final = wait(replayed, 8)
                code, exported = request('/play/api/export')
                assert code == 200 and len(exported['runs']) == 6
                text = json.dumps(exported)
                assert 'secret_env' not in text and 'Bearer ' not in text
                print(json.dumps({'scenario':'playground six scenarios/replay/export', 'runs':6, 'duplicate_effects':2, 'dead_replayed':True}), flush=True)
            except Exception:
                log.flush()
                print((folder / 'lab.log').read_text())
                raise
            finally:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)


if __name__ == '__main__':
    run()
