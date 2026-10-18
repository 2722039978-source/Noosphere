"""Bounded incremental source indexing; graph edges are conservative static facts."""
import hashlib
import os
import threading
from contextlib import contextmanager
from pathlib import Path
from ast_parser import LANGUAGES, get_analyzer
from symbol_chunks import symbol_chunks

GRAPH_CACHE = {}
PARSE_CACHE = {}
INDEX_LOCK = threading.RLock()
EXCLUDES = {'.git', 'node_modules', '__pycache__', 'target', 'dist', '.next', 'vendor', '.local', '.venv', 'venv', '.tmp-edge-profile'}
MAX_FILE_BYTES = 1_000_000
MAX_PROJECT_BYTES = 30_000_000
MAX_FILES = 5000

def project_root(value):
    root = Path(value).resolve(strict=True)
    if not root.is_dir():
        raise ValueError('project_path must be a directory')
    return root

def project_id(root):
    return 'code-' + hashlib.sha256(os.path.normcase(str(root)).encode('utf8')).hexdigest()[:24]

def project_file(root, value):
    candidate = (root / value).resolve(strict=True)
    if not candidate.is_relative_to(root) or not candidate.is_file():
        raise ValueError('file must be inside the selected project')
    if candidate.stat().st_size > MAX_FILE_BYTES:
        raise ValueError('file exceeds 1 MB analysis limit')
    return candidate

def check_cancelled(cancelled):
    if cancelled is not None and cancelled.is_set():
        raise InterruptedError('index cancelled')

@contextmanager
def index_lock(cancelled):
    while not INDEX_LOCK.acquire(timeout=0.1):
        check_cancelled(cancelled)
    try:
        check_cancelled(cancelled)
        yield
    finally:
        INDEX_LOCK.release()

def source_files(root, excluded=(), cancelled=None):
    total = 0
    count = 0
    for directory, dirs, files in os.walk(root, followlinks=False):
        check_cancelled(cancelled)
        dirs[:] = sorted(d for d in dirs if d not in EXCLUDES | set(excluded) and not (Path(directory) / d).is_symlink() and not getattr(Path(directory) / d, 'is_junction', lambda: False)())
        for name in sorted(files):
            check_cancelled(cancelled)
            path = Path(directory) / name
            if path.suffix.lower() not in LANGUAGES or path.is_symlink():
                continue
            path = project_file(root, str(path))
            total += path.stat().st_size
            count += 1
            if total > MAX_PROJECT_BYTES or count > MAX_FILES:
                raise ValueError('project exceeds index limits (5000 files / 30 MB); exclude generated folders')
            yield path

def build_index(root, excluded=(), cancelled=None):
    with index_lock(cancelled):
        analyzer = get_analyzer()
        old = PARSE_CACHE.get(str(root), {})
        fresh = {}
        graph, entity_files, names = {}, {}, {}
        chunks, languages, sources = [], {}, {}
        calls, imports, lines, reused = 0, 0, 0, 0
        for path in source_files(root, excluded, cancelled):
            rel = path.relative_to(root).as_posix()
            source = path.read_bytes()
            sources[rel] = source
            digest = hashlib.sha256(source).hexdigest()
            cached = old.get(rel)
            if cached and cached[0] == digest:
                parsed = cached[1]; reused += 1
            else:
                parsed = analyzer.analyze_file(str(path), source)
            check_cancelled(cancelled)
            if parsed is None:
                raise ValueError(f'could not parse {rel}')
            fresh[rel] = (digest, parsed)
            languages[parsed['language']] = languages.get(parsed['language'], 0) + 1
            calls += len(parsed['calls']); imports += len(parsed['imports']); lines += parsed['lines']
            entities = parsed['entities']
            for ent in entities:
                qualified = '.'.join(filter(None, (ent['scope'], ent['name'])))
                key = f"{rel}::{qualified}@{ent['line']}"
                graph[key] = {'callers': set(), 'callees': set(), 'unresolved': set()}
                entity_files[key] = rel
                names.setdefault(ent['name'], []).append(key)
        for rel, (_, parsed) in fresh.items():
            check_cancelled(cancelled)
            funcs = [e for e in parsed['entities'] if e['type'] == 'function']
            def key(ent):
                name = '.'.join(filter(None, (ent['scope'], ent['name'])))
                return f"{rel}::{name}@{ent['line']}"
            for call in parsed['calls']:
                check_cancelled(cancelled)
                owners = [e for e in funcs if e['start_byte'] <= call['start_byte'] < e['end_byte']]
                if not owners:
                    continue
                owner = min(owners, key=lambda e: e['end_byte']-e['start_byte'])
                candidates = []
                if '.' not in call['name']:
                    scopes = ['.'.join(filter(None, (owner['scope'], owner['name']))), owner['scope'], '']
                    for scope in dict.fromkeys(scopes):
                        candidates = [e for e in funcs if e['name'] == call['name'] and e['scope'] == scope]
                        if candidates:
                            break
                if len(candidates) == 1:
                    target = key(candidates[0]); caller = key(owner)
                    graph[caller]['callees'].add(target); graph[target]['callers'].add(caller)
                else:
                    graph[key(owner)]['unresolved'].add(call['name'])
        result_graph = {k: {field: sorted(values) for field, values in v.items()} for k, v in graph.items()}
        for rel, (_, parsed) in fresh.items():
            check_cancelled(cancelled)
            chunks.extend(symbol_chunks(rel, sources[rel], parsed, result_graph, lambda: check_cancelled(cancelled)))
        revision = hashlib.sha256('\n'.join(f'{rel}:{fresh[rel][0]}' for rel in sorted(fresh)).encode()).hexdigest()
        check_cancelled(cancelled)
        GRAPH_CACHE[str(root)] = {'graph': result_graph, 'files': entity_files, 'names': names}
        PARSE_CACHE[str(root)] = fresh
        while len(PARSE_CACHE) > 8:
            oldest = next(iter(PARSE_CACHE)); PARSE_CACHE.pop(oldest); GRAPH_CACHE.pop(oldest, None)
        return {'project_root': str(root), 'project_id': project_id(root), 'total_files': len(fresh),
                'total_lines': lines, 'languages': languages, 'total_entities': len(graph),
                'total_calls': calls, 'total_imports': imports, 'graph_entries': len(graph),
                'reused_files': reused, 'chunks': chunks, 'revision': revision, 'chunk_strategy': 'symbols-v1'}
