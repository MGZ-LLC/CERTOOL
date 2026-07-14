// Package korad implements a device connector for the Korad/RND KWR103
// programmable DC power supply, used in the earth-continuity test as the
// floating constant-current source (EN 60204-1 Cl. 18.2.2).
//
// The KWR103 speaks the KORAD ASCII protocol over a USB virtual COM port. It
// lets the framework FORCE settings (constant-current value, voltage limit,
// output on/off) and READ BACK the actual output current and voltage — which are
// exactly the I and V the four-wire bonding test records (R = V / I).
//
// Protocol notes: commands are short ASCII strings with no terminator; the PSU
// replies to queries ("VOUT?", "IOUT?") with an ASCII float and usually no
// newline, so reads are drained until a short idle timeout.
package korad

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MGZ-LLC/CERTOOL/internal/connector"
	"go.bug.st/serial"
)

const defaultPort = "" // resolved at Connect() if empty (first likely port)

type korad struct {
	addr string
	port serial.Port
}

func init() {
	connector.Register("korad", func(addr string) connector.Connector {
		return &korad{addr: addr}
	})
}

func (k *korad) ID() string   { return "korad" }
func (k *korad) Name() string { return "Korad/RND KWR103 (floating CC source)" }

func (k *korad) Capability() connector.Capability {
	return connector.Capability{
		CanApply:     true,
		CanRead:      true,
		ReadChannels: []string{"I", "V"},
		ApplyKeys:    []string{"current", "vlimit", "output"},
	}
}

func (k *korad) Available() bool { return k.port != nil }

func (k *korad) Connect() error {
	addr := k.addr
	if addr == "" {
		p, err := firstSerialPort()
		if err != nil {
			return err
		}
		addr = p
	}
	mode := &serial.Mode{BaudRate: 115200} // KWR103 default; some units use 9600
	port, err := serial.Open(addr, mode)
	if err != nil {
		return fmt.Errorf("korad: open %s: %w", addr, err)
	}
	port.SetReadTimeout(200 * time.Millisecond)
	k.port = port
	k.addr = addr
	return nil
}

func (k *korad) Close() error {
	if k.port == nil {
		return nil
	}
	// Safety: turn the output off on close.
	_ = k.write("OUT0")
	err := k.port.Close()
	k.port = nil
	return err
}

// Apply forces settings. Keys: current (A), vlimit (V), output ("on"/"off").
func (k *korad) Apply(settings map[string]string) error {
	if k.port == nil {
		return fmt.Errorf("korad: not connected")
	}
	if v, ok := settings["current"]; ok {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("korad: bad current %q: %w", v, err)
		}
		if err := k.write(fmt.Sprintf("ISET:%.3f", f)); err != nil {
			return err
		}
	}
	if v, ok := settings["vlimit"]; ok {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("korad: bad vlimit %q: %w", v, err)
		}
		if err := k.write(fmt.Sprintf("VSET:%.2f", f)); err != nil {
			return err
		}
	}
	if v, ok := settings["output"]; ok {
		cmd := "OUT0"
		if v == "on" || v == "1" || v == "true" {
			cmd = "OUT1"
		}
		if err := k.write(cmd); err != nil {
			return err
		}
	}
	return nil
}

// Read returns the requested channels ("I", "V") from the live output.
func (k *korad) Read(channels []string) ([]connector.Reading, error) {
	if k.port == nil {
		return nil, fmt.Errorf("korad: not connected")
	}
	if len(channels) == 0 {
		channels = []string{"I", "V"}
	}
	out := make([]connector.Reading, 0, len(channels))
	for _, ch := range channels {
		var query, unit string
		switch strings.ToUpper(ch) {
		case "I":
			query, unit = "IOUT?", "A"
		case "V":
			query, unit = "VOUT?", "V"
		default:
			return nil, fmt.Errorf("korad: unknown channel %q", ch)
		}
		f, err := k.query(query)
		if err != nil {
			return nil, err
		}
		out = append(out, connector.Reading{Channel: strings.ToUpper(ch), Value: f, Unit: unit})
	}
	return out, nil
}

func (k *korad) write(cmd string) error {
	_, err := k.port.Write([]byte(cmd))
	// KORAD units need a short gap between commands.
	time.Sleep(50 * time.Millisecond)
	return err
}

func (k *korad) query(cmd string) (float64, error) {
	if err := k.write(cmd); err != nil {
		return 0, err
	}
	buf := make([]byte, 0, 32)
	tmp := make([]byte, 32)
	deadline := time.Now().Add(600 * time.Millisecond)
	for time.Now().Before(deadline) {
		n, err := k.port.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
		if n == 0 && len(buf) > 0 {
			break // idle after receiving something
		}
	}
	s := strings.TrimSpace(string(buf))
	if s == "" {
		return 0, fmt.Errorf("korad: no reply to %q", cmd)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("korad: bad reply %q to %q: %w", s, cmd, err)
	}
	return f, nil
}

// firstSerialPort returns a plausible serial port when none was specified.
func firstSerialPort() (string, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return "", err
	}
	if len(ports) == 0 {
		return "", fmt.Errorf("korad: no serial ports found (specify one with -addr)")
	}
	return ports[0], nil
}
