"""Fetch one small real weight into the runner's temporary directory for native CI."""
from http.client import IncompleteRead
import hashlib
import os
from pathlib import Path
import shutil
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.request import urlopen

FILENAME = "SmolLM2-135M-Instruct-Q4_K_M.gguf"
REVISION = "09816acd5d99df7be770d85ea30822623dab342c"
URL = f"https://huggingface.co/bartowski/SmolLM2-135M-Instruct-GGUF/resolve/{REVISION}/{FILENAME}"
SIZE = 105454432
SHA256 = "2e8040ceae7815abe0dcb3540b9995eaa1fa0d2ca9e797d0a635ae4433c68c2d"


def verify(path):
    if path.stat().st_size != SIZE:
        raise ValueError("Unexpected smoke model size")
    with path.open("rb") as stream:
        digest = hashlib.sha256()
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    if digest.hexdigest() != SHA256:
        raise ValueError("Unexpected smoke model digest")


def main():
    directory = Path(os.environ["RUNNER_TEMP"])
    target = directory / FILENAME
    if target.exists():
        verify(target)
    else:
        with tempfile.NamedTemporaryFile(dir=directory, delete=False) as output:
            staged = Path(output.name)
        try:
            for attempt in range(3):
                try:
                    with urlopen(URL + "?download=true", timeout=120) as source, staged.open("wb") as output:
                        shutil.copyfileobj(source, output)
                    verify(staged)
                    staged.replace(target)
                    break
                except HTTPError as error:
                    if error.code not in (408, 429, 500, 502, 503, 504) or attempt == 2:
                        raise
                except (URLError, TimeoutError, ConnectionError, IncompleteRead):
                    if attempt == 2:
                        raise
                time.sleep(2 ** attempt)
        finally:
            staged.unlink(missing_ok=True)
    with Path(os.environ["GITHUB_ENV"]).open("a", encoding="utf-8") as env:
        env.write(f"NOOSPHERE_TEST_GGUF={target}\n")
    print(f"Verified {FILENAME} ({SIZE} bytes)")


if __name__ == "__main__":
    main()
