"""Run the server in the base interpreter using only the isolated venv packages.

Windows venv python.exe is a redirector. Launching the base interpreter with
-I -S avoids a separate unowned child PID and ignores global/user site packages.
"""
from pathlib import Path
import runpy
import site
import sys

runtime_home = Path(sys.argv.pop(1)).resolve()
site.addsitedir(str(runtime_home / "venv" / "Lib" / "site-packages"))
source = Path(__file__).resolve().parent
sys.path.insert(0, str(source))
sys.argv[0] = str(source / "server.py")
runpy.run_path(sys.argv[0], run_name="__main__")
