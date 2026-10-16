"""Run filesystem exhaustion, EIO or interrupted WAL tests in a disposable tmpfs.

Requires Go and Docker Engine. Never fills the workspace or the host filesystem.
"""
import argparse
import os
from pathlib import Path
import subprocess
import tempfile
import uuid

ROOT = Path(__file__).resolve().parents[1]
IMAGE = "alpine:3.24.0@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--suite", choices=("full", "io"), default="full")
    args = parser.parse_args()
    pattern = "^TestFilesystemFull" if args.suite == "full" else "^TestFilesystem(IOFailures|InterruptedWAL)$"
    subprocess.run(["docker", "info", "--format", "{{.ServerVersion}}"], check=True)
    with tempfile.TemporaryDirectory(prefix="hookrelay-storage-full-") as tmp:
        binary = Path(tmp) / "storage-tests"
        subprocess.run(["go", "test", "-c", "-o", str(binary), "./internal/relay"],
                       cwd=ROOT, env=dict(os.environ, CGO_ENABLED="0", GOOS="linux"), check=True)
        binary.chmod(0o555)
        name = "hookrelay-storage-full-" + uuid.uuid4().hex[:12]
        try:
            subprocess.run([
                "docker", "run", "--rm", "--name", name, "--read-only", "--network", "none",
                "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
                "--pids-limit", "64", "--memory", "256m", "--user", "10001:10001",
                "--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=4m,uid=10001,gid=10001",
                "--mount", f"type=bind,source={binary},target=/storage-tests,readonly",
                "--env", "HOOKRELAY_TEST_FULL_FS=1",
                "--env", "HOOKRELAY_TEST_STORAGE_FAULTS=1", IMAGE, "/storage-tests",
                f"-test.run={pattern}", "-test.v", "-test.timeout=45s",
            ], check=True, timeout=90)
        finally:
            subprocess.run(["docker", "container", "rm", "-f", name],
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)


if __name__ == "__main__":
    main()
