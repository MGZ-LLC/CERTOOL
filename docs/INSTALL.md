# CERTOOL — installation

CERTOOL is a single local binary that serves a web UI at
`http://127.0.0.1:8787/`. Pick your platform.

---

## Linux

On Linux the tool talks to USBTMC instruments **directly via `/dev/usbtmc*`** —
no Python or VISA runtime is needed.

1. Download and unpack the `certool-linux-amd64.tar.gz` package.
2. Run the installer (installs the binary and a udev rule for instrument access):
   ```sh
   cd certool-linux
   ./install.sh          # uses sudo for /usr/local/bin + /etc/udev/rules.d
   ```
3. One-time: make sure your user is in the `plugdev` group, then log out/in:
   ```sh
   sudo usermod -aG plugdev "$USER"
   ```
4. Run it:
   ```sh
   certool -out ~/certool-campaigns
   ```
   A browser opens at `http://127.0.0.1:8787/`.

**Instruments:** plug the Keysight/Rigol in by USB; they appear as
`/dev/usbtmc0`, `/dev/usbtmc1`, … and the `bench-4wire` connector auto-discovers
them by `*IDN?`. If a device stays root-only, re-check the udev rule and the
`plugdev` membership.

---

## Windows

Windows has no built-in USBTMC support, so live instrument control goes through a
small **Python/pyvisa bridge** that the installer sets up in a self-contained
`.venv` (nothing is installed system-wide by the tool).

1. Download and unpack the `certool-windows` bundle to a writable folder
   (e.g. the Desktop) — **not** a read-only location.
2. Install **Python 3** from python.org — tick *Add Python to PATH*.
3. Run **`install.bat`** — it creates `.venv`, installs pyvisa etc., then lists
   any instruments it can see.
4. Make the instruments visible to VISA — install **Keysight IO Libraries Suite**
   (free; provides the VISA runtime + USBTMC drivers for *both* the Keysight and
   Rigol). Re-run `install.bat` to confirm they list as `USB0::0x2A8D::…` /
   `USB0::0x1AB1::…`. *(Advanced alternative: bind each instrument to WinUSB with
   Zadig and use the pyvisa-py fallback — see the bundle README.)*
5. Run **`run.bat`** → browser opens at `http://127.0.0.1:8787/`.

**Running in a VM:** SPICE USB redirection can *enumerate* a USBTMC instrument
but not sustain its I/O session (VISA `VI_ERROR_ALLOC`). Use native USB
passthrough instead, and give the VM ≥ 8 GiB RAM.

---

## First run

1. Pick the test (e.g. *Protective Bonding Continuity*).
2. Fill the EUT/session fields; choose the **`bench-4wire`** connector (or
   `manual`).
3. Follow **[USER-GUIDE.md](USER-GUIDE.md)** for the capture workflow, pause/
   resume, re-measure, ambient import, and report generation.

Output is written under your `-out` folder, organised as
`<EUT>/<test>/<YYYY-MM-DD_HHMMSS>/` with the report, `results.xlsx`, and an
`evidence/` tree.
