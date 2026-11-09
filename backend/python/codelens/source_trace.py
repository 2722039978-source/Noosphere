from collections import defaultdict
from functools import lru_cache
import gzip
import hashlib
import json
import os
from pathlib import Path
import posixpath
import re
import tempfile


MAX_RELATIONS = 60_000
MAX_MATCHES = 12
MAX_RESULTS = 24
MAX_CANDIDATES = 3


def snapshot_path(project_id, revision):
    directory = os.environ.get('NOOSPHERE_CODELENS_TRACE_DIR')
    if not directory:
        return None
    return Path(directory) / project_id / f'{revision}.json.gz'


def save_snapshot(project_id, snapshot, check):
    target = snapshot_path(project_id, snapshot['revision'])
    if target is None:
        return
    target.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=target.parent, delete=False) as pending:
        temporary = Path(pending.name)
        try:
            payload = json.dumps(snapshot, separators=(',', ':'), ensure_ascii=False).encode('utf-8')
            with gzip.GzipFile(fileobj=pending, mode='wb', compresslevel=3) as compressed:
                compressed.write(payload)
            pending.close()
            check()
            temporary.replace(target)
        finally:
            pending.close()
            temporary.unlink(missing_ok=True)


@lru_cache(maxsize=4)
def load_snapshot(project_id, revision):
    target = snapshot_path(project_id, revision)
    if target is None or not target.is_file():
        return None
    with gzip.open(target, 'rt', encoding='utf-8') as handle:
        snapshot = json.load(handle)
    if snapshot['revision'] != revision:
        raise ValueError('已保存的源码索引版本无效。')
    return snapshot


def source_location(file, digest, symbol, start, end):
    return {'file': file, 'source_hash': digest, 'symbol': symbol, 'start_line': start, 'end_line': end}


def relative_modules(file, specifier, indexed):
    if not specifier.startswith(('./', '../')):
        return []
    module = posixpath.normpath(posixpath.join(posixpath.dirname(file), specifier))
    extension = posixpath.splitext(module)[1]
    supported = ('.ts', '.tsx', '.js', '.jsx')
    if extension == '.js':
        paths = (module, module[:-3] + '.ts', module[:-3] + '.tsx')
    elif extension == '.jsx':
        paths = (module, module[:-4] + '.tsx')
    elif extension in supported:
        paths = (module,)
    else:
        paths = tuple(module + ext for ext in supported) + tuple(module + '/index' + ext for ext in supported)
    return [item for item in paths if item in indexed]


def trace_snapshot(indexed, graph, revision, check):
    locations, links = {}, defaultdict(list)
    go_packages, functions = defaultdict(list), defaultdict(list)
    relations = []
    truncated = False
    for file, (digest, parsed) in indexed.items():
        check()
        locations[file] = source_location(file, digest, file, 1, 1)
        for entity in parsed['entities']:
            symbol = '.'.join(filter(None, (entity['scope'], entity['name'])))
            key = f"{file}::{symbol}@{entity['line']}"
            locations[key] = source_location(file, digest, symbol, entity['line'], entity['end_line'])
            if entity['type'] == 'function' and not entity['scope']:
                functions[file, entity['name']].append(key)
                if parsed['trace_package']:
                    go_packages[posixpath.dirname(file), parsed['trace_package'], entity['name']].append(key)

    def add(kind, file, owner, source, targets):
        nonlocal truncated
        if len(relations) >= MAX_RELATIONS:
            truncated = True
            return
        targets = list(dict.fromkeys(targets))
        item = {'kind': kind, 'source': source, 'status': 'unresolved', 'reason': '当前索引未确定目标。'}
        if kind == 'import' and len(targets) == 1:
            item.update(status='resolved', target=locations[targets[0]], reason='相对导入路径在当前索引中唯一匹配。')
        elif targets:
            reason = '基于名称和作用域找到候选；调用目标未经类型检查。' if kind == 'call' else '相对导入路径匹配多个文件。'
            item.update(status='candidate', candidates=[locations[key] for key in targets[:MAX_CANDIDATES]], reason=reason)
            if len(targets) > MAX_CANDIDATES:
                item['reason'] += '仅显示前三个候选。'
        for key in dict.fromkeys([file, owner, *targets[:MAX_CANDIDATES], *(locations[key]['file'] for key in targets[:MAX_CANDIDATES])]):
            links[key].append(len(relations))
        relations.append(item)

    for file, (digest, parsed) in indexed.items():
        check()
        imports = {}
        for item in parsed['trace_imports']:
            check()
            targets = relative_modules(file, item['path'], indexed)
            add('import', file, file, source_location(file, digest, item['path'], item['line'], item['end_line']), targets)
            for local, exported in item['bindings'].items():
                imports.setdefault(local, []).extend(key for target in targets for key in functions[target, exported])
        scope = [entity for entity in parsed['entities'] if entity['type'] == 'function']
        for call in parsed['calls']:
            check()
            name = call['name']
            owners = [entity for entity in scope if entity['start_byte'] <= call['start_byte'] < entity['end_byte']]
            owner = file
            targets = []
            if owners:
                entry = min(owners, key=lambda entity: entity['end_byte'] - entity['start_byte'])
                symbol = '.'.join(filter(None, (entry['scope'], entry['name'])))
                owner = f"{file}::{symbol}@{entry['line']}"
                targets.extend(key for key in graph[owner]['callees'] if locations[key]['symbol'].split('.')[-1] == name)
            if name.isidentifier():
                targets.extend(imports.get(name, []))
                if parsed['trace_package'] and not targets:
                    targets.extend(go_packages.get((posixpath.dirname(file), parsed['trace_package'], name), []))
            add('call', file, owner, source_location(file, digest, name[:512], call['line'], call['line']), targets)

    warnings = ['关系来自静态索引。依赖配置、运行时选择和动态调用可能需要额外核对。']
    if any(parsed['trace_parse_error'] for _, parsed in indexed.values()):
        warnings.append('部分文件包含语法错误，相关结果可能不完整。')
    if truncated:
        warnings.append('项目关系数量超过索引上限，结果只覆盖已记录的关系。')
    return {'revision': revision, 'locations': locations, 'links': links, 'relations': relations, 'warnings': warnings}


def find_trace(root, snapshot, query):
    if not query.strip():
        raise ValueError('请输入函数、文件或关键词。')
    normalized = query.strip().replace('\\', '/')
    needle = normalized.casefold()
    tokens = [token.rstrip('.') for token in re.findall(r'[A-Za-z_$][A-Za-z0-9_$./-]*', normalized)]
    terms = {token.casefold() for token in tokens if len(token) >= 3}
    preferred = {token.casefold() for token in tokens if any(c in token for c in '_$/.') or re.search(r'[a-z][A-Z]', token)}
    matches = []
    warnings = list(snapshot['warnings'])
    versions = {}

    def current(location):
        file = location['file']
        if file not in versions:
            path = root / file
            if path.is_file() and path.resolve().is_relative_to(root) and not path.is_symlink():
                with path.open('rb') as handle:
                    raw = handle.read(1_000_001)
                versions[file] = hashlib.sha256(raw).hexdigest() if len(raw) <= 1_000_000 else ''
            else:
                versions[file] = ''
        if versions[file] == location['source_hash']:
            return True
        message = '部分源文件已变更或无法读取，请更新项目索引。'
        if message not in warnings:
            warnings.append(message)
        return False

    ranked = []
    for key, item in snapshot['locations'].items():
        symbol, file = item['symbol'].casefold(), item['file'].casefold()
        identities = {file, posixpath.basename(file)} if key == item['file'] else {symbol, symbol.rsplit('.', 1)[-1]}
        if needle in identities:
            rank = (0, 0)
        elif preferred & identities:
            rank = (1, -max(map(len, preferred & identities)))
        elif needle in symbol or (key == item['file'] and needle in file):
            rank = (2, 0)
        elif terms & identities:
            rank = (3, 0)
        else:
            continue
        ranked.append((rank, key))

    ids = {}
    relations = []
    for _, key in sorted(ranked):
        item = snapshot['locations'][key]
        if not current(item):
            continue
        matches.append(item)
        for index in snapshot['links'].get(key, []):
            ids.setdefault(index, None)
        if len(matches) >= MAX_MATCHES:
            break

    def relation_rank(index):
        source = snapshot['relations'][index]['source']
        symbol = source['symbol'].casefold()
        names = {symbol, symbol.rsplit('.', 1)[-1]}
        rank = 0 if needle in names or preferred & names else (1 if terms & names else 2)
        return rank, bool(matches and source['file'] != matches[0]['file'])

    for index in sorted(ids, key=relation_rank):
        relation = snapshot['relations'][index]
        if not current(relation['source']):
            continue
        targets = [relation['target']] if 'target' in relation else relation.get('candidates', [])
        if not all(current(target) for target in targets):
            relation = {'kind': relation['kind'], 'source': relation['source'], 'status': 'unresolved',
                        'reason': '目标文件版本已变化，请更新索引。'}
        relations.append(relation)
        if len(relations) >= MAX_RESULTS:
            break
    if len(matches) >= MAX_MATCHES or len(relations) >= MAX_RESULTS:
        warnings.append('已达到本次查询的结果上限，可以输入更明确的函数名或文件名。')
    return {'revision': snapshot['revision'], 'matches': matches, 'relations': relations, 'warnings': warnings}
