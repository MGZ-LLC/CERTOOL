#!/usr/bin/env python3
"""certool instrument helper — a thin pyvisa bridge driven by the Go core.

This is the "separate Python connection layer" for platforms where Go cannot talk
USBTMC directly (notably Windows, which has no /dev/usbtmc). The Go binary spawns
this script and speaks a line protocol over stdin/stdout (UTF-8, one command and
one reply per line):

  LIST                 -> "DEV <resource>\\t<idn>" per instrument, then "OK"
  OPEN <resource>      -> "OK <idn>" | "ERR <msg>"
                          <resource> may be "@<substr>" to auto-pick the resource
                          whose *IDN? contains <substr> (case-insensitive)
  W <scpi>             -> "OK" | "ERR <msg>"       (write, no response expected)
  Q <scpi>             -> "R <response>" | "ERR <msg>"  (query)
  CLOSE                -> "OK"

It uses whatever VISA backend pyvisa finds: the system VISA runtime (Keysight IO
Libraries / NI-VISA) if installed, else the pure-python pyvisa-py backend (@py)
with a libusb backend. One helper process fronts one instrument.
"""
import sys


def out(s):
    sys.stdout.write(s + "\n")
    sys.stdout.flush()


def dbg(msg):
    sys.stderr.write("[helper] " + msg + "\n")
    sys.stderr.flush()


def make_rm():
    """Return a ResourceManager, preferring a real VISA runtime, else pyvisa-py."""
    import pyvisa
    try:
        rm = pyvisa.ResourceManager()  # default backend (system VISA if present)
        dbg("VISA backend: %r" % (rm.visalib,))
        return rm
    except Exception as e:
        dbg("default VISA unavailable (%s) -> falling back to pyvisa-py (@py)" % e)
        rm = pyvisa.ResourceManager("@py")
        dbg("backend: @py (pyvisa-py)")
        return rm


def open_match(rm, spec):
    """Open the instrument named by spec and return (inst, idn).

    spec is either a full VISA resource string, or '@substr' matching the *IDN?
    (case-insensitive) or the resource string. The matched device is opened ONCE
    and returned (no close/reopen), which avoids a VISA 'resource busy' race.
    Raises with a diagnostic listing everything it saw.
    """
    if not spec.startswith("@"):
        inst = rm.open_resource(spec)
        inst.timeout = 5000
        return inst, inst.query("*IDN?").strip()

    sub = spec[1:].lower()
    resources = list(rm.list_resources())
    dbg("looking for %r among resources: %s" % (spec, list(resources)))
    last_err = None
    for r in resources:
        # A resource-string match (VID/model/serial) avoids opening the device.
        if sub in r.lower():
            dbg("  %s -> resource-string match" % r)
            inst = rm.open_resource(r)
            inst.timeout = 5000
            return inst, inst.query("*IDN?").strip()
        try:
            inst = rm.open_resource(r)
            inst.timeout = 5000
            idn = inst.query("*IDN?").strip()
        except Exception as e:
            dbg("  %s -> open/IDN failed: %s" % (r, e))
            last_err = e
            continue
        dbg("  %s -> IDN %r" % (r, idn))
        if sub in idn.lower():
            return inst, idn
        inst.close()
    raise RuntimeError("no resource matching %s among %s (last error: %s)"
                       % (spec, list(resources), last_err))


def main():
    try:
        rm = make_rm()
    except Exception as e:
        # Report the failure on every command so the Go side surfaces it cleanly.
        for line in sys.stdin:
            if line.strip():
                out("ERR pyvisa/VISA unavailable: %s" % e)
        return

    inst = None
    for line in sys.stdin:
        line = line.rstrip("\r\n")
        if not line:
            continue
        cmd, _, arg = line.partition(" ")
        cmd = cmd.upper()
        try:
            if cmd == "LIST":
                for r in rm.list_resources():
                    idn = ""
                    try:
                        d = rm.open_resource(r)
                        idn = d.query("*IDN?").strip()
                        d.close()
                    except Exception as e:
                        idn = "(%s)" % e
                    out("DEV %s\t%s" % (r, idn))
                out("OK")
            elif cmd == "OPEN":
                try:
                    inst, idn = open_match(rm, arg.strip())
                except Exception as e:
                    dbg("OPEN %s failed: %s" % (arg.strip(), e))
                    out("ERR %s" % e)
                    continue
                dbg("OPEN %s -> connected, IDN %r" % (arg.strip(), idn))
                out("OK " + idn)
            elif cmd == "W":
                if inst is None:
                    out("ERR not open")
                    continue
                inst.write(arg)
                out("OK")
            elif cmd == "Q":
                if inst is None:
                    out("ERR not open")
                    continue
                out("R " + inst.query(arg).strip())
            elif cmd == "CLOSE":
                if inst is not None:
                    inst.close()
                    inst = None
                out("OK")
            else:
                out("ERR unknown command %s" % cmd)
        except Exception as e:
            out("ERR %s" % e)


if __name__ == "__main__":
    main()
