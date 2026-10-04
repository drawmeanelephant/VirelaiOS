import hashlib
import json
from pathlib import Path
import subprocess
import unittest

FIXTURES = Path(__file__).resolve().parents[2] / "tests/fixtures/js"


class GoldenTests(unittest.TestCase):
    def test_pinned_independent_host_outputs(self):
        lock = json.loads((FIXTURES / "goldens.json").read_text())
        self.assertEqual(subprocess.check_output(["node", "--version"], text=True).strip(),
                         lock["engine"].removeprefix("Node.js "))
        for filename, digest in lock["sha256"].items():
            self.assertEqual(hashlib.sha256((FIXTURES / filename).read_bytes()).hexdigest(), digest)
        for name, source in (("file", "file.js"), ("refused", "refused.js")):
            actual = subprocess.check_output(["node", "-e", lock["wrapper"], str(FIXTURES / source)])
            self.assertEqual(actual, (FIXTURES / (name + ".stdout")).read_bytes())


if __name__ == "__main__":
    unittest.main()
