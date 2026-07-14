#!/usr/bin/env bash
# certool Linux installer — installs the binary and the USBTMC udev rule.
# On Linux the tool talks to instruments directly via /dev/usbtmc* (no Python
# or VISA runtime needed). Usage: ./install.sh [bin-dir]   (default /usr/local/bin)
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
BIN="${1:-/usr/local/bin}"

echo "certool installer"
echo "================="

[ -f "$here/certool" ] || { echo "error: certool binary not found next to this script"; exit 1; }

echo "• Installing binary -> $BIN/certool  (sudo)"
sudo install -m 0755 "$here/certool" "$BIN/certool"

echo "• Installing USBTMC udev rule -> /etc/udev/rules.d/  (sudo)"
sudo install -m 0644 "$here/99-certool-usbtmc.rules" /etc/udev/rules.d/99-certool-usbtmc.rules
sudo udevadm control --reload-rules
sudo udevadm trigger --subsystem-match=usbmisc || true

echo
echo "Done."
if ! id -nG "$USER" | tr ' ' '\n' | grep -qx plugdev; then
    echo "• Add yourself to 'plugdev' for instrument access, then re-login:"
    echo "    sudo usermod -aG plugdev $USER"
fi
echo "• Run:  certool -out ~/certool-campaigns"
echo "  then open http://127.0.0.1:8787/"
