package scpi

import (
	"bufio"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

//go:embed instr_helper.py
var helperPy []byte

var (
	helperOnce sync.Once
	helperPath string
	helperErr  error
)

// helperScript writes the embedded pyvisa helper to a stable temp path once and
// returns it, so the bridge can invoke it with the venv's python.
func helperScript() (string, error) {
	helperOnce.Do(func() {
		p := filepath.Join(os.TempDir(), "certool-instr_helper.py")
		helperErr = os.WriteFile(p, helperPy, 0o644)
		helperPath = p
	})
	return helperPath, helperErr
}

// pythonPath resolves the interpreter: $CERTOOL_PYTHON, else a .venv next to the
// executable (the installer creates it), else python/python3 on PATH.
func pythonPath() string {
	if p := os.Getenv("CERTOOL_PYTHON"); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		cands := []string{
			filepath.Join(dir, ".venv", "Scripts", "python.exe"),
			filepath.Join(dir, ".venv", "bin", "python"),
		}
		for _, c := range cands {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}

// bridge is a Transport that proxies SCPI to the pyvisa helper subprocess.
type bridge struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
	mu  sync.Mutex
}

func openBridge(resource string) (Transport, error) {
	script, err := helperScript()
	if err != nil {
		return nil, fmt.Errorf("scpi: write helper: %w", err)
	}
	cmd := exec.Command(pythonPath(), script)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("scpi: start python helper (%s): %w", pythonPath(), err)
	}
	b := &bridge{cmd: cmd, in: stdin, out: bufio.NewReader(stdout)}

	resp, err := b.round("OPEN " + resource)
	if err != nil {
		b.Close()
		return nil, err
	}
	if !strings.HasPrefix(resp, "OK") {
		b.Close()
		return nil, fmt.Errorf("scpi: bridge open %q: %s", resource, resp)
	}
	return b, nil
}

// round sends one command and reads exactly one reply line.
func (b *bridge) round(cmd string) (string, error) {
	if _, err := io.WriteString(b.in, strings.TrimRight(cmd, "\n")+"\n"); err != nil {
		return "", err
	}
	line, err := b.out.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("scpi: helper closed (no reply to %q)", cmd)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (b *bridge) Write(cmd string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	resp, err := b.round("W " + cmd)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(resp, "OK") {
		return fmt.Errorf("scpi: write %q: %s", cmd, resp)
	}
	return nil
}

func (b *bridge) Query(cmd string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	resp, err := b.round("Q " + cmd)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(resp, "R ") {
		return strings.TrimPrefix(resp, "R "), nil
	}
	return "", fmt.Errorf("scpi: query %q: %s", cmd, resp)
}

func (b *bridge) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, _ = b.round("CLOSE")
	_ = b.in.Close()
	if b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
	_ = b.cmd.Wait()
	return nil
}
