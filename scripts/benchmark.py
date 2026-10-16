"""Linux local baseline, not a production throughput guarantee. No extra Python packages."""
import argparse
from concurrent.futures import ThreadPoolExecutor
from contextlib import closing
from datetime import datetime, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import sqlite3
import subprocess
import tempfile
import threading
import time
import urllib.request
import urllib.error

from failures import ROOT, address, ready, wait_until

ENV = dict(os.environ, HOOKRELAY_TOKEN="benchmark-token-local", HOOKRELAY_SECRET="benchmark-secret-local",
           HOOKRELAY_METRICS_TOKEN="benchmark-metrics-local")
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def stats(values):
    ordered = sorted(values)
    if not ordered:
        return {"count": 0}
    def percentile(p):
        return ordered[max(0, math.ceil(len(ordered) * p) - 1)]
    return {"count": len(values), "min_ms": ordered[0] * 1000, "mean_ms": sum(values) / len(values) * 1000,
            "p50_ms": percentile(.5) * 1000, "p95_ms": percentile(.95) * 1000,
            "p99_ms": percentile(.99) * 1000, "max_ms": ordered[-1] * 1000}


def proc_stats(pid):
    raw = Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()
    cpu = (int(raw[11]) + int(raw[12])) / os.sysconf("SC_CLK_TCK")
    rss = int(Path(f"/proc/{pid}/statm").read_text().split()[1]) * os.sysconf("SC_PAGE_SIZE")
    return cpu, rss


def disk_stats(path):
    result = {}
    for suffix in ("", "-wal", "-shm"):
        file = Path(str(path) + suffix)
        if file.exists():
            st = file.stat()
            result["db" + suffix] = {"logical_bytes": st.st_size, "allocated_bytes": st.st_blocks * 512}
    result["total_logical_bytes"] = sum(v["logical_bytes"] for v in result.values())
    result["total_allocated_bytes"] = sum(v["allocated_bytes"] for v in result.values() if isinstance(v, dict))
    return result


def command(args):
    try:
        return subprocess.run(args, capture_output=True, text=True, timeout=10, check=True, cwd=ROOT).stdout.strip()
    except (OSError, subprocess.SubprocessError):
        return "unavailable"


def scrape(base):
    req = urllib.request.Request(base + "/metrics", headers={"Authorization": "Bearer " + ENV["HOOKRELAY_METRICS_TOKEN"]})
    start = time.perf_counter()
    with OPENER.open(req, timeout=5) as response:
        text = response.read().decode()
        if response.status != 200:
            raise RuntimeError("scrape failed")
    return time.perf_counter() - start, text


def metric(text, name):
    for line in text.splitlines():
        if line.startswith(name + " "):
            return float(line.split()[1])
    raise AssertionError(f"missing metric {name}")


def run_case(args, concurrency):
    with tempfile.TemporaryDirectory(prefix="run-", dir=args.data_dir) as tmp:
        folder = Path(tmp)
        db = folder / "benchmark.db"
        api, receiver_addr = address(), address()
        base = "http://" + api
        processes, logs = [], []
        stop = threading.Event()
        sample_errors, scrape_errors, periodic_scrapes = [], [], []
        resource = {"dispatcher": {"peak_rss_bytes": 0}, "receiver": {"peak_rss_bytes": 0}}
        queue_samples = []
        sampler = scraper = None

        def start(name, cli):
            log = (folder / (name + ".log")).open("wb")
            logs.append(log)
            process = subprocess.Popen([str(ROOT / "bin" / name), *cli], env=ENV, stdout=log, stderr=log)
            processes.append(process)
            return process

        try:
            receiver = start("receiver", ["--listen", receiver_addr])
            wait_until(lambda: ready("http://" + receiver_addr, "/healthz", receiver), 5, "receiver")
            dispatcher = start("dispatcher", ["--listen", api, "--endpoint", "http://" + receiver_addr + "/webhook",
                "--allow-loopback-test", "--db", str(db), "--max-queue", str(args.events + 10),
                "--max-client-queue", str(args.events + 10), "--max-events", str(args.events + 10),
                "--max-payload-bytes", str((args.events + 10) * args.payload_bytes)])
            # failures.ready uses its own bearer only for readyz, which is public.
            wait_until(lambda: ready(base, "/readyz", dispatcher), 5, "dispatcher")
            _, initial = scrape(base)
            assert metric(initial, "hookrelay_queue_depth") == 0
            payload = json.dumps({"data": "x" * (args.payload_bytes - 11)}, separators=(",", ":")).encode()
            assert len(payload) == args.payload_bytes
            publication = b'{"endpoint_id":"test","event_type":"benchmark.sent","payload":' + payload + b'}'
            prefix = os.urandom(16).hex()
            results, latencies = [], []
            initial_cpu = {name: proc_stats(process.pid)[0] for name, process in (("dispatcher", dispatcher), ("receiver", receiver))}

            def sample():
                try:
                    with closing(sqlite3.connect(db.as_uri() + "?mode=ro", uri=True, timeout=2)) as conn:
                        while not stop.is_set():
                            for name, process in (("dispatcher", dispatcher), ("receiver", receiver)):
                                _, rss = proc_stats(process.pid)
                                resource[name]["peak_rss_bytes"] = max(resource[name]["peak_rss_bytes"], rss)
                            row = conn.execute("SELECT count(*),min(e.created) FROM deliveries d JOIN events e ON e.id=d.event_id WHERE d.status IN ('PENDING','RETRYING','PROCESSING')").fetchone()
                            queue_samples.append({"depth": row[0], "oldest_age_seconds": max(0, time.time() - row[1] / 1000) if row[1] else 0})
                            stop.wait(.1)
                except Exception as error:
                    sample_errors.append(str(error))

            def scrape_loop():
                while not stop.wait(args.scrape_interval):
                    try:
                        latency, _ = scrape(base)
                        periodic_scrapes.append(latency)
                    except Exception as error:
                        scrape_errors.append(str(error))

            sampler = threading.Thread(target=sample, daemon=True)
            sampler.start()
            if args.scrape_interval > 0:
                scraper = threading.Thread(target=scrape_loop, daemon=True)
                scraper.start()

            def publish(index):
                req = urllib.request.Request(base + "/api/v1/publish", data=publication, headers={
                    "Authorization": "Bearer " + ENV["HOOKRELAY_TOKEN"], "Content-Type": "application/json",
                    "Idempotency-Key": prefix + "-" + str(index)})
                start = time.perf_counter()
                try:
                    with OPENER.open(req, timeout=10) as response:
                        value = json.load(response)
                        return response.status, time.perf_counter() - start, value
                except urllib.error.HTTPError as error:
                    return error.code, time.perf_counter() - start, {"error": "HTTP rejection"}
                except Exception as error:
                    return 0, time.perf_counter() - start, {"error": type(error).__name__}

            begin = time.perf_counter()
            with ThreadPoolExecutor(max_workers=concurrency) as pool:
                results = list(pool.map(publish, range(args.events)))
            publication_seconds = time.perf_counter() - begin
            codes = {}
            for code, latency, _ in results:
                codes[str(code)] = codes.get(str(code), 0) + 1
                latencies.append(latency)
            if codes != {"202": args.events}:
                raise AssertionError(f"publication failures: {codes}")
            ids = [result[2]["delivery_id"] for result in results]
            assert len(set(ids)) == args.events
            deadline = time.monotonic() + args.timeout
            with closing(sqlite3.connect(db.as_uri() + "?mode=ro", uri=True, timeout=2)) as conn:
                while True:
                    counts = dict(conn.execute("SELECT status,count(*) FROM deliveries GROUP BY status").fetchall())
                    if counts == {"SUCCESS": args.events}:
                        break
                    if counts.get("DEAD", 0) or time.monotonic() > deadline:
                        raise AssertionError(f"queue did not drain successfully: {counts}")
                    time.sleep(.1)
                total_seconds = time.perf_counter() - begin
                # Millisecond UTC timestamps, approximate publication-to-finalized latency.
                delivery_latencies = [row[0] / 1000 for row in conn.execute("SELECT d.terminal_at-e.created FROM deliveries d JOIN events e ON e.id=d.event_id")]
                attempts = dict(conn.execute("SELECT http_status,count(*) FROM attempts GROUP BY http_status").fetchall())
                schema = conn.execute("PRAGMA user_version").fetchone()[0]
                sql_version = conn.execute("SELECT sqlite_version()").fetchone()[0]
            assert attempts == {204: args.events}, attempts
            stop.set()
            sampler.join(timeout=5)
            if scraper:
                scraper.join(timeout=6)
            if sample_errors or scrape_errors:
                raise AssertionError({"sampler": sample_errors, "scrapes": scrape_errors})
            for name, process in (("dispatcher", dispatcher), ("receiver", receiver)):
                resource[name]["cpu_seconds"] = proc_stats(process.pid)[0] - initial_cpu[name]
                resource[name]["mean_cpu_percent_one_core"] = resource[name]["cpu_seconds"] / total_seconds * 100
            retained_scrapes = [scrape(base)[0] for _ in range(args.scrape_samples)]
            _, final = scrape(base)
            assert metric(final, "hookrelay_queue_depth") == 0
            assert metric(final, "hookrelay_events_retained") == args.events
            assert metric(final, "hookrelay_payload_bytes_retained") == args.events * args.payload_bytes
            active_disk = disk_stats(db)
            dispatcher.terminate()
            dispatcher.wait(timeout=8)
            closed_disk = disk_stats(db)
            with closing(sqlite3.connect(db.as_uri() + "?mode=ro", uri=True)) as conn:
                assert conn.execute("PRAGMA integrity_check").fetchone() == ("ok",)
                assert conn.execute("PRAGMA foreign_key_check").fetchone() is None
            return {
                "publication_concurrency": concurrency, "outbound_workers": 1,
                "events": args.events, "payload_bytes_per_event": args.payload_bytes,
                "publication_body_bytes": len(publication), "publication_codes": codes,
                "publication_seconds": publication_seconds, "accepted_per_second": args.events / publication_seconds,
                "publication_http_latency": stats(latencies), "drain_seconds_after_publication": total_seconds - publication_seconds,
                "total_seconds_until_drained": total_seconds, "successes_per_second_until_drained": args.events / total_seconds,
                "delivery_publication_to_terminal_latency": stats(delivery_latencies), "attempt_codes": attempts,
                "queue_sample_peak": max((x["depth"] for x in queue_samples), default=0),
                "queue_sample_oldest_age_peak_seconds": max((x["oldest_age_seconds"] for x in queue_samples), default=0),
                "resource_sample_interval_seconds": .1, "queue_sample_count": len(queue_samples), "resources": resource,
                "scrape_interval_seconds": args.scrape_interval, "scrape_during_load_latency": stats(periodic_scrapes),
                "scrape_after_drain_latency": stats(retained_scrapes), "disk_before_shutdown": active_disk,
                "disk_after_graceful_shutdown": closed_disk, "python_sqlite_version": sql_version,
                "schema_version": schema, "assertions": "all publications accepted; one finalized HTTP 204 attempt per event; integrity/FK valid",
            }
        except Exception:
            for log in folder.glob("*.log"):
                print(log.name + ":\n" + log.read_text()[-4000:], flush=True)
            raise
        finally:
            stop.set()
            for thread in (sampler, scraper):
                if thread:
                    thread.join(timeout=6)
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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--events", type=int, default=200)
    parser.add_argument("--concurrency", type=int, nargs="+", default=[1, 8])
    parser.add_argument("--payload-bytes", type=int, default=1024)
    parser.add_argument("--scrape-interval", type=float, default=1)
    parser.add_argument("--scrape-samples", type=int, default=20)
    parser.add_argument("--timeout", type=float, default=120)
    parser.add_argument("--data-dir", type=Path, default=ROOT / "work" / "benchmarks")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if platform.system() != "Linux":
        parser.error("Linux /proc is required for CPU/RSS sampling")
    if not 1 <= args.events <= 10000 or not 11 <= args.payload_bytes <= 60000 or any(c < 1 or c > 64 for c in args.concurrency) or not math.isfinite(args.scrape_interval) or args.scrape_interval < 0 or args.scrape_samples < 1 or not math.isfinite(args.timeout) or args.timeout <= 0:
        parser.error("invalid or excessive benchmark arguments")
    for name in ("dispatcher", "receiver"):
        if not (ROOT / "bin" / name).is_file():
            parser.error("run make build first")
    if args.output.exists():
        parser.error("output already exists; choose a new report path")
    args.data_dir.mkdir(parents=True, exist_ok=True)
    args.data_dir = args.data_dir.resolve()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    cpu_model = next((line.split(":", 1)[1].strip() for line in Path("/proc/cpuinfo").read_text().splitlines() if line.startswith("model name")), "unknown")
    memory = next(line for line in Path("/proc/meminfo").read_text().splitlines() if line.startswith("MemTotal:"))
    report = {"recorded_at_utc": datetime.now(timezone.utc).isoformat(), "git_commit": command(["git", "rev-parse", "HEAD"]),
        "git_dirty": bool(command(["git", "status", "--porcelain"])),
        "generator_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "binary_build_info": {name: command(["go", "version", "-m", str(ROOT / "bin" / name)]).replace(str(ROOT), "<project>").replace(str(Path.home()), "<home>") for name in ("dispatcher", "receiver")},
        "binary_sha256": {name: hashlib.sha256((ROOT / "bin" / name).read_bytes()).hexdigest() for name in ("dispatcher", "receiver")},
        "environment": {"kernel": platform.release(), "machine": platform.machine(), "cpu_model": cpu_model,
            "logical_cpus": os.cpu_count(), "allowed_cpus": len(os.sched_getaffinity(0)), "memory": memory,
            "go": command(["go", "version"]), "python": platform.python_version(),
            "sqlite_driver": next(line.strip() for line in (ROOT / "go.mod").read_text().splitlines() if "modernc.org/sqlite " in line),
            "filesystem": command(["findmnt", "-T", str(args.data_dir), "-o", "FSTYPE", "-n"]),
            "data_directory": ("<project>/" + args.data_dir.relative_to(ROOT).as_posix()) if args.data_dir.is_relative_to(ROOT) else "<benchmark-data>", "durability": "WAL + synchronous FULL",
            "receiver": "Go test receiver, HMAC verified, 204, no artificial delay; request logging enabled",
            "http": "Python urllib, new connections; API loopback; outgoing keepalive disabled; no retries by load generator"},
        "measurement_notes": ["Paths anonymized; filesystem type retained without device or subvolume identifiers.",
            "Single run per concurrency; not a production SLO or maximum capacity.",
            "Generator, receiver, dispatcher and sampler share one machine; caches and ambient load uncontrolled.",
            "CPU percentages use one core=100%; RSS/queue peaks sampled, not absolute peaks.",
            "Read-only SQLite queue polling every 0.1s adds observer load; periodic scrapes are additional load.",
            "Go worker sends eligible events consecutively; polls every 0.1s only when no work is eligible.",
            "Drain completion sampled every 0.1s; end-to-end latency uses wall-clock milliseconds from SQLite.",
            "Disk reports apparent size and st_blocks allocation, not write amplification nor complete Btrfs physical accounting.",
            "Publication throughput is a short burst rate; sustained publication capacity not measured.",
            "Scrapes after drain read a retained dataset; no inference about much larger datasets."], "cases": []}
    for concurrency in args.concurrency:
        result = run_case(args, concurrency)
        report["cases"].append(result)
        print(json.dumps(result), flush=True)
    with args.output.open("x") as file:
        json.dump(report, file, indent=2)
        file.write("\n")
    print(f"Report saved: {args.output}", flush=True)


if __name__ == "__main__":
    main()
