from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import hashlib
import json
import os
from pathlib import Path
import queue
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time
from urllib.error import HTTPError
from urllib.parse import quote
from urllib.request import Request, urlopen
import tarfile
import shutil

previous_artifacts, candidate_artifacts, evidence, target, candidate_sha256 = sys.argv[1:]
assert target in {"macos-arm64", "macos-x64"}
root = Path(tempfile.mkdtemp(prefix="v090-upgrade-", dir=Path(evidence).resolve()))


def extract_backend(artifacts, version, revision, destination, pinned_hash=None):
    manifests = list(Path(artifacts).rglob("build-info.json"))
    assert len(manifests) == 1
    info = json.loads(manifests[0].read_text(encoding="utf-8"))
    assert (info["version"], info["source_revision"], info["source_dirty"], info["target"]) == (
        version, revision, False, target)
    name = f"Noosphere-{version}-{target}.app.tar.gz"
    checksum_lines = (manifests[0].parent / "SHA256SUMS.txt").read_text(encoding="utf-8").splitlines()
    checksums = dict((line.split()[1].lstrip("*"), line.split()[0]) for line in checksum_lines)
    snapshot = manifests[0].parent / name
    with snapshot.open("rb") as stream:
        digest = hashlib.file_digest(stream, "sha256").hexdigest()
    assert digest == checksums[name]
    if pinned_hash:
        assert digest == pinned_hash
    with tarfile.open(snapshot, "r:gz") as archive:
        members = [item for item in archive if item.isfile()
                   and item.name.endswith(".app/Contents/Resources/bin/noosphere-backend")]
        assert len(members) == 1
        with archive.extractfile(members[0]) as source, destination.open("xb") as output:
            shutil.copyfileobj(source, output)
    destination.chmod(0o700)
    with destination.open("rb") as stream:
        backend_digest = hashlib.file_digest(stream, "sha256").hexdigest()
    assert backend_digest.lower() == info["backend_sha256"].lower()
    print(f"{target} version={version} source={revision} archive_sha256={digest} backend_sha256={backend_digest}", flush=True)
    return destination


old = extract_backend(previous_artifacts, "0.9.0", "0f6cdef68f6296aeebf1eee6e120db07dfacef57", root / "v090-backend")
candidate = extract_backend(candidate_artifacts, "1.0.0", "564ad1f93549b0a05d4cdfa6f19e5bbddfd6d5a0",
                            root / "preview-backend", candidate_sha256)
data_root = root / "Original 工作台"
data_root.mkdir()
project = root / "Project 资料"
project.mkdir()
(project / "readme.txt").write_text("fixture guidance", encoding="utf-8")
(data_root / "config.yaml").write_text("""config_version: 1
modules:
  codelens: {enabled: false}
  devops: {enabled: false}
  nebula:
    embedding: {provider: mock}
""", encoding="utf-8")
class Provider(BaseHTTPRequestHandler):
    def reply(self, payload):
        body = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def do_GET(self):
        self.reply({"data": [{"id": "fixture"}]})
    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length", 0)))
        self.reply({"choices": [{"message": {"role": "assistant", "content": "OK"}, "finish_reason": "stop"}], "usage": {"prompt_tokens": 1, "completion_tokens": 1}})
    def log_message(self, *_):
        pass

provider = ThreadingHTTPServer(("127.0.0.1", 0), Provider)
threading.Thread(target=provider.serve_forever, daemon=True).start()
(data_root / "models.yaml").write_text(f"""default_model: fixture
providers:
  - {{id: fixture, name: Validation fixture, type: openai, base_url: 'http://127.0.0.1:{provider.server_port}/v1', model: fixture, enabled: true}}
""", encoding="utf-8")

class Backend:
    def __init__(self, binary, data_dir, name):
        self.token = os.urandom(24).hex()
        self.config = data_dir / "config.yaml"
        self.lines = queue.Queue()
        env = dict(os.environ, NOOSPHERE_DESKTOP_MANAGED="1", NOOSPHERE_BACKEND_TOKEN=self.token,
                   NOOSPHERE_BACKEND_INSTANCE=name)
        self.log = (root / (name + ".log")).open("wb")
        self.process = subprocess.Popen([binary, "-config", self.config], cwd=data_dir,
                                        env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=self.log)
        def reader():
            for row in self.process.stdout:
                self.lines.put(row.decode("utf-8").strip())
            self.lines.put(None)
        threading.Thread(target=reader, daemon=True).start()
        self.process.stdin.write(b"start\n")
        self.process.stdin.flush()
        while True:
            line = self.lines.get(timeout=60)
            if line is None:
                raise RuntimeError("backend failed: " + name)
            if line.startswith("NOOSPHERE_READY "):
                self.base = "http://127.0.0.1:" + line.split()[-1]
                break
    def request(self, path, data=None, headers=None):
        headers = dict(headers or {}, Authorization="Bearer " + self.token)
        if isinstance(data, dict):
            data = json.dumps(data).encode()
            headers["Content-Type"] = "application/json"
        try:
            with urlopen(Request(self.base + path, data=data, headers=headers), timeout=15) as response:
                return json.load(response)
        except HTTPError as error:
            raise RuntimeError(f"{path}: {error.read().decode()}") from error
    def stop(self, hard=False):
        if hard:
            self.process.kill()
        self.process.stdin.close()
        self.process.wait(timeout=20)
        self.log.close()

def record(message):
    print(message, flush=True)

previous = Backend(old, data_root, "released-0.9.0")
try:
    assert previous.request("/health")["version"] == "0.9.0"
    workspace = previous.request("/api/workspaces/open", {"path": str(project)})["id"]
    ws = "/api/workspaces/" + workspace
    previous.request("/api/gateway/selection", dict(scope="session", workspace_id=workspace, session_id="entry-v09", model_id="fixture"))
    selection = "/api/gateway/selection?session_id=entry-v09&workspace_id=" + workspace
    memory = previous.request(ws + "/knowledge", dict(content="Retain fixture project convention", category="decision", source={"kind": "note"}))
    memory["status"] = "confirmed"
    memory = previous.request(ws + "/knowledge", memory)
    run = previous.request(ws + "/runs", dict(kind="index", title="Record existing failed index task", input={}))
    for _ in range(30):
        run = previous.request("/api/runs/" + run["id"])
        if run["status"] not in ("queued", "running"):
            break
        time.sleep(0.1)
    assert run["status"] == "failed"
    contents = "Original attachment evidence\n模型路径与文件都含 Unicode\n".encode()
    scope = dict(workspace_id=workspace, session_id="entry-v09")
    metadata = dict(**scope, id="original-v09", name="来源 文档.txt", project_path=str(project))
    attached = previous.request("/api/chat-attachments/stage", contents, {"X-Noosphere-Attachment": quote(json.dumps(metadata, ensure_ascii=False))})
    turn = dict(**scope, message_id="message-v09", ids=[attached["id"]], question="Original attachment evidence")
    previous.request("/api/chat-attachments/prepare", turn)
    assert (data_root / "data/workspace.sqlite-wal").is_file()
finally:
    previous.stop(hard=True)
record("PASS v0.9.0 tag-build backend wrote workspace, session-model binding, model config, confirmed memory, failed task, and committed Unicode attachment; abruptly stopped to retain WAL")

if os.environ.get("NOOSPHERE_UPGRADE_SIMULATE_FAILURE") == "1":
    # Hold a SQLite write lock without changing a row. Copying the closed v0.9
    # bytes is possible, but the candidate cannot complete startup recovery.
    database = sqlite3.connect(data_root / "data/workspace.sqlite", isolation_level=None)
    try:
        database.execute("BEGIN IMMEDIATE")
        try:
            Backend(candidate, data_root, "candidate-blocked-store")
        except RuntimeError as problem:
            assert "backend failed" in str(problem)
        else:
            raise AssertionError("candidate incorrectly started with blocked migration")
        state = json.loads((data_root / "data-version.json").read_text(encoding="utf-8"))
        pending = json.loads((data_root / "data-upgrade-pending.json").read_text(encoding="utf-8"))
        assert state["app_version"] == "0.9.0"
        assert pending["from_version"] == "0.9.0" and pending["to_version"] == "1.0.0"
        backup = data_root / "upgrade-backups" / pending["snapshot"]
        assert (backup / "manifest.json").is_file()
        record("PASS candidate refused readiness when a SQLite write lock prevented recovery, retained the original pre-migration snapshot and kept version 0.9.0")
    finally:
        database.close()
current = Backend(candidate, data_root, "candidate")
def verify(current):
    stored_path = next(w for w in current.request("/api/workspaces")["workspaces"] if w["id"] == workspace)["project_path"]
    assert Path(stored_path).samefile(project)
    assert current.request(selection)["model_id"] == "fixture"
    assert any(m["id"] == memory["id"] and m["status"] == "confirmed" for m in current.request(ws + "/knowledge")["memories"])
    assert current.request("/api/runs/" + run["id"])["status"] == "failed"
    assert any(c["source_hash"] == attached["source_hash"] and c["title"] == attached["name"] and "Original attachment" in c["excerpt"]
               for c in current.request("/api/chat-attachments/prepare", turn)["citations"])
try:
    assert current.request("/health")["version"] == "1.0.0"
    verify(current)
    backups = [entry for entry in (data_root / "upgrade-backups").iterdir() if json.loads((entry / "manifest.json").read_text(encoding="utf-8"))["from_version"] == "0.9.0"]
    assert len(backups) == 1
    backup = backups[0]
    manifest = json.loads((backup / "manifest.json").read_text(encoding="utf-8"))
    assert (manifest["from_version"], manifest["to_version"]) == ("0.9.0", "1.0.0")
    assert "data/workspace.sqlite-wal" in manifest["sha256"]
    for name, digest in manifest["sha256"].items():
        with (backup / name).open("rb") as f:
            assert hashlib.file_digest(f, "sha256").hexdigest() == digest, name
    locked = subprocess.run([candidate, "-config", current.config, "-backup-data"], capture_output=True, timeout=20)
    assert locked.returncode != 0 and b"cannot lock project data" in locked.stderr
finally:
    current.stop()
record("PASS candidate upgraded actual 0.9.0 backend stores, verified data and committed attachment citations, hashed all pre-upgrade copies and rejected live backup")

manual = subprocess.run([candidate, "-config", data_root / "config.yaml", "-backup-data"], capture_output=True, timeout=30)
assert manual.returncode == 0, manual.stderr.decode("utf-8", "replace")
manual_path = Path(manual.stdout.decode("utf-8").strip())
manual_manifest = json.loads((manual_path / "manifest.json").read_text(encoding="utf-8"))
assert manual_path.parent == (data_root / "upgrade-backups")
assert (manual_manifest["from_version"], manual_manifest["to_version"]) == ("1.0.0", "1.0.0")
assert "data/workspace.sqlite" in manual_manifest["sha256"] and "models.yaml" in manual_manifest["sha256"]
record("PASS explicit offline backup from another working directory selected the configured store and emitted a checksummed snapshot")
recovery = root / "Recovery 恢复"
subprocess.run([candidate, "-restore-data-from", backup, "-restore-data-to", recovery], check=True, capture_output=True, timeout=30)
restored = Backend(old, recovery, "recovered-v0.9.0")
try:
    assert restored.request("/health")["version"] == "0.9.0"
    verify(restored)
finally:
    restored.stop()
record("PASS v0.9.0 tag-build backend reopened isolated recovered pre-upgrade snapshot, including the session binding, memory, task and attachment citation")
record("Evidence directory " + str(root))
