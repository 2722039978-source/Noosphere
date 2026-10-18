"""Isolated packaged-backend Agent test. Local mock model only, no live credentials."""
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.request import Request, urlopen
from urllib.error import HTTPError

ROOT = Path(__file__).resolve().parents[1]
EXE = ROOT / 'release/bin/noosphere-backend.exe'
fixture = Path(tempfile.mkdtemp(prefix='noosphere-agent-release-')).resolve()
source = fixture / 'project'
source.mkdir()
raw = b'package main\nfunc start() {}\n'
(source / 'main.go').write_bytes(raw)
calls = []
received = threading.Event()
release = threading.Event()
upstream_errors = []

class Model(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        try:
            body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
            calls.append(body)
            assert body['thinking']['type'] == 'disabled'
            assert body['tools'] and body['max_tokens'] == 2048
            goal = next(m['content'] for m in body['messages'] if m['role'] == 'user')
            if goal.startswith('hold'):
                received.set()
                release.wait(20)
            if goal.startswith('propose'):
                names = {t['function']['name'] for t in body['tools']}
                assert 'propose_file_change' in names and 'approve' not in names
                if body['messages'][-1]['role'] == 'tool':
                    assert json.loads(body['messages'][-1]['content'])['success']
                    message = {'content': 'Proposal saved; awaiting human approval.'}
                else:
                    name = 'propose_test' if goal == 'propose test' else 'propose_file_change'
                    args = {'runner': 'python', 'reason': 'run fixture tests'} if goal == 'propose test' else {'path': 'main.go', 'old_text': 'func start() {}', 'new_text': 'func boot() {}', 'reason': 'rename fixture function'}
                    message = {'tool_calls': [{'id': 'proposal-call', 'type': 'function', 'function': {'name': name, 'arguments': json.dumps(args)}}]}
            elif body['messages'][-1]['role'] == 'tool':
                result = json.loads(body['messages'][-1]['content'])
                assert body['messages'][-1]['tool_call_id'] == 'file-call'
                assert result['success'] and result['evidence'][0]['source_hash'] == hashlib.sha256(raw).hexdigest()
                message = {'content': 'Verified startup [E1]'}
            else:
                message = {'tool_calls': [{'id': 'file-call', 'type': 'function', 'function': {'name': 'project_read_file', 'arguments': '{"path":"main.go"}'}}]}
            data = json.dumps({'choices': [{'finish_reason': 'tool_calls' if 'tool_calls' in message else 'stop', 'message': message}]}).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError):
            pass
        except Exception as e:
            upstream_errors.append(str(e))
            self.send_error(500)

mock = ThreadingHTTPServer(('127.0.0.1', 0), Model)
threading.Thread(target=mock.serve_forever, daemon=True).start()
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    port = sock.getsockname()[1]
models = {'default_model': 'fixture', 'providers': [{'id': 'fixture', 'name': 'fixture', 'type': 'deepseek', 'model': 'fixture', 'base_url': f'http://127.0.0.1:{mock.server_port}', 'enabled': True}]}
(fixture / 'models.yaml').write_text(json.dumps(models), encoding='utf-8')
config = {'app': {'data_dir': str(fixture / 'data')}, 'server': {'host': '127.0.0.1', 'port': port}, 'modules': {'nebula': {'enabled': False}, 'codelens': {'enabled': False}, 'devops': {'enabled': False}}, 'gateway': {'config_path': str(fixture / 'models.yaml')}, 'logging': {'dir': str(fixture / 'logs')}}
config_path = fixture / 'config.yaml'
config_path.write_text(json.dumps(config), encoding='utf-8')
env = dict(os.environ, NOOSPHERE_BACKEND_TOKEN='isolated-agent-release-fixture')
process = None
log = (fixture / 'backend.log').open('ab')

def api(path, body=None):
    data = None if body is None else json.dumps(body).encode()
    request = Request(f'http://127.0.0.1:{port}{path}', data=data, headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + env['NOOSPHERE_BACKEND_TOKEN']})
    with urlopen(request, timeout=5) as response:
        return json.load(response)

def start():
    global process
    process = subprocess.Popen([str(EXE), '-config', str(config_path)], cwd=fixture, env=env, stdout=log, stderr=log, creationflags=getattr(subprocess, 'CREATE_NO_WINDOW', 0))
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        assert process.poll() is None, 'backend exited'
        try:
            if api('/health')['status'] == 'ok':
                return
        except Exception:
            time.sleep(.1)
    raise AssertionError('startup timeout')

def stop():
    if process and process.poll() is None:
        process.kill()
        process.wait(timeout=5)

def wait(run_id):
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        run = api('/api/runs/' + run_id)
        if run['status'] not in ('queued', 'running'):
            return run
        time.sleep(.05)
    raise AssertionError('task timeout')

passed = []
def check(name):
    passed.append(name)
    print('PASS ' + name, flush=True)

try:
    start()
    ws = api('/api/workspaces', {'name': 'Agent fixture', 'project_path': str(source)})
    def submit(goal, reviewed=False):
        return api(f"/api/workspaces/{ws['id']}/runs", {'kind': 'agent', 'title': goal, 'input': {'goal': goal, 'session_id': 'fixture-chat', 'reviewed_actions': reviewed}})
    run = submit('inspect startup')
    done = wait(run['id'])
    assert done['status'] == 'completed', done
    assert done['result']['answer'] == 'Verified startup [E1]'
    assert done['result']['model_calls'] == 2 and done['result']['tool_calls'] == 1
    assert done['result']['model_id'] == 'fixture' and done['session_id'] == 'fixture-chat'
    assert len(calls) == 2 and not upstream_errors
    check('packaged structured model/tool/result roundtrip with evidence hash')
    events = api(f"/api/runs/{run['id']}/events")['events']
    assert {'agent_started', 'tool_started', 'tool_completed'}.issubset({e['phase'] for e in events})
    check('durable per-step progress and original chat identity')
    held = submit('hold cancellation')
    assert received.wait(5)
    cancelled = api(f"/api/runs/{held['id']}/cancel", {})
    assert cancelled['status'] == 'cancelled'
    release.set()
    time.sleep(.2)
    assert not any(e['phase'] == 'tool_started' for e in api(f"/api/runs/{held['id']}/events")['events'])
    check('cancel during model response executes no later tool')
    received.clear(); release.clear()
    rebound = submit('hold rebind')
    assert received.wait(5)
    replacement = fixture / 'replacement'; replacement.mkdir()
    ws['project_path'] = str(replacement)
    api('/api/workspaces', ws)
    release.set()
    failed = wait(rebound['id'])
    assert failed['status'] == 'failed' and failed['result']['tool_calls'] == 0, failed
    check('workspace rebind stops before executing returned tool')
    received.clear(); release.clear()
    interrupted = submit('hold restart')
    assert received.wait(5)
    before = len(calls)
    stop(); release.set(); start()
    assert api('/api/runs/' + interrupted['id'])['status'] == 'interrupted'
    recovered = api('/api/runs/' + run['id'])
    assert recovered['result'] == done['result']
    time.sleep(.2)
    assert len(calls) == before
    check('restart retains completed evidence and interrupts pending work without replay')
    ws['project_path'] = str(source)
    ws = api('/api/workspaces', ws)
    def actions(task):
        return api(f"/api/workspaces/{ws['id']}/runs/{task['id']}/actions")['actions']
    def decide(action, decision):
        return api(f"/api/workspaces/{ws['id']}/actions/{action['id']}", {'decision': decision})
    proposed = submit('propose file', True)
    assert wait(proposed['id'])['status'] == 'completed'
    action = actions(proposed)[0]
    assert action['status'] == 'pending' and (source / 'main.go').read_bytes() == raw
    check('structured proposal persists without mutating a project file')
    stop(); start()
    assert actions(proposed)[0]['status'] == 'pending'
    decide(action, 'approve')
    assert b'func boot()' in (source / 'main.go').read_bytes()
    try:
        decide(action, 'approve')
        raise AssertionError('duplicate approval succeeded')
    except HTTPError as error:
        assert error.code == 409
    check('pending proposal survives restart and approval applies exactly once')
    (source / 'main.go').write_text('external change', encoding='utf-8')
    try:
        decide(action, 'rollback')
        raise AssertionError('rollback overwrote external changes')
    except HTTPError as error:
        assert error.code == 409
    (source / 'main.go').write_bytes(action['after'].encode('utf-8'))
    decide(action, 'rollback')
    assert (source / 'main.go').read_bytes() == raw
    check('rollback rejects external changes and restores the exact original')
    (source / 'test_smoke.py').write_text('import unittest\nclass Smoke(unittest.TestCase):\n    def test_pass(self): self.assertEqual(2 + 2, 4)\n', encoding='utf-8')
    test_task = submit('propose test', True)
    assert wait(test_task['id'])['status'] == 'completed'
    test_action = actions(test_task)[0]
    decide(test_action, 'approve')
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        test_action = actions(test_task)[0]
        if test_action['status'] != 'running': break
        time.sleep(.1)
    assert test_action['status'] == 'completed' and 'OK' in test_action['output'], test_action
    check('approved fixed test runner executes real isolated Python tests')
    assert not upstream_errors, upstream_errors
    report = {'passed': len(passed), 'checks': passed, 'artifact': str(EXE), 'fixture': str(fixture)}
    (fixture / 'result.json').write_text(json.dumps(report, indent=2), encoding='utf-8')
    print(json.dumps(report, indent=2))
finally:
    release.set(); stop(); mock.shutdown(); log.close()
