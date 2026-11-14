"""Download the pinned official OpenVINO IR; verify every file before publication.

The optional ModelScope transport uses the same bytes verified against the
official Hugging Face revision (Git blob IDs for small files, SHA256 for LFS).
No remote Python or conversion code is executed. TLS validation stays enabled.
"""
import argparse
import concurrent.futures
import hashlib
import json
from pathlib import Path
import time
import urllib.error
import urllib.request


def digest(path):
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def verified(path, item):
    return path.is_file() and path.stat().st_size == item["size"] and digest(path) == item["sha256"]


def download(item, target, manifest, mirror, direct):
    name = item["name"]
    if Path(name).name != name or name in (".", ".."):
        raise ValueError("Only manifest filenames in the model directory are accepted")
    dest = target / name
    if verified(dest, item):
        return {"name": name, "status": "already_verified", "size": item["size"]}
    part = dest.with_name(dest.name + ".partial")
    transport = manifest["mirror"] if mirror else manifest["source"]
    url = f'{transport["base_url"]}/{transport["revision"]}/{name}'
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({})) if direct else urllib.request.build_opener()
    for attempt in range(5):
        try:
            offset = part.stat().st_size if part.exists() else 0
            if offset > item["size"]:
                raise ValueError(f"Oversized partial retained for inspection: {part}")
            while offset < item["size"]:
                end = min(offset + 8 * 1024 * 1024, item["size"]) - 1
                request = urllib.request.Request(url, headers={"Range": f"bytes={offset}-{end}", "User-Agent": "Noosphere-OpenVINO-Installer/1"})
                with opener.open(request, timeout=60) as response:
                    if response.status == 206:
                        expected = f"bytes {offset}-{end}/{item['size']}"
                        if response.headers.get("Content-Range") != expected:
                            raise ValueError("Incorrect Content-Range")
                        limit = end - offset + 1
                    elif response.status == 200 and offset == 0:
                        limit = item["size"]
                    else:
                        raise ValueError(f"Server did not honor resumed range: {response.status}")
                    written = 0
                    with part.open("ab") as f:
                        while written < limit:
                            block = response.read(min(1024 * 1024, limit - written))
                            if not block:
                                raise OSError("Unexpected EOF")
                            f.write(block)
                            written += len(block)
                    offset += written
            if not verified(part, item):
                raise ValueError(f"SHA256 mismatch; partial retained: {part}")
            # The official revision uses Git blob SHA1 for non-LFS files.
            if item.get("git_blob_sha1"):
                h = hashlib.sha1(f"blob {item['size']}\0".encode())
                h.update(part.read_bytes())
                if h.hexdigest() != item["git_blob_sha1"]:
                    raise ValueError(f"Official Git blob mismatch: {name}")
            part.replace(dest)
            return {"name": name, "status": "downloaded_verified", "size": item["size"], "sha256": item["sha256"]}
        except (OSError, urllib.error.URLError) as exc:
            if attempt == 4:
                raise
            print(f"retry {name}: {type(exc).__name__}: {exc}", flush=True)
            time.sleep(2 * (attempt + 1))
    raise RuntimeError("unreachable")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--target", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, default=Path(__file__).with_name("model.lock.json"))
    parser.add_argument("--mirror", action="store_true", help="Use the pinned byte-equivalent ModelScope transport")
    parser.add_argument("--direct", action="store_true", help="Bypass proxies for this downloader process only; TLS remains verified")
    parser.add_argument("--report", type=Path)
    args = parser.parse_args()
    manifest = json.loads(args.manifest.read_text(encoding="utf-8-sig"))
    args.target.mkdir(parents=True, exist_ok=True)
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        futures = [pool.submit(download, f, args.target, manifest, args.mirror, args.direct) for f in manifest["files"]]
        results = []
        for future in concurrent.futures.as_completed(futures):
            result = future.result()
            results.append(result)
            print(json.dumps(result), flush=True)
    report = {"source": manifest["source"], "mirror_used": args.mirror, "verified": True, "files": sorted(results, key=lambda f: f["name"])}
    if args.report:
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(report, indent=2), encoding="utf-8")


if __name__ == "__main__":
    main()
