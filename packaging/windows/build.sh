#!/usr/bin/env bash
# Assemble the Windows bench bundle: cross-compile certool.exe and stage it
# alongside the venv-bootstrap installer. Produces packaging/windows/dist/ and a zip.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
dist="$here/dist/certool-windows"

echo "building certool.exe (windows/amd64)…"
( cd "$root" && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w" -o "$dist/certool.exe" ./cmd/certool )

echo "staging installer files…"
cp "$here/install.bat" "$here/run.bat" "$here/README.txt" "$here/requirements.txt" "$dist/"

# Ship a copy of the pyvisa helper for transparency (the exe also embeds it).
cp "$root/internal/connector/scpi/instr_helper.py" "$dist/instr_helper.py"
# Ship the user guide.
cp "$root/docs/USER-GUIDE.md" "$dist/USER-GUIDE.md"

# CRLF line endings for Windows-consumed text files. cmd.exe mis-parses LF-only
# .bat files (fragments run as commands); Notepad wants CRLF too. The .py stays
# LF (Python is fine with either).
echo "converting .bat/.txt to CRLF…"
for f in "$dist"/*.bat "$dist"/README.txt "$dist"/requirements.txt; do
    sed -i 's/\r$//' "$f"   # strip any existing CR first
    sed -i 's/$/\r/' "$f"   # then append CR -> CRLF
done

echo "zipping…"
( cd "$here/dist" && rm -f certool-windows.zip && \
  ( command -v zip >/dev/null && zip -rq certool-windows.zip certool-windows || \
    python3 -c "import shutil; shutil.make_archive('certool-windows','zip','.','certool-windows')" ) )

echo "done:"
ls -lh "$dist" "$here/dist/certool-windows.zip"
