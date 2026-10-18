import hashlib
import tempfile
import unittest
from pathlib import Path
from main import IndexRequest, index_project
from symbol_chunks import symbol_chunks


class CountedEntities(list):
    def __init__(self, items):
        super().__init__(items)
        self.visits = 0

    def __iter__(self):
        for item in super().__iter__():
            self.visits += 1
            yield item


class SymbolChunkTests(unittest.TestCase):
    def test_many_functions_do_not_rescan_all_entities_for_each_chunk(self):
        size = 1000
        source = ''.join(f'def f{i}(): return {i}\n' for i in range(size)).encode()
        entities = CountedEntities([{'type': 'function', 'scope': '', 'name': f'f{i}', 'line': i+1, 'end_line': i+1} for i in range(size)])
        chunks = symbol_chunks('many.py', source, {'entities': entities, 'language': 'python'}, {})
        self.assertEqual(len(chunks), size)
        self.assertLess(entities.visits, size * 5, 'quadratic full-entity scans')

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def index(self):
        return index_project(IndexRequest(project_path=str(self.root)))

    def test_function_is_complete_and_identity_survives_line_insertions(self):
        path = self.root / 'a.py'
        body = 'def compute():\n' + ''.join(f'    item_{i} = {i}\n' for i in range(80)) + '    return item_79\n'
        path.write_text(body, encoding='utf8')
        first = self.index()
        chunk = next(c for c in first['chunks'] if c.get('symbol') == 'compute')
        self.assertIn('return item_79', chunk['content'])
        self.assertEqual(chunk['kind'], 'function')
        path.write_text('# a new comment\n' + body, encoding='utf8')
        second = self.index()
        updated = next(c for c in second['chunks'] if c.get('symbol') == 'compute')
        self.assertEqual(chunk['id'], updated['id'])
        self.assertEqual(chunk['content_hash'], updated['content_hash'])
        self.assertNotEqual(chunk['source_hash'], updated['source_hash'])
        self.assertEqual(updated['start_line'], chunk['start_line'] + 1)

    def test_large_class_retains_methods_and_long_lines_are_not_silently_lost(self):
        source = 'import math\nclass Service:\n' + ''.join(f'    def method_{i}(self):\n        return {i}\n' for i in range(90))
        (self.root / 'a.py').write_text(source, encoding='utf8')
        (self.root / 'b.ts').write_text('export const run = () => "' + 'z' * 25000 + '";\n', encoding='utf8')
        chunks = self.index()['chunks']
        self.assertTrue(any(c.get('symbol') == 'Service.method_89' for c in chunks))
        self.assertTrue(any('import math' in c['content'] for c in chunks))
        self.assertTrue(all(len(c['content']) <= 12000 for c in chunks))
        self.assertEqual(sum(c['content'].count('z') for c in chunks if c['file'] == 'b.ts'), 25000)

    def test_chunks_include_version_and_conservative_call_evidence(self):
        source = 'def target():\n    return 1\ndef caller():\n    return target()\n'
        (self.root / 'a.py').write_text(source, encoding='utf8')
        result = self.index()
        caller = next(c for c in result['chunks'] if c.get('symbol') == 'caller')
        self.assertEqual(caller['source_hash'], hashlib.sha256((self.root / 'a.py').read_bytes()).hexdigest())
        self.assertTrue(any('target' in target for target in caller['callees']))
        self.assertEqual(result['chunk_strategy'], 'symbols-v1')
        self.assertEqual(len(result['revision']), 64)


if __name__ == '__main__':
    unittest.main()
