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

    certificate = get("/api/certificates/000000000094821")
    if not certificate["is_demo"] or certificate["snapshot"]["recording"]["count"] != 28:
        raise RuntimeError("Unexpected development certificate; run make seed")

    if any(field in certificate for field in ("id", "number", "successor_number", "successor_id")):
        raise RuntimeError("Public certificate exposed an internal certificate identifier")

    current = get("/certificates/000000000094821")
    if current != certificate:
        raise RuntimeError("IMEI public route returned a different certificate")

    state = get("/api/devices/000000000094821")
    if state["record_count"] != 28 or state["status"]["sequence"] != 28:
        raise RuntimeError("Unexpected public lifetime device state")
    history = get("/api/devices/000000000094821/history?limit=2")
    if history["total"] != 28 or len(history["items"]) != 2:
        raise RuntimeError("Unexpected lifetime status history")

    records = get("/api/certificates/000000000094821/records?limit=2")
    if records["total"] != 28 or len(records["items"]) != 2:
        raise RuntimeError("Unexpected evidence page")

    print("API smoke checks passed: health, DB readiness, demo certificate, evidence page")


if __name__ == "__main__":
    main()
