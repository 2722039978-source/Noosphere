import tempfile
import unittest
import threading
from unittest.mock import patch
from pathlib import Path

from main import IndexRequest, index_project, index_project_request, GRAPH_CACHE, AnalyzeRequest, analyze_file
from project_index import build_index, PARSE_CACHE


class ProjectIndexTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / 'a.py').write_text('标题 = "中文"\ndef target():\n    return 1\ndef caller():\n    return target()\ndef idle():\n    return 0\n', encoding='utf8')
        (self.root / 'b.py').write_text('def target():\n    return 2\n', encoding='utf8')

    def test_chunks_include_source_and_project_identity(self):
        result = index_project(IndexRequest(project_path=str(self.root)))
        self.assertTrue(result.get('project_id'))
        self.assertTrue(any('return target()' in c['content'] for c in result.get('chunks', [])))
        self.assertTrue(all(c['file'] in ('a.py', 'b.py') and c['start_line'] > 0 for c in result['chunks']))

    def test_call_graph_tracks_owner_and_same_named_symbols(self):
        index_project(IndexRequest(project_path=str(self.root)))
        graph = GRAPH_CACHE[str(self.root.resolve())]['graph']
        targets = [key for key in graph if '::target@' in key]
        self.assertEqual(len(targets), 2)
        caller = next(v for k, v in graph.items() if '::caller@' in k)
        idle = next(v for k, v in graph.items() if '::idle@' in k)
        self.assertEqual(len(caller['callees']), 1)
        self.assertIn('a.py::target@', caller['callees'][0])
        self.assertEqual(idle['callees'], [])

    def test_reindex_removes_deleted_file_and_reuses_unchanged_parse(self):
        index_project(IndexRequest(project_path=str(self.root)))
        (self.root / 'b.py').unlink()
        result = index_project(IndexRequest(project_path=str(self.root)))
        self.assertEqual(result.get('reused_files'), 1)
        self.assertTrue(all(c['file'] != 'b.py' for c in result['chunks']))

    def test_analyze_contract_accepts_project_and_relative_file(self):
        result = analyze_file(AnalyzeRequest(project_path=str(self.root), file='a.py'))
        names = [e['name'] for e in result['entities']]
        self.assertIn('caller', names)
        self.assertIn('target', names)

    def test_cancel_mid_parse_keeps_previous_snapshot(self):
        index_project(IndexRequest(project_path=str(self.root)))
        root = self.root.resolve()
        before_graph, before_parse = GRAPH_CACHE[str(root)], PARSE_CACHE[str(root)]
        (root / 'a.py').write_text('def changed():\n    return 3\n', encoding='utf8')
        cancelled = threading.Event()
        from ast_parser import get_analyzer
        analyzer = get_analyzer()
        original = analyzer.analyze_file
        def parse(path, source=None):
            result = original(path, source)
            cancelled.set()
            return result
        with patch.object(analyzer, 'analyze_file', side_effect=parse):
            with self.assertRaises(InterruptedError):
                build_index(root, cancelled=cancelled)
        self.assertIs(GRAPH_CACHE[str(root)], before_graph)
        self.assertIs(PARSE_CACHE[str(root)], before_parse)


class RequestCancellationTests(unittest.IsolatedAsyncioTestCase):
    async def test_disconnected_transport_signals_parser(self):
        from fastapi import HTTPException
        stopped = threading.Event()
        class Disconnected:
            async def is_disconnected(self):
                return True
        def parser(_req, cancelled):
            if not cancelled.wait(2):
                raise AssertionError('disconnect never reached parser')
            stopped.set()
            raise InterruptedError()
        with patch('main.index_project', side_effect=parser):
            with self.assertRaises(HTTPException) as error:
                await index_project_request(IndexRequest(project_path='fixture'), Disconnected())
        self.assertEqual(error.exception.status_code, 499)
        self.assertTrue(stopped.is_set())

if __name__ == '__main__':
    unittest.main()
