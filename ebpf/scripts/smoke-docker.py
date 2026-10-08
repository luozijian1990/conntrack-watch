#!/usr/bin/env python3
"""Real Linux BPF/container smoke test. Uses only owned containers/networks.

Prerequisites: build/egress-watch matching Docker's Linux architecture,
build/egress.bpf.o, conntrack-watch-egress-toolchain:dev and python:3.12-alpine.
Requires Docker and privileged containers. No host ports are published.
"""
import json
import os
import subprocess
import time
import uuid
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ARTIFACTS = ROOT / "build" / "smoke"
ARTIFACTS.mkdir(parents=True, exist_ok=True)
PREFIX = "cw-egress-" + uuid.uuid4().hex[:8]
NETWORK = PREFIX + "-net"
containers = []
network_created = False


def docker(*args, check=True):
    result = subprocess.run(["docker", *map(str, args)], capture_output=True, text=True, timeout=60)
    if check and result.returncode:
        raise RuntimeError(f"docker {args}: {result.stderr}\n{result.stdout}")
    return result.stdout.strip()


def start(suffix, *args):
    name = PREFIX + "-" + suffix
    docker("run", "-d", "--name", name, *args)
    containers.append(name)
    return name


def ip(name):
    return json.loads(docker("inspect", name))[0]["NetworkSettings"]["Networks"][NETWORK]["IPAddress"]


def fetch(client, url):
    return docker("exec", client, "python", "-c",
                  "import urllib.request;print(urllib.request.urlopen(" + repr(url) + ",timeout=2).read().decode())")


try:
    docker("network", "create", NETWORK)
    network_created = True
    server = start("server", "--network", NETWORK, "python:3.12-alpine", "python", "-m", "http.server", "18443")
    clients = [start("client" + str(i), "--network", NETWORK, "python:3.12-alpine", "python", "-c", "import time;time.sleep(300)") for i in range(2)]
    server_ip = ip(server)
    client_ips = [ip(c) for c in clients]
    gateway = json.loads(docker("network", "inspect", NETWORK))[0]["IPAM"]["Config"][0]["Gateway"]
    config = {
        "ports": [18443, 18444],
        "source_cidrs": [addr + "/32" for addr in client_ips + [gateway]],
        "exclude_cidrs": [gateway + "/32"],
        "object_path": "/src/build/egress.bpf.o",
        "host_netns_path": "/host/proc/1/ns/net",
        "listen_addr": ":9359", "log_attempts": True, "node": "smoke-node",
    }
    # JSON is a YAML subset and avoids another dependency in the smoke runner.
    (ARTIFACTS / "config.yaml").write_text(json.dumps(config))
    collector = start("collector", "--privileged", "--network", NETWORK,
                      "-v", str(ROOT) + ":/src:ro",
                      "-v", "/sys/kernel/btf:/sys/kernel/btf:ro",
                      "-v", "/proc:/host/proc:ro",
                      "conntrack-watch-egress-toolchain:dev", "sh", "-c",
                      "mount -t tracefs tracefs /sys/kernel/tracing && exec /src/build/egress-watch -config /src/build/smoke/config.yaml")
    collector_ip = ip(collector)
    for attempt in range(30):
        try:
            fetch(clients[0], f"http://{collector_ip}:9359/readyz")
            fetch(clients[0], f"http://{server_ip}:18443/")  # one warm-up flow, recorded below
            break
        except RuntimeError:
            if attempt == 29:
                raise RuntimeError(docker("logs", collector))
            time.sleep(.2)
    expected = []
    connect = """import socket,json,sys
s=socket.socket();s.settimeout(2)
try:
 s.connect((sys.argv[1],int(sys.argv[2])));error=0
except OSError as e: error=e.errno
print(json.dumps({'src_port':s.getsockname()[1],'error':error}))
s.close()
"""
    for client, addr in zip(clients, client_ips):
        for port in (18443, 18444):
            result = json.loads(docker("exec", client, "python", "-c", connect, server_ip, port))
            assert result["error"] == (0 if port == 18443 else 111), result
            expected.append({"src_ip": addr, "src_port": result["src_port"], "dst_port": port})
        # Unconfigured destination port and excluded destination must not appear.
        docker("exec", client, "python", "-c", connect, server_ip, 18445)
        docker("exec", client, "python", "-c", connect, gateway, 18444)
    # Host socket must be excluded even though its source IP is explicitly allowed.
    docker("run", "--rm", "--network", "host", "python:3.12-alpine", "python", "-c", connect, server_ip, 18443)
    for attempt in range(30):
        log = docker("logs", collector)
        records = [json.loads(line) for line in log.splitlines() if line.startswith("{")]
        events = [e for e in records if e.get("type") == "egress_connection"]
        outcomes = [e for e in events if e["event"] != "attempt"]
        if len(outcomes) >= 5:
            break
        time.sleep(.2)
    assert len(outcomes) == 5, outcomes  # warm-up plus 2 successful and 2 refused
    assert len(events) == 10, events
    assert {e["src_ip"] for e in events} == set(client_ips)
    assert all(e["dst_ip"] == server_ip and e["dst_port"] in (18443, 18444) for e in events)
    for wanted in expected:
        matches = [e for e in outcomes if all(e[k] == v for k, v in wanted.items())]
        assert len(matches) == 1, (wanted, outcomes)
        e = matches[0]
        assert e["event"] == ("established" if wanted["dst_port"] == 18443 else "closed_before_established")
        assert e["src_port_known"] and e["connect_duration_ms"] >= 0 and e["pid"] > 0 and e["netns"] > 0
    time.sleep(1.1)  # allow periodic kernel health counters to be sampled
    metrics = fetch(clients[0], f"http://{collector_ip}:9359/metrics")
    for line in [
        'egress_connections_total{event="attempt",port="18443"} 3',
        'egress_connections_total{event="established",port="18443"} 3',
        'egress_connections_total{event="attempt",port="18444"} 2',
        'egress_connections_total{event="closed_before_established",port="18444"} 2',
        'egress_connect_duration_seconds_count{port="18443"} 3',
        'egress_collector_ready 1',
        'egress_decode_errors_total 0',
        'egress_kernel_errors_total{reason="ringbuf_full"} 0',
        'egress_kernel_errors_total{reason="pending_map_full"} 0',
        'egress_kernel_errors_total{reason="namespace_read"} 0',
    ]:
        assert line in metrics, line
    docker("stop", "--time", "5", collector)
    assert json.loads(docker("inspect", collector))[0]["State"]["ExitCode"] == 0
    (ARTIFACTS / "connections.jsonl").write_text("\n".join(json.dumps(e) for e in events) + "\n")
    (ARTIFACTS / "metrics.txt").write_text(metrics + "\n")
    report = {"result": "passed", "kernel": docker("run", "--rm", "--network", "none", "python:3.12-alpine", "uname", "-r"),
              "client_ips": client_ips, "server_ip": server_ip, "events": len(events),
              "checks": ["real tracepoint/CO-RE load", "two source container IPs", "successful opens", "refused opens", "source port completion", "destination exclusion", "port exclusion", "host namespace exclusion", "JSON and Prometheus agreement", "zero collection errors", "SIGTERM detach"],
              "boundary": "Docker Linux VM, IPv4; not target Kubernetes/CNI, IPv6 traffic, or load testing"}
    (ARTIFACTS / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))
finally:
    for name in reversed(containers):
        docker("rm", "-f", name, check=False)
    if network_created:
        docker("network", "rm", NETWORK, check=False)
