import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from fastapi.testclient import TestClient
from main import app

class SecurityTests(unittest.TestCase):
    def test_missing_or_invalid_worker_credentials_are_denied(self):
        with patch.dict(os.environ, {'NOOSPHERE_CODELENS_TOKEN':'test-secret'}):
            with TestClient(app) as client:
                self.assertEqual(client.get('/api/v1/health').status_code,401)
                self.assertEqual(client.get('/api/v1/health',headers={'Authorization':'Bearer wrong'}).status_code,401)
                self.assertEqual(client.get('/api/v1/health',headers={'Authorization':'Bearer test-secret'}).status_code,200)

    def test_file_analysis_cannot_escape_selected_root(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {'NOOSPHERE_CODELENS_TOKEN':'test-secret'}):
            root=Path(directory)/'project';root.mkdir()
            (Path(directory)/'outside.py').write_text('password = "private"')
            with TestClient(app) as client:
                response=client.post('/api/v1/analyze/file',headers={'Authorization':'Bearer test-secret'},json={'project_path':str(root),'file':'../outside.py'})
                self.assertEqual(response.status_code,400)
                self.assertNotIn('private',response.text)

if __name__=='__main__': unittest.main()
