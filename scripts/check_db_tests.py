#!/usr/bin/env python3
"""Verify required tests from Go JSON events, including descendants and packages."""
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent

def verify(events, required):
    terminals = {}
    packages = {}
    bad = []
    for event in events:
        action = event.get("Action")
        if action not in ("pass", "fail", "skip"):
            continue
        package = event.get("Package", "")
        name = event.get("Test")
        if name:
            terminals[(package, name)] = action
            if action == "fail":
                bad.append(f"failed: {package}/{name}")
        else:
            packages[package] = action
            if action == "fail":
                bad.append(f"package {action}: {package}")
    for item in required:
        key = (item["package"], item["test"])
        if terminals.get(key) != "pass":
            bad.append(f"required test {terminals.get(key, 'missing')}: {key[0]}/{key[1]}")
        if packages.get(key[0]) != "pass":
            bad.append(f"required package did not complete: {key[0]}")
        for (package, name), action in terminals.items():
            if package == key[0] and name.startswith(key[1] + "/") and action != "pass":
                bad.append(f"required subtest {action}: {package}/{name}")
    return terminals, bad

if __name__ == "__main__":
    required = json.loads((ROOT / "required_db_tests.json").read_text())
    discovered = set()
    for path in (ROOT.parent / "backend" / "internal").rglob("*integration_test.go"):
        package = "backend/" + str(path.parent.relative_to(ROOT.parent / "backend"))
        for name in re.findall(r"^func (Test\w+)\(t \*testing.T\)", path.read_text(), re.MULTILINE):
            discovered.add((package, name))
    listed = {(item["package"], item["test"]) for item in required}
    if not listed or listed != discovered or len(listed) != len(required):
        sys.exit("required database test manifest is empty, duplicated, or differs from integration test sources")
    with open(sys.argv[1]) as stream:
        events = [json.loads(line) for line in stream if line.strip()]
    terminals, errors = verify(events, required)
    print(json.dumps({"required": len(required), "passed": sum(v == "pass" for v in terminals.values()),
                      "failed": sum(v == "fail" for v in terminals.values()),
                      "skipped": sum(v == "skip" for v in terminals.values())}))
    for error in errors:
        print(error, file=sys.stderr)
    sys.exit(bool(errors))
