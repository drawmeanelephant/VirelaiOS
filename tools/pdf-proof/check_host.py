"""Independent host diagnostic comparison; missing pages fail, never skip."""
import json
import sys
from pathlib import Path

from compare import compare_files
from corpus import FIXTURES, ROOT


def check():
    failures, outcomes = [], []
    refs = json.loads((FIXTURES/"oracle.json").read_text())["references"]
    for row in refs:
        name = row["id"]+".bgra"
        try:
            result = compare_files(ROOT/"artifacts/m89-acceptance/reference"/name,
                                   ROOT/"artifacts/m89-acceptance/host"/name, row)
            outcomes.append({"id": row["id"], "comparison": result})
        except (OSError, ValueError) as exc:
            failures.append({"id": row["id"], "failure": str(exc)})
    (ROOT/"artifacts/m89-acceptance/host-comparison.json").write_text(
        json.dumps({"passed": outcomes, "failed": failures}, indent=2)+"\n")
    for failure in failures:
        print("FAIL "+failure["id"]+": "+failure["failure"])
    print(f"independent comparisons: {len(outcomes)}/{len(refs)}")
    return not failures


if __name__ == "__main__":
    sys.exit(0 if check() else 1)
