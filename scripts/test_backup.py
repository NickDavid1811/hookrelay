"""Storage edge cases plus a real offline restore/resume scenario."""
from contextlib import closing
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile
import unittest

from backup import snapshot, validate
from failures import ROOT, ENV, address, request, ready, wait_until


class BackupTests(unittest.TestCase):
    def test_committed_wal_and_refuse_overwrite_or_corruption(self):
        with tempfile.TemporaryDirectory() as tmp:
            folder = Path(tmp)
            source = folder / "wal.db"
            # Keep the connection open: committed data stays in WAL during backup.
            with closing(sqlite3.connect(source)) as db:
                db.execute("PRAGMA journal_mode=WAL")
                db.execute("PRAGMA wal_autocheckpoint=0")
                schema = json.loads((ROOT / "scripts/backup_schema.json").read_text())["1"]
                for statement in schema:
                    db.execute(statement)
                db.execute("PRAGMA user_version=1")
                db.execute("INSERT INTO events VALUES('wal-event','test',X'7b7d',0)")
                db.commit()
                self.assertTrue(Path(str(source) + "-wal").exists())
                backup = folder / "backup.db"
                snapshot(source, backup)
                with closing(sqlite3.connect(backup)) as restored:
                    self.assertEqual(restored.execute("SELECT id FROM events").fetchall(), [("wal-event",)])
                    self.assertEqual(restored.execute("PRAGMA journal_mode").fetchone()[0], "delete")
                self.assertEqual(backup.stat().st_mode & 0o777, 0o600)
                original = backup.read_bytes()
                with self.assertRaises(FileExistsError):
                    snapshot(source, backup)
                self.assertEqual(backup.read_bytes(), original)
                with self.assertRaises(FileExistsError):
                    snapshot(source, source)
                Path(str(folder / "occupied.db") + "-wal").write_text("occupied")
                with self.assertRaises(FileExistsError):
                    snapshot(source, folder / "occupied.db")
            corrupt = folder / "corrupt.db"
            corrupt.write_bytes(b"invalid SQLite")
            with self.assertRaises(sqlite3.DatabaseError):
                snapshot(corrupt, folder / "invalid-restore.db")
            self.assertFalse((folder / "invalid-restore.db").exists())
            self.assertFalse(list(folder.glob(".hookrelay-backup-*")))

    def test_corrupt_index_is_rejected_and_good_snapshot_restores(self):
        with tempfile.TemporaryDirectory() as tmp:
            folder = Path(tmp)
            source = folder / "source.db"
            with closing(sqlite3.connect(source)) as db:
                for statement in json.loads((ROOT / "scripts/backup_schema.json").read_text())["5"]:
                    db.execute(statement)
                db.execute("PRAGMA user_version=5")
                page = db.execute("PRAGMA page_size").fetchone()[0]
                root = db.execute("SELECT rootpage FROM sqlite_master WHERE name='eligible'").fetchone()[0]
                db.commit()
            good = folder / "healthy.db"
            snapshot(source, good)
            healthy_bytes = good.read_bytes()
            with source.open("r+b") as file:
                file.seek((root - 1) * page)
                file.write(b"\x00")
            corrupt_bytes = source.read_bytes()
            for operation in ("backup", "restore"):
                rejected = folder / (operation + "-rejected.db")
                result = subprocess.run([os.sys.executable, str(ROOT / "scripts/backup.py"),
                                         operation, str(source), str(rejected)], capture_output=True)
                self.assertEqual(result.returncode, 1)
                self.assertFalse(rejected.exists())
                self.assertEqual(source.read_bytes(), corrupt_bytes)
            self.assertEqual(good.read_bytes(), healthy_bytes)
            restored = folder / "restored.db"
            snapshot(good, restored)
            with closing(sqlite3.connect(restored)) as db:
                validate(db)
            self.assertFalse(list(folder.glob(".hookrelay-backup-*")))

    def test_versioned_schema_objects_and_mutations(self):
        versions = json.loads((ROOT / "scripts/backup_schema.json").read_text())
        for version, statements in versions.items():
            with self.subTest(version=version), closing(sqlite3.connect(":memory:")) as db:
                for statement in statements:
                    db.execute(statement)
                db.execute(f"PRAGMA user_version={int(version)}")
                validate(db)
                db.execute("DROP INDEX eligible")
                with self.assertRaisesRegex(ValueError, "schema object"):
                    validate(db)
        with closing(sqlite3.connect(":memory:")) as db:
            for statement in versions["5"]:
                db.execute(statement)
            db.execute("PRAGMA user_version=5")
            db.execute("DROP TRIGGER endpoint_owner_update")
            db.execute("CREATE TRIGGER endpoint_owner_update BEFORE UPDATE ON endpoints BEGIN SELECT 1; END")
            with self.assertRaisesRegex(ValueError, "schema object"):
                validate(db)

    def test_rejects_fake_supported_schema(self):
        with closing(sqlite3.connect(":memory:")) as db:
            for table in ("events", "deliveries", "keys", "attempts", "replays", "clients", "endpoints"):
                db.execute(f"CREATE TABLE {table}(id TEXT)")
            db.execute("PRAGMA user_version=5")
            with self.assertRaisesRegex(ValueError, "schema object"):
                validate(db)

    def test_rejects_future_schema_and_broken_references(self):
        with tempfile.TemporaryDirectory() as tmp:
            folder = Path(tmp)
            source = folder / "invalid.db"
            with closing(sqlite3.connect(source)) as db:
                db.executescript("""CREATE TABLE events(id TEXT PRIMARY KEY);
                    CREATE TABLE deliveries(id TEXT PRIMARY KEY, event_id TEXT REFERENCES events(id));
                    CREATE TABLE keys(key TEXT PRIMARY KEY);
                    CREATE TABLE attempts(number INTEGER);
                    PRAGMA user_version=99;""")
            with self.assertRaises(ValueError):
                snapshot(source, folder / "future.db")
            with closing(sqlite3.connect(source)) as db:
                db.execute("PRAGMA user_version=1")
                db.execute("INSERT INTO deliveries VALUES('delivery','missing-event')")
                db.commit()
            with self.assertRaisesRegex(ValueError, "foreign key check"):
                snapshot(source, folder / "broken.db")
            self.assertFalse((folder / "future.db").exists())
            self.assertFalse((folder / "broken.db").exists())

    def test_offline_restore_resumes_replay_and_preserves_idempotency(self):
        with tempfile.TemporaryDirectory(prefix="hookrelay-restore-") as tmp:
            folder = Path(tmp)
            api, receiver_addr = address(), address()
            base = "http://" + api
            dbpath = folder / "source.db"
            logs, processes = [], []

            def start(name, args):
                log = (folder / f"{name}-{len(processes)}.log").open("wb")
                logs.append(log)
                process = subprocess.Popen([str(ROOT / "bin" / name), *args], env=ENV, stdout=log, stderr=log)
                processes.append(process)
                return process

            def dispatcher(path):
                process = start("dispatcher", ["--listen", api, "--endpoint", "http://" + receiver_addr + "/webhook",
                    "--allow-loopback-test", "--db", str(path)])
                wait_until(lambda: ready(base, "/readyz", process), 5, "dispatcher")
                return process

            try:
                receiver = start("receiver", ["--listen", receiver_addr, "--mode", "503", "--fail-first", "5"])
                wait_until(lambda: ready("http://" + receiver_addr, "/healthz", receiver), 5, "receiver")
                process = dispatcher(dbpath)
                payload = b'{"endpoint_id":"test","event_type":"test.restore","payload":{"restored":true}}'
                code, publication = request(base, "/api/v1/publish", payload)
                self.assertEqual(code, 202)
                path = "/api/v1/deliveries/" + publication["delivery_id"]
                wait_until(lambda: request(base, path)[1]["status"] == "DEAD", 25, "DEAD")
                _, history_before = request(base, path + "/attempts")
                # Stop the consumer to keep the replay unfinished in the backup.
                receiver.terminate()
                receiver.wait(timeout=5)
                code, replay = request(base, path + "/replay", b'{"expected_cycle":1,"reason":"restore test"}')
                self.assertEqual(code, 202)
                process.terminate()
                process.wait(timeout=8)
                with closing(sqlite3.connect(dbpath)) as db:
                    before = {table: db.execute("SELECT * FROM " + table + " ORDER BY rowid").fetchall()
                              for table in ("events", "deliveries", "keys", "attempts", "replays", "clients", "endpoints")}
                    self.assertIn(before["deliveries"][0][2], ("PENDING", "RETRYING", "PROCESSING"))
                backup = folder / "snapshot.db"
                restored = folder / "restored.db"
                for operation, source, destination in (("backup", dbpath, backup), ("restore", backup, restored)):
                    subprocess.run([os.sys.executable, str(ROOT / "scripts" / "backup.py"), operation,
                                    str(source), str(destination)], check=True)
                with closing(sqlite3.connect(restored)) as db:
                    after = {table: db.execute("SELECT * FROM " + table + " ORDER BY rowid").fetchall()
                             for table in before}
                    self.assertEqual(before, after)
                new_receiver_addr = address()
                new_api = address()
                base = "http://" + new_api
                with closing(sqlite3.connect(restored)) as db:
                    db.execute("UPDATE endpoints SET url = ? WHERE id = 'test'", ("http://" + new_receiver_addr + "/webhook",))
                    db.commit()
                receiver = start("receiver", ["--listen", new_receiver_addr])
                wait_until(lambda: ready("http://" + new_receiver_addr, "/healthz", receiver), 5, "restored receiver")
                process = start("dispatcher", ["--listen", new_api, "--endpoint", "http://" + new_receiver_addr + "/webhook",
                    "--allow-loopback-test", "--db", str(restored)])
                wait_until(lambda: ready(base, "/readyz", process), 5, "restored dispatcher")
                wait_until(lambda: request(base, path)[1]["status"] == "SUCCESS", 40, "restored SUCCESS")
                _, final = request(base, path)
                _, history = request(base, path + "/attempts")
                _, audit = request(base, path + "/replays")
                self.assertEqual(history[:5], history_before)
                self.assertEqual(final["cycle"], 2)
                self.assertGreaterEqual(final["attempts"], 6)
                self.assertEqual(len(audit), 1)
                code, duplicate = request(base, "/api/v1/publish", payload)
                self.assertEqual(code, 202)
                self.assertEqual(duplicate["delivery_id"], publication["delivery_id"])
                print(json.dumps({"scenario": "offline backup/restore", "status": final["status"],
                    "cycle": final["cycle"], "attempts": final["attempts"], "audit_entries": len(audit),
                    "all_tables_preserved": True}), flush=True)
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
    unittest.main(verbosity=2)
