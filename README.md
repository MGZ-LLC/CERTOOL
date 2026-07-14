# CERTOOL

A platform-agnostic, local-web-UI **test-campaign recorder** for the bench. It
walks an operator record-by-record through a test's run sheet, forces and reads
instrument settings where a device connector supports it (falling back to *smart
manual entry* otherwise), and writes a self-describing, organised output folder:
an Obsidian report, an `xlsx` of the calculated tabular data, and timestamped
records so bench photos correlate by capture time.

Built by **MGZ Consulting** for CE-marking test campaigns and released as an
open-source resource. The tool itself contains **no client-confidential data** —
test methods derive from public standards; product data lives only in generated
session output.

## Highlights

- **One local web UI**, one self-contained binary per platform. Cross-platform
  (Linux, Windows).
- **Live instrument control** via a `bench-4wire` connector — Keysight E36312A
  sources the current, Rigol DM858E reads the drop — or **smart manual entry**
  when no instrument is attached.
- **Colour status light** (orange ready · green OK · red off-limit · yellow
  unstable) and a keystroke-driven capture loop (`Enter` commit, `r` read).
- **Append-only records** with re-measure (superseded attempts retained),
  **suspend/resume any session**, and **incremental points** with auto-fill
  (progressive numbering + repeated presets).
- **Instrument serials auto-captured** from `*IDN?`; **AZ 88163 ambient logger**
  export merged by timestamp.
- **Organised output**: `campaigns/<EUT>/<test>/<date>/` with report, xlsx, and
  an `evidence/` tree.

## Install

See **[docs/INSTALL.md](docs/INSTALL.md)** (and the PDF manual). In short:

- **Linux:** download the `certool-linux` package, `./install.sh` (binary +
  udev rule for `/dev/usbtmc`). No Python/VISA needed.
- **Windows:** download the `certool-windows` bundle, run `install.bat` (sets up
  a local Python venv for the pyvisa bridge), install a VISA runtime (Keysight IO
  Libraries), then `run.bat`.

Then open `http://127.0.0.1:8787/`.

## Build from source

Requires Go 1.24+.

```sh
go run ./cmd/certool                 # serve http://127.0.0.1:8787
GOOS=windows go build ./cmd/certool  # cross-compile for Windows
bash packaging/linux/build.sh        # Linux package
bash packaging/windows/build.sh      # Windows bundle
```

## Architecture

Three layers, so new tests and instruments plug in without touching the core:

```
 framework (internal/{engine,model,output,web,ambient})
    │  session · run sheet · timestamped records · xlsx + Obsidian md · web UI
    ├── test apps      (internal/testapp/*)     ← the "why" + acceptance logic
    │      earth-continuity (EN 60204-1 Cl.18)  ← reference implementation
    └── device connectors (internal/connector/*)
           manual   ← default smart-fallback (expected values prefilled)
           bench-4wire · keysight · rigol · korad · scpi (USBTMC + LAN + pyvisa bridge)
```

## Documentation

- **[docs/USER-GUIDE.md](docs/USER-GUIDE.md)** — the bench workflow, end to end.
- **[docs/INSTALL.md](docs/INSTALL.md)** — install for Linux and Windows.

## Licence

MIT — see [LICENSE](LICENSE).
