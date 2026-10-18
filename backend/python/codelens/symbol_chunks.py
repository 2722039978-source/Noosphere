"""Versioned symbol units with bounded parts and complete module coverage."""
import hashlib
from collections import defaultdict
from bisect import bisect_left, bisect_right

MAX_CHARS = 12000
MAX_LINES = 120


def symbol_chunks(file, source, parsed, graph, check=None):
    lines = source.decode('utf8', errors='replace').splitlines(keepends=True)
    source_hash = hashlib.sha256(source).hexdigest()
    all_entities = sorted(parsed['entities'], key=lambda e: e['line'])
    all_starts = [e['line'] - 1 for e in all_entities]
    entities = sorted((e for e in all_entities if e['type'] in ('function', 'class', 'interface', 'enum')),
                      key=lambda e: (e['line'], -e['end_line']))
    starts = [e['line'] - 1 for e in entities]
    units = []

    def partition(start, end, candidates, scope='module'):
        cursor = start
        for entity in candidates:
            if check:
                check()
            lo, hi = entity['line'] - 1, min(entity['end_line'], len(lines))
            if lo < cursor or hi > end:
                continue
            if lo > cursor:
                units.append((cursor, lo, 'module', scope))
            symbol = '.'.join(filter(None, (entity['scope'], entity['name'])))
            children = []
            if entity['type'] in ('class', 'interface') and (hi - lo > MAX_LINES or len(''.join(lines[lo:hi])) > MAX_CHARS):
                children = [e for e in entities[bisect_left(starts, lo):bisect_right(starts, hi - 1)]
                            if e is not entity and e['end_line'] <= hi and (e['line'] - 1 > lo or e['end_line'] < hi)]
            if children:
                partition(lo, hi, children, symbol)
            else:
                units.append((lo, hi, entity['type'], symbol))
            cursor = hi
        if cursor < end:
            units.append((cursor, end, 'module', scope))

    partition(0, len(lines), entities)
    chunks, occurrences = [], defaultdict(int)
    for start, end, kind, symbol in units:
        if check:
            check()
        text = ''.join(lines[start:end])
        if not text.strip():
            continue
        identity = f'{file}::{kind}:{symbol}'
        ordinal = occurrences[identity]
        occurrences[identity] += 1
        parts, offset, line = [], 0, start + 1
        while offset < len(text):
            stop = min(offset + MAX_CHARS, len(text))
            # Prefer complete lines; a very long line is explicitly split into parts.
            if stop < len(text):
                newline = text.rfind('\n', offset, stop)
                if newline > offset:
                    stop = newline + 1
            content = text[offset:stop]
            end_line = line + content.count('\n') - (1 if content.endswith('\n') else 0)
            parts.append((content, line, max(line, end_line)))
            line += content.count('\n')
            offset = stop
        relevant = all_entities[bisect_left(all_starts, start):bisect_left(all_starts, end)]
        keys = [f"{file}::{'.'.join(filter(None, (e['scope'], e['name'])))}@{e['line']}" for e in relevant]
        for part, (content, lo, hi) in enumerate(parts):
            chunks.append({'id': f'{identity}#{ordinal}/part{part+1}', 'file': file,
                           'start_line': lo, 'end_line': hi, 'language': parsed['language'],
                           'content': content, 'kind': kind, 'symbol': symbol, 'part': part+1, 'parts': len(parts),
                           'source_hash': source_hash, 'content_hash': hashlib.sha256(content.encode()).hexdigest(),
                           'symbols': sorted(set(e['name'] for e in relevant)),
                           'callees': sorted({v for key in keys for v in graph.get(key, {}).get('callees', [])}),
                           'callers': sorted({v for key in keys for v in graph.get(key, {}).get('callers', [])}),
                           'unresolved': sorted({v for key in keys for v in graph.get(key, {}).get('unresolved', [])})})
    return chunks
