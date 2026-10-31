"""Check a running development API after `make seed`, using Python's standard library."""

import json
import sys
import urllib.request


def main():
    base_url = (sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8080").rstrip("/")

    def get(path):
        with urllib.request.urlopen(base_url + path, timeout=5) as response:
            return json.load(response)

    for path in ("/health", "/ready"):
        if get(path) != {"status": "ok"}:
            raise RuntimeError(f"Unexpected health response: {path}")

    certificate = get("/api/certificates/01ABCDEFGH")
    if not certificate["is_demo"] or certificate["snapshot"]["recording"]["count"] != 28:
        raise RuntimeError("Unexpected development certificate; run make seed")

    records = get("/api/certificates/01ABCDEFGH/records?limit=2")
    if records["total"] != 28 or len(records["items"]) != 2:
        raise RuntimeError("Unexpected evidence page")

    print("API smoke checks passed: health, DB readiness, demo certificate, evidence page")


if __name__ == "__main__":
    main()
