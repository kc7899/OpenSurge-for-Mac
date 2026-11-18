"""Disposable Ubuntu package test; never run on a user's gateway."""
import os
from pathlib import Path
import subprocess
import time

assert os.environ.get("GITHUB_ACTIONS") == "true" and os.geteuid() == 0
root = Path("/var/lib/opensurge/runtime")
process = subprocess.Popen([
    "unshare", "--net", "sh", "-ec",
    "ip link set lo up; exec /usr/sbin/dnsmasq --keep-in-foreground "
    "--conf-file=/var/lib/opensurge/runtime/dnsmasq.conf "
    "--log-facility=/var/lib/opensurge/runtime/logs/dnsmasq.log",
], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
try:
    for _ in range(100):
        if process.poll() is not None:
            raise AssertionError(process.communicate())
        if (root / "dnsmasq.pid").exists() and (root / "logs/dnsmasq.log").exists():
            break
        time.sleep(0.05)
    assert int((root / "dnsmasq.pid").read_text().strip()) == process.pid
    label = Path(f"/proc/{process.pid}/attr/current").read_text()
    assert "dnsmasq" in label and "(enforce)" in label, label
    result = subprocess.check_output([
        "nsenter", "--target", str(process.pid), "--net", "dig", "+short",
        "+time=1", "+tries=1", "@127.0.0.1", "-p", "15353", "opensurge-fixture.test",
    ], text=True)
    assert result.strip() == "192.0.2.15", result
    assert (root / "logs/dnsmasq.log").stat().st_size > 0
    # No access to the rest of the private configuration was granted.
    denied = subprocess.run([
        "/usr/sbin/dnsmasq", "--test", "--conf-file=/var/lib/opensurge/config.yaml",
    ], capture_output=True, text=True)
    assert denied.returncode != 0 and "Permission denied" in denied.stderr
    print("Enforced stock AppArmor: exact config/PID/log paths work, DNS answers, private config remains denied.")
finally:
    process.terminate()
    try:
        process.communicate(timeout=5)
    except subprocess.TimeoutExpired:
        process.kill()
        process.communicate()
