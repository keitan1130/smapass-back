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

    refs = get("/api/devices/search?imeis=000000000094821")["items"]
    if len(refs) != 1:
        raise RuntimeError("Demo device missing; run make seed")
    device_id = refs[0]["id"]
    state = get(f"/api/devices/{device_id}?limit=2")
    if not state["is_demo"] or state["record_count"] != 28 or state["status"]["sequence"] != 28:
        raise RuntimeError("Unexpected live demo state")
    if state["history"]["total"] != 28 or len(state["history"]["items"]) != 2:
        raise RuntimeError("Unexpected history page")
    if any(field in state for field in ("imei", "imeis", "snapshot", "certificate", "ownership_id")):
        raise RuntimeError("Display response exposed IMEI or legacy/private data")
    summaries = get(f"/api/devices?ids={device_id}")["items"]
    if len(summaries) != 1 or "history" in summaries[0] or "imei" in summaries[0]:
        raise RuntimeError("Unexpected batch summaries")
    records = get(f"/api/devices/{device_id}/records?limit=2")
    if records["total"] != 28 or len(records["items"]) != 2:
        raise RuntimeError("Unexpected evidence page")
    print("API smoke checks passed: search, UUID display, history, summaries, evidence")


if __name__ == "__main__":
    main()
