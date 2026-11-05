#!/usr/bin/env python3
"""Write a CycloneDX 1.5 SBOM from `go list -m -json all` on stdin."""

import json
import sys


def modules(raw: str) -> list[dict[str, str]]:
    decoder = json.JSONDecoder()
    idx = 0
    components: list[dict[str, str]] = []
    length = len(raw)
    while idx < length:
        while idx < length and raw[idx].isspace():
            idx += 1
        if idx >= length:
            break
        obj, end = decoder.raw_decode(raw, idx)
        idx = end
        if obj.get("Main"):
            continue
        path = obj.get("Path")
        version = obj.get("Version")
        if not path or not version:
            continue
        components.append(
            {
                "type": "library",
                "name": path,
                "version": version,
                "purl": f"pkg:golang/{path}@{version}",
            }
        )
    return components


def main() -> int:
    document = {
        "bomFormat": "CycloneDX",
        "specVersion": "1.5",
        "version": 1,
        "components": modules(sys.stdin.read()),
    }
    json.dump(document, sys.stdout)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
