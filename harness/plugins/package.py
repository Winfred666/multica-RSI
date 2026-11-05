#!/usr/bin/env python3
"""Build a ZIP whose single executable includes the shared standard-library helpers."""
import argparse
import json
from pathlib import Path
from zipfile import ZIP_DEFLATED, ZipFile


def package(source, output):
    manifest = json.loads((source / "multica.plugin.json").read_text())
    code = (source.parent / "runtime.py").read_text() + "\n" + (source / "hook.py").read_text()
    with ZipFile(output, "w", ZIP_DEFLATED) as bundle:
        bundle.writestr("multica.plugin.json", json.dumps(manifest, indent=2))
        bundle.writestr("hook.py", code)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("plugin", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    package(args.plugin.resolve(), args.output)
