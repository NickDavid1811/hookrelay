"""SQLite snapshot/restore utility. Stop the dispatcher before operational use."""
import argparse
import json
import re
from contextlib import closing
import os
from pathlib import Path
import sqlite3
import tempfile


def validate(db):
    if db.execute("PRAGMA integrity_check").fetchall() != [("ok",)]:
        raise ValueError("SQLite integrity check failed")
    if db.execute("PRAGMA foreign_key_check").fetchone() is not None:
        raise ValueError("foreign key check failed")
    version = db.execute("PRAGMA user_version").fetchone()[0]
    if version not in (1, 2, 3, 4, 5):
        raise ValueError(f"unsupported HookRelay schema: {version}")
    # Versioned DDL is a checked-in contract, not inferred from the input database.
    schemas = json.loads(Path(__file__).with_name("backup_schema.json").read_text())
    with closing(sqlite3.connect(":memory:")) as expected:
        for statement in schemas[str(version)]:
            expected.execute(statement)
        def objects(connection):
            return {(kind, name): re.sub(r"\s+", " ", sql.strip())
                    for kind, name, sql in connection.execute(
                        "SELECT type,name,sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%'")}
        actual, required = objects(db), objects(expected)
        for key, definition in required.items():
            if actual.get(key) != definition:
                raise ValueError(f"invalid HookRelay schema object: {key[0]} {key[1]}")


def snapshot(source, destination):
    """Backup API includes committed WAL data; publish without replacing any file."""
    source = Path(source).resolve(strict=True)
    destination = Path(destination).absolute()
    if not source.is_file():
        raise ValueError("source must be an existing database file")
    if destination.exists() or destination.is_symlink():
        raise FileExistsError("destination already exists; restore to a new path")
    for suffix in ("-wal", "-shm", "-journal"):
        if Path(str(destination) + suffix).exists():
            raise FileExistsError("destination has SQLite sidecar files")
    fd, temporary = tempfile.mkstemp(prefix=".hookrelay-backup-", dir=destination.parent)
    os.close(fd)
    temporary = Path(temporary)
    try:
        with closing(sqlite3.connect(source.as_uri() + "?mode=ro", uri=True, timeout=5)) as src:
            validate(src)
            with closing(sqlite3.connect(temporary, timeout=5)) as dst:
                src.backup(dst, pages=256, sleep=0.05)
                # Make the snapshot independently usable, without WAL sidecars.
                dst.execute("PRAGMA journal_mode=DELETE")
                validate(dst)
        with temporary.open("rb") as file:
            os.fsync(file.fileno())
        # link is atomic and fails if another process creates destination meanwhile.
        os.link(temporary, destination)
        directory_fd = os.open(destination.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)
    finally:
        temporary.unlink(missing_ok=True)
        for suffix in ("-wal", "-shm", "-journal"):
            Path(str(temporary) + suffix).unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=("backup", "restore"))
    parser.add_argument("source", type=Path)
    parser.add_argument("destination", type=Path)
    args = parser.parse_args()
    try:
        snapshot(args.source, args.destination)
    except (OSError, ValueError, sqlite3.Error) as error:
        parser.exit(1, f"{args.operation} failed: {error}\n")
    print(f"{args.operation} complete: {args.destination}")


if __name__ == "__main__":
    main()
