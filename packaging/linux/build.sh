#!/usr/bin/env bash
# Assemble the Linux package: build the binary and stage it with the installer
# and udev rule. Produces packaging/linux/dist/ and a tarball.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
dist="$here/dist/certool-linux"
arch="${1:-amd64}"

echo "building certool (linux/$arch)…"
mkdir -p "$dist"
( cd "$root" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w" -o "$dist/certool" ./cmd/certool )

echo "staging…"
cp "$here/install.sh" "$here/99-certool-usbtmc.rules" "$dist/"
cp "$root/docs/USER-GUIDE.md" "$dist/USER-GUIDE.md"
chmod +x "$dist/install.sh"

echo "tarball…"
( cd "$here/dist" && rm -f "certool-linux-$arch.tar.gz" && \
  tar czf "certool-linux-$arch.tar.gz" certool-linux )

echo "done:"
ls -lh "$dist" "$here/dist/certool-linux-$arch.tar.gz"
