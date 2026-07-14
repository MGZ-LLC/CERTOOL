// Package scpi provides a small transport-agnostic SCPI client used by the
// instrument connectors (Keysight, Rigol, …). It deliberately keeps the wire
// transport separate from instrument logic so the same connector works over:
//
//   - USBTMC  — Linux /dev/usbtmcN character device (kernel usbtmc driver)
//   - LAN     — raw SCPI socket, TCP port 5025 (LXI); pure Go, cross-platform
//
// A Windows build with USB-only instruments can add a VISA/Python-bridge
// transport implementing the same Transport interface without touching any
// connector — this is where the optional Python helper layer plugs in.
package scpi

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Transport is the minimal SCPI wire contract.
type Transport interface {
	Write(cmd string) error
	Query(cmd string) (string, error)
	Close() error
}

// Open resolves an address to a transport. Accepted forms:
//
//	/dev/usbtmc1            explicit USBTMC node
//	usbtmc:/dev/usbtmc1     explicit USBTMC node
//	usbtmc:2a8d:1102        auto-find the USBTMC node whose *IDN? / device matches VID:PID
//	usbtmc:@Keysight        auto-find the USBTMC node whose *IDN? contains "Keysight"
//	lan:192.168.0.5         LAN socket on default port 5025
//	192.168.0.5:5025        LAN socket, explicit port
func Open(addr string) (Transport, error) {
	addr = strings.TrimSpace(addr)
	switch {
	case strings.HasPrefix(addr, "lan:"):
		return openLAN(strings.TrimPrefix(addr, "lan:"))
	case strings.HasPrefix(addr, "usbtmc:"):
		return openUSBTMC(strings.TrimPrefix(addr, "usbtmc:"))
	case strings.HasPrefix(addr, "visa:"):
		return openBridge(strings.TrimPrefix(addr, "visa:"))
	case strings.HasPrefix(addr, "/dev/"):
		return openUSBTMCNode(addr)
	case isHostPort(addr):
		return openLAN(addr)
	default:
		return nil, fmt.Errorf("scpi: unrecognised address %q", addr)
	}
}

// DefaultUSB returns the platform-appropriate address to reach a USB instrument
// whose *IDN? contains match. Linux talks USBTMC directly via /dev/usbtmc*;
// other platforms (notably Windows) go through the pyvisa bridge.
func DefaultUSB(match string) string {
	if runtime.GOOS == "linux" {
		return "usbtmc:@" + match
	}
	return "visa:@" + match
}

// ---- USBTMC (Linux character device) ----

type usbtmc struct {
	f    *os.File
	path string
}

// openUSBTMC handles an explicit node path, a VID:PID, or an @IDN-substring by
// scanning /dev/usbtmc* and matching.
func openUSBTMC(spec string) (Transport, error) {
	if strings.HasPrefix(spec, "/dev/") {
		return openUSBTMCNode(spec)
	}
	nodes, _ := filepath.Glob("/dev/usbtmc[0-9]*")
	if len(nodes) == 0 {
		return nil, fmt.Errorf("scpi: no /dev/usbtmc* nodes present")
	}
	match := strings.TrimPrefix(spec, "@")
	byIDN := strings.HasPrefix(spec, "@")
	for _, n := range nodes {
		t, err := openUSBTMCNode(n)
		if err != nil {
			continue // likely a permission error or a busy node
		}
		idn, err := t.Query("*IDN?")
		if err != nil {
			t.Close()
			continue
		}
		if byIDN {
			if strings.Contains(strings.ToLower(idn), strings.ToLower(match)) {
				return t, nil
			}
		} else if strings.Contains(idn, match) { // VID:PID or model substring
			return t, nil
		}
		t.Close()
	}
	return nil, fmt.Errorf("scpi: no USBTMC node matched %q", spec)
}

func openUSBTMCNode(path string) (Transport, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("scpi: open %s: %w", path, err)
	}
	return &usbtmc{f: f, path: path}, nil
}

func (u *usbtmc) Write(cmd string) error {
	_, err := u.f.WriteString(strings.TrimRight(cmd, "\n") + "\n")
	return err
}

func (u *usbtmc) Query(cmd string) (string, error) {
	if err := u.Write(cmd); err != nil {
		return "", err
	}
	// The usbtmc driver returns one response message per read.
	buf := make([]byte, 4096)
	n, err := u.f.Read(buf)
	if err != nil {
		return "", fmt.Errorf("scpi: read after %q: %w", cmd, err)
	}
	return strings.TrimSpace(string(buf[:n])), nil
}

func (u *usbtmc) Close() error { return u.f.Close() }

// ---- LAN (raw SCPI socket, port 5025) ----

type lan struct {
	conn net.Conn
	r    *bufio.Reader
}

func openLAN(hostport string) (Transport, error) {
	if !strings.Contains(hostport, ":") {
		hostport += ":5025"
	}
	conn, err := net.DialTimeout("tcp", hostport, 4*time.Second)
	if err != nil {
		return nil, fmt.Errorf("scpi: dial %s: %w", hostport, err)
	}
	return &lan{conn: conn, r: bufio.NewReader(conn)}, nil
}

func (l *lan) Write(cmd string) error {
	_ = l.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err := l.conn.Write([]byte(strings.TrimRight(cmd, "\n") + "\n"))
	return err
}

func (l *lan) Query(cmd string) (string, error) {
	if err := l.Write(cmd); err != nil {
		return "", err
	}
	_ = l.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := l.r.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("scpi: read after %q: %w", cmd, err)
	}
	return strings.TrimSpace(line), nil
}

func (l *lan) Close() error { return l.conn.Close() }

// ---- helpers ----

func isHostPort(s string) bool {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return false
	}
	if _, err := strconv.Atoi(port); err != nil {
		return false
	}
	return host != ""
}

// ParseFloat is a lenient parser for SCPI numeric replies (handles trailing
// units or channel echoes some instruments append).
func ParseFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " ,;"); i > 0 {
		s = s[:i]
	}
	return strconv.ParseFloat(s, 64)
}
