certool — Windows bench setup
===================================

What this is
------------
The bench test recorder. On Windows the live instrument link (Keysight E36312A
current source + Rigol DM858E DMM) goes through a small Python "bridge" using
pyvisa, because Windows has no built-in USBTMC support. install.bat sets that up
in a self-contained .venv next to the tool — nothing is installed system-wide by
the tool itself.

One-time setup
--------------
1. Install Python 3 (https://www.python.org/downloads/) — tick "Add Python to PATH".
2. Double-click  install.bat  — it creates .venv and installs pyvisa etc., then
   lists any instruments it can see.

Making the instruments visible (USBTMC on Windows)
--------------------------------------------------
Pick ONE:

  A) RECOMMENDED — install "Keysight IO Libraries Suite" (free from keysight.com).
     It provides the VISA runtime and USBTMC drivers for BOTH instruments
     (they're standard USBTMC, so the Rigol works through it too). Then re-run
     install.bat — the instruments should list as USB0::0x2A8D::... / ::0x1AB1::...
     No per-device fiddling.

  B) ADVANCED — no VISA runtime, use pyvisa-py + libusb:
     Use Zadig (https://zadig.akeo.ie) to bind each instrument to the "WinUSB"
     (or libusbK) driver, once per instrument. Then re-run install.bat.
     Note: while bound to WinUSB, vendor software can't use that instrument.

Running
-------
Run  run.bat . A browser opens at http://127.0.0.1:8787/.
Choose the "bench-4wire" connector — it drives the Keysight (current) and Rigol
(voltage) together. Sessions are written under a "campaigns" folder next to the tool.

Ambient logger (AZ 88163)
-------------------------
The AZ 88163 is a standalone logger, not a live instrument. Start it before the
run; at the end, export its tab-delimited CSV and drop it in the session's
evidence/instrument-logs/ folder (the finish page lists the steps).

Troubleshooting
---------------
- "No .venv" when running run.bat  -> run install.bat first.
- Instruments not listed -> do section A or B above, then re-run install.bat.
- To see what VISA sees:  .venv\Scripts\python -c "import pyvisa; print(pyvisa.ResourceManager().list_resources())"
