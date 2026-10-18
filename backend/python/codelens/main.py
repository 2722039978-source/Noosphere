"""CodeLens AI - tree-sitter AST code analysis service."""
import os
import hmac
import time
import asyncio
import threading
from fastapi import FastAPI, HTTPException, Request
from pydantic import BaseModel, Field
import uvicorn
from ast_parser import get_analyzer, LANGUAGES
from project_index import GRAPH_CACHE, INDEX_LOCK, build_index, project_root, project_file, source_files

app = FastAPI(title='CodeLens AI', version='0.6.0')

@app.middleware('http')
async def worker_auth(request, call_next):
    from fastapi.responses import JSONResponse
    token = os.environ.get('NOOSPHERE_CODELENS_TOKEN', '')
    if not token:
        return JSONResponse(status_code=503, content={'error':'worker authentication is not configured'})
    if not hmac.compare_digest(request.headers.get('Authorization', '').encode(), ('Bearer '+token).encode()):
        return JSONResponse(status_code=401, content={'error':'unauthorized'})
    return await call_next(request)

class IndexRequest(BaseModel):
    project_path: str
    exclude: list[str] = Field(default_factory=list)

class SearchRequest(BaseModel):
    query: str = Field(min_length=1, max_length=4000)
    project_path: str
    max_results: int = Field(default=20, ge=1, le=100)

class AnalyzeRequest(BaseModel):
    project_path: str
    file: str

class GraphRequest(BaseModel):
    project_path: str

@app.exception_handler(ValueError)
async def invalid_value(_request, error):
    from fastapi.responses import JSONResponse
    return JSONResponse(status_code=400, content={'error': str(error)})

@app.exception_handler(OSError)
async def invalid_path(_request, error):
    from fastapi.responses import JSONResponse
    return JSONResponse(status_code=400, content={'error': str(error)})

@app.get('/api/v1/health')
def health():
    return {'status': 'ok', 'service': 'codelens', 'version': '0.6.0', 'parser': 'tree-sitter', 'languages': list(LANGUAGES.values())}

def index_project(req: IndexRequest, cancelled=None):
    result = build_index(project_root(req.project_path), req.exclude, cancelled)
    result['indexed_at'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
    return result

@app.post('/api/v1/index')
async def index_project_request(req: IndexRequest, request: Request):
    # Closing the Go proxy request cooperatively stops parsing between files or
    # graph steps; a cancelled build never replaces the previous cache snapshot.
    cancelled = threading.Event()
    task = asyncio.create_task(asyncio.to_thread(index_project, req, cancelled))
    try:
        while not task.done():
            await asyncio.wait({task}, timeout=0.1)
            if not task.done() and await request.is_disconnected():
                cancelled.set()
        return await task
    except InterruptedError:
        raise HTTPException(499, 'index cancelled')
    finally:
        cancelled.set()
        if not task.done():
            task.add_done_callback(lambda done: done.exception() if not done.cancelled() else None)

@app.post('/api/v1/search')
def search_code(req: SearchRequest):
    root = project_root(req.project_path)
    results = []
    for path in source_files(root):
        for number, line in enumerate(path.read_text(encoding='utf8', errors='replace').splitlines(), 1):
            if req.query.casefold() in line.casefold():
                results.append({'file': path.relative_to(root).as_posix(), 'line': number, 'content': line.strip()[:500]})
                if len(results) >= req.max_results:
                    return {'query': req.query, 'results': results, 'count': len(results)}
    return {'query': req.query, 'results': results, 'count': len(results)}

@app.post('/api/v1/analyze/file')
def analyze_file(req: AnalyzeRequest):
    path = project_file(project_root(req.project_path), req.file)
    with INDEX_LOCK:
        result = get_analyzer().analyze_file(str(path))
    if result is None:
        raise HTTPException(400, 'unsupported file type')
    return {**result, 'size_bytes': path.stat().st_size}

def graph_entry(req, name):
    cache = GRAPH_CACHE.get(str(project_root(req.project_path)))
    if not cache:
        raise HTTPException(409, 'index this project first')
    if name in cache['graph']:
        return cache, name
    candidates = cache['names'].get(name, [])
    if len(candidates) != 1:
        raise HTTPException(409, {'message': 'symbol missing or ambiguous; use its qualified id', 'candidates': candidates})
    return cache, candidates[0]

@app.post('/api/v1/call-chain/{name:path}')
def call_chain(name: str, req: GraphRequest):
    cache, key = graph_entry(req, name)
    return {'entity': key, **cache['graph'][key], 'file': cache['files'][key], 'resolution': 'same-file static calls; unresolved calls are listed separately'}

@app.post('/api/v1/impact/{name:path}')
def impact_analysis(name: str, req: GraphRequest):
    cache, key = graph_entry(req, name)
    direct = set(cache['graph'][key]['callers'])
    seen, queue = {key} | direct, list(direct)
    while queue:
        for caller in cache['graph'][queue.pop()]['callers']:
            if caller not in seen:
                seen.add(caller); queue.append(caller)
    indirect = seen - direct - {key}
    return {'entity': key, 'direct': sorted(direct), 'indirect': sorted(indirect), 'total_impact': len(direct | indirect)}

@app.post('/api/v1/graph')
def code_graph(req: GraphRequest):
    cache = GRAPH_CACHE.get(str(project_root(req.project_path)))
    if not cache:
        raise HTTPException(409, 'index this project first')
    ranked = sorted(cache['graph'].items(), key=lambda item: len(item[1]['callers'])+len(item[1]['callees']), reverse=True)
    return {'entities': len(ranked), 'top_connected': [{'name': name, 'callers': len(edges['callers']), 'callees': len(edges['callees'])} for name, edges in ranked[:30]]}

if __name__ == '__main__':
    uvicorn.run(app, host='127.0.0.1', port=int(os.environ.get('NOOSPHERE_CODELENS_PORT', '18765')), log_level='info')
