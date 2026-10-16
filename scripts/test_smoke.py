"""A failed child must never be mistaken for an already-running healthy service."""
import subprocess
import sys
import unittest
from unittest.mock import patch

from smoke import ready


class SmokeIsolationTests(unittest.TestCase):
    def test_dead_child_does_not_probe_another_service(self):
        process = subprocess.Popen([sys.executable, "-c", "raise SystemExit(1)"])
        process.wait(timeout=5)
        with patch("smoke.OPENER.open") as request:
            with self.assertRaisesRegex(AssertionError, "exited early"):
                ready("http://127.0.0.1:8080", "/readyz", process)
            request.assert_not_called()


if __name__ == "__main__":
    unittest.main(verbosity=2)
