"""Exercise the desktop Agent v2 protocol against a real backend and local scripted model."""
import argparse
from contextlib import closing, contextmanager
import hashlib
import json
import os
import socket
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


ORIGINAL = "user's existing draft\nfirst idea\nlast idea\n"
REVISED = "user's existing draft\nfirst revision\nlast revision\n"
REPORT = "Both edits were verified against the staged file.\n"


def sha(text):
    return hashlib.sha256(text.encode()).hexdigest()


class ScriptedModel(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        try:
            body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
            native = self.server.native
            index = self.server.calls
            self.server.calls += 1
            definitions = {t['function']['name'] for t in body['tools']}
            assert {'read_file', 'edit_file', 'write_file', 'present'} <= definitions, definitions
            if index:
                m = body['messages'][-1]
                assert m['role'] == 'tool' and m['tool_call_id'] == f'fixture-{index - 1}', m
                observation = json.loads(m['content'])
                if index == 2:
                    assert observation['text'] == ORIGINAL and observation['sha256'] == sha(ORIGINAL), observation
                elif index == 3:
                    assert observation['ok'] is False and observation['error'], observation
                elif native and index in (8, 10):
                    assert (observation['exit_code'] == 0) == (index == 10), observation
                    assert len(observation['log_id']) == 64, observation
                    if index == 10: self.server.log_id = observation['log_id']
                elif native and index == 12:
                    assert observation['sha256'] == hashlib.sha256(b'x' * 140000).hexdigest(), observation
                elif index == 5:
                    assert observation['text'] == REVISED and observation['sha256'] == sha(REVISED), observation
                else:
                    assert observation.get('ok', True) and not observation.get('error'), observation
            edits = [{'old': 'first idea', 'new': 'first revision'}, {'old': 'last idea', 'new': 'last revision'}]
            steps = [
                ('list_files', {}),
                ('read_file', {'path': 'notes.md'}),
                ('edit_file', {'path': 'notes.md', 'expected_hash': '0' * 64, 'edits': edits}),
                ('edit_file', {'path': 'notes.md', 'expected_hash': sha(ORIGINAL), 'edits': edits}),
                ('read_file', {'path': 'notes.md'}),
                ('write_file', {'path': 'report.txt', 'content': REPORT}),
                ('present', {'path': 'report.txt'}),
            ]
            if native:
                validate = "from pathlib import Path; assert Path('ready.txt').is_file(); Path('result.bin').write_bytes(b'x'*140000); print('verified delivery')"
                command = {'program': 'python', 'args': ['-I', '-c', validate], 'cwd': '.', 'verify': True}
                steps.extend([
                    ('run_command', command), ('write_file', {'path': 'ready.txt', 'content': 'ready'}),
                    ('run_command', command), ('read_command_log', {'log_id': getattr(self.server,'log_id',''), 'offset': 0,'limit': 200}),
                    ('present', {'path': 'result.bin'}),
                    ('read_file', {'path': 'ready.txt'}),
                ])
            if self.server.withdraw:
                steps.append(('write_file', {'path': 'z/guard.txt', 'expected_hash': sha('original guard'), 'content': 'revised guard'}))
            if index < len(steps):
                name, args = steps[index]
                msg = {'tool_calls': [{'id': f'fixture-{index}', 'type': 'function',
                                      'function': {'name': name, 'arguments': json.dumps(args)}}]}
            else:
                assert index == len(steps), 'unexpected model replay'
                msg = {'content': 'Reviewed the revised notes and prepared the report.'}
            data = json.dumps({'choices': [{'message': msg, 'finish_reason': 'tool_calls' if 'tool_calls' in msg else 'stop'}]}).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        except Exception as e:
            self.server.errors.append(repr(e))
            self.send_error(500)


@contextmanager
def block_replace(root):
    # Keep the target readable for preflight, but deny the actual rename.
    if os.name == 'nt':
        import ctypes
        from ctypes import wintypes
        kernel = ctypes.WinDLL('kernel32', use_last_error=True)
        create = kernel.CreateFileW
        create.argtypes = [wintypes.LPCWSTR, wintypes.DWORD, wintypes.DWORD, wintypes.LPVOID,
                           wintypes.DWORD, wintypes.DWORD, wintypes.HANDLE]
        create.restype = wintypes.HANDLE
        handle = create(str(root / 'z/guard.txt'), 0x80000000, 3, None, 3, 0x80, None)
        if handle == wintypes.HANDLE(-1).value:
            raise ctypes.WinError(ctypes.get_last_error())
        try:
            yield
        finally:
            kernel.CloseHandle.argtypes = [wintypes.HANDLE]
            kernel.CloseHandle(handle)
    else:
        folder = root / 'z'
        mode = folder.stat().st_mode & 0o777
        folder.chmod(0o555)
        try:
            yield
        finally:
            folder.chmod(mode)


def exercise(exe, base, mock, withdraw=False, native=False):
    root = base / 'project'
    root.mkdir()
    if withdraw:
        (root / 'z').mkdir()
        (root / 'z/guard.txt').write_bytes(b'original guard')
    mock.withdraw, mock.native, mock.calls, mock.errors = withdraw, native, 0, []
    (root / 'notes.md').write_bytes(ORIGINAL.encode())
    (root / 'untracked.txt').write_bytes(b'leave this alone')
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    models = base / 'models.yaml'
    models.write_text(json.dumps({'default_model': 'fixture', 'providers': [{
        'id': 'fixture', 'name': 'fixture', 'model': 'fixture', 'type': 'deepseek',
        'enabled': True, 'base_url': f'http://127.0.0.1:{mock.server_port}'}]}), encoding='utf-8')
    config = base / 'config.yaml'
    config.write_text(json.dumps({
        'app': {'data_dir': str(base / 'data')}, 'server': {'host': '127.0.0.1', 'port': port},
        'modules': {name: {'enabled': False} for name in ('nebula', 'devops', 'codelens')},
        'gateway': {'config_path': str(models)}, 'logging': {'dir': str(base / 'logs')},
    }), encoding='utf-8')
    token = 'local-only-delivery-fixture'
    env = dict(os.environ, NOOSPHERE_BACKEND_TOKEN=token)
    proc = None

    def request(path, body=None, binary=False):
        raw = None if body is None else json.dumps(body).encode()
        headers = {'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token}
        with urlopen(Request(f'http://127.0.0.1:{port}' + path, data=raw, headers=headers), timeout=10) as response:
            return response.read() if binary else json.load(response)

    def stop():
        if proc and proc.poll() is None:
            proc.kill()
            proc.wait(timeout=10)

    def start(log):
        nonlocal proc
        proc = subprocess.Popen([str(exe), '-config', str(config)], cwd=base, stdout=log, stderr=log, env=env,
                                creationflags=getattr(subprocess, 'CREATE_NO_WINDOW', 0))
        deadline = time.monotonic() + 25
        while time.monotonic() < deadline:
            assert proc.poll() is None, 'backend exited'
            try:
                if request('/health')['status'] == 'ok':
                    return
            except (URLError, OSError):
                time.sleep(.1)
        raise AssertionError('backend startup timeout')

    def rejected(fn):
        try:
            fn()
        except HTTPError as e:
            assert 400 <= e.code < 500, (e.code, e.read())
        else:
            raise AssertionError('invalid or stale authorization accepted')

    def wait(run_id, wanted):
        deadline = time.monotonic() + (240 if native else 40)
        while time.monotonic() < deadline:
            assert not mock.errors, mock.errors
            run = request('/api/runs/' + run_id)
            if run['status'] == wanted:
                return run
            assert run['status'] in ('running', 'queued'), run
            time.sleep(.075)
        raise AssertionError('timed out waiting for ' + wanted)

    with (base / 'backend.log').open('ab') as log:
        try:
            start(log)
            ws = request('/api/workspaces', {'name': 'Delivery fixture', 'project_path': str(root)})
            group = '/api/workspaces/' + ws['id']
            task = {'kind': 'agent', 'title': 'Deliver notes', 'input': {
                'engine_version': 2, 'request_id': 'fixture-submission', 'mode': 'review', 'goal': 'Update the two ideas, verify them and deliver a report.',
                'authorized_binding_id': ws['project_binding_id'], 'model_id': 'fixture', 'session_id': 'delivery-fixture'}}
            rejected(lambda: request(group + '/runs', task))
            request(group + '/agent-grant', {'binding_id': ws['project_binding_id']})
            run_id = request(group + '/runs', task)['id']
            assert request(group + '/runs', task)['id'] == run_id
            conflicting = dict(task, input=dict(task['input'], goal='A different request'))
            try:
                request(group + '/runs', conflicting)
            except HTTPError as err:
                assert err.code == 409, err.code
            else:
                raise AssertionError('reused submission ID changed the task')

            detail = group + '/runs/' + run_id
            wait(run_id, 'waiting_approval')
            result = request(detail + '/delivery')
            digest = result['digest']
            assert result['approval'] == digest and len(digest) == 64, result
            paths = {'notes.md', 'report.txt'} | ({'ready.txt','result.bin'} if native else set()) | ({'z/guard.txt'} if withdraw else set())
            assert {c['path'] for c in result['changes']} == paths, result
            assert result['change_total'] == len(paths) and result['change_offset'] == 0
            rest = request(detail + '/delivery?change_offset=1')
            assert rest['digest'] == digest and rest['changes'] == result['changes'][1:]
            rejected(lambda: request(detail + '/delivery?change_offset=-1'))
            assert (root / 'notes.md').read_bytes() == ORIGINAL.encode() and not (root / 'report.txt').exists()
            assert mock.calls == (9 if withdraw else 14 if native else 8), mock.calls
            stop()
            start(log)
            assert request('/api/runs/' + run_id)['status'] == 'waiting_approval'
            assert request(detail + '/delivery')['approval'] == digest

            def control(action, request_id, expected=digest):
                return request(detail + '/control', {'action': action, 'request_id': request_id, 'expected': expected})
            rejected(lambda: control('approve', 'bad-approval', '0' * 64))
            if withdraw:
                with block_replace(root):
                    control('approve', 'good-approval')
                    wait(run_id, 'paused')
                    partial = request(detail + '/delivery')
                    assert partial['publication_state'] == 'publishing' and partial['can_undo'], partial
                    assert (root / 'notes.md').read_bytes() == REVISED.encode() and (root / 'report.txt').exists()
                    assert (root / 'z/guard.txt').read_bytes() == b'original guard'
                    stop()
                    start(log)
                    rejected(lambda: request(detail + '/control', {'action': 'steer', 'value': 'new goal', 'request_id': 'late-steer'}))
                    paused = request('/api/runs/' + run_id)
                    control('undo', 'withdraw-partial')
                    assert request(detail + '/delivery')['undone']
                    stop()
                    # Reproduce a crash after the file journal reached "undone" but
                    # before the SQLite receipt and run-state transaction committed.
                    with closing(sqlite3.connect(base / 'data' / 'workspace.sqlite')) as db, db:
                        db.execute("UPDATE runs SET status='paused',body=? WHERE id=?", (json.dumps(paused).encode(), run_id))
                        db.execute("DELETE FROM agent_controls WHERE run_id=? AND request='withdraw-partial'", (run_id,))
                        db.execute("DELETE FROM events WHERE run_id=? AND phase='undone'", (run_id,))
                    start(log)
                    pending = request(detail + '/delivery')
                    assert pending['undone'] and pending['can_undo'] and pending['undo_receipt_pending'], pending
                    control('undo', 'reconcile-withdrawal')
                    assert not request(detail + '/delivery')['undo_receipt_pending']
                    assert request('/api/runs/' + run_id)['status'] == 'cancelled'
                    assert (root / 'notes.md').read_bytes() == ORIGINAL.encode() and not (root / 'report.txt').exists()
                    assert (root / 'z/guard.txt').read_bytes() == b'original guard'
                    assert mock.calls == 9
                print('PASS partial publication: locked target, restart, conflicting steering denied, withdrawal of only committed files')
                return
            control('approve', 'good-approval')
            completed = wait(run_id, 'completed')
            assert completed['result']['published'] and mock.calls == (14 if native else 8), completed
            assert (root / 'notes.md').read_bytes() == REVISED.encode()
            assert (root / 'report.txt').read_bytes() == REPORT.encode()
            delivered = request(detail + '/delivery')
            cards = delivered['artifacts']
            assert len(cards) == (2 if native else 1) and delivered['published'], delivered
            originals = {item['name']:request(detail+'/artifacts/'+item['id'],binary=True) for item in cards}
            assert originals['report.txt'] == REPORT.encode()
            if native: assert originals['result.bin'] == b'x'*140000 and completed['result']['tool_calls'] > 12
            (root / 'notes.md').write_bytes(b'subsequent editor change')
            rejected(lambda: control('undo', 'conflicted-undo'))
            assert (root / 'report.txt').exists() and (root / 'notes.md').read_bytes() == b'subsequent editor change'
            (root / 'notes.md').write_bytes(REVISED.encode())
            control('undo', 'undo-round')
            assert request(detail + '/delivery')['undone']
            assert (root / 'notes.md').read_bytes() == ORIGINAL.encode() and not (root / 'report.txt').exists()
            assert (root / 'untracked.txt').read_bytes() == b'leave this alone'
            if native:
                assert not (root/'ready.txt').exists() and not (root/'result.bin').exists()
                print('PASS native 14 model turns, failing Python verification, repair, same-command recheck, paged command log, 140 KB binary and exact undo')
                return
            print('PASS v2 grant, 8 model turns, observed repair, immutable artifact, restart, reviewed publication, conflict and round undo')
        except Exception:
            log.flush()
            print((base / 'backend.log').read_text(encoding='utf-8', errors='replace')[-6000:], file=sys.stderr)
            raise
        finally:
            stop()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--backend', required=True, type=Path)
    exe = parser.parse_args().backend.resolve(strict=True)
    with ThreadingHTTPServer(('127.0.0.1', 0), ScriptedModel) as model, tempfile.TemporaryDirectory(prefix='noosphere-delivery-') as raw:
        model.calls, model.errors = 0, []
        threading.Thread(target=model.serve_forever, daemon=True).start()
        try:
            base = Path(raw).resolve()
            normal = base / 'successful'; normal.mkdir()
            exercise(exe, normal, model)
            if sys.platform.startswith(('win','linux')):
                native = base / 'native'; native.mkdir()
                exercise(exe,native,model,native=True)
            # A privileged Unix account bypasses directory write permissions.
            if os.name == 'nt' or os.geteuid() != 0:
                partial = base / 'interrupted'; partial.mkdir()
                exercise(exe, partial, model, withdraw=True)
        finally:
            model.shutdown()


if __name__ == '__main__':
    main()
