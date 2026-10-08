#!/usr/bin/env python3
"""Static dashboard checks; does not claim live Grafana/Prometheus verification."""
import json
import re
from pathlib import Path

root = Path(__file__).resolve().parents[1]
dashboard = json.loads((root / "dashboard/egress-watch.json").read_text())
assert dashboard["uid"] == "ebpf-egress-watch"
variables = {v["name"] for v in dashboard["templating"]["list"]}
assert variables == {"DS_PROMETHEUS", "job", "instance", "port"}
ids = set()
for panel in dashboard["panels"]:
    assert panel["id"] not in ids
    ids.add(panel["id"])
    assert panel["datasource"]["uid"] == "${DS_PROMETHEUS}"
    assert panel.get("description")
    for target in panel["targets"]:
        expr = target["expr"]
        assert "conntrack_" not in expr
        assert "job=~\"$job\"" in expr and "instance=~\"$instance\"" in expr
        refs = set(re.findall(r"\$(\w+)", expr))
        assert refs <= variables | {"__rate_interval", "__range"}
print(f"Static dashboard checks passed: {len(ids)} panels")
