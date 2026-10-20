"""Apply the pinned Juggler patch without unpacking unrelated Firefox resources."""
import pathlib
import subprocess
import sys
import tempfile
import zipfile

archive, patch = map(pathlib.Path, sys.argv[1:])
prefix = 'chrome/juggler/content/'
with tempfile.TemporaryDirectory() as tmp:
    root = pathlib.Path(tmp)
    with zipfile.ZipFile(archive) as source:
        for name in ('protocol/PageHandler.js', 'protocol/Protocol.js', 'TargetRegistry.js', 'content/main.js'):
            dest = root / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(source.read(prefix + name))
        subprocess.run(['patch', '--batch', '--fuzz=0', '-p1', '-i', str(patch.resolve())], cwd=root, check=True)
        output = root / 'omni.ja'
        with zipfile.ZipFile(output, 'w') as target:
            target.comment = source.comment
            for entry in source.infolist():
                replacement = root / entry.filename.removeprefix(prefix)
                data = replacement.read_bytes() if entry.filename.startswith(prefix) and replacement.is_file() else source.read(entry)
                target.writestr(entry, data)
    archive.write_bytes(output.read_bytes())
