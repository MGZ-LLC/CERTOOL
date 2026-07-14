// Command certool is a platform-agnostic, local-web-UI recorder for test
// campaigns. It serves a bench UI that walks an operator record-by-record
// through a test app's run sheet, forces/reads instrument settings where a
// connector supports it (falling back to smart manual entry otherwise), and
// writes a documented output folder: an Obsidian report, an xlsx of the
// calculated tabular data, and timestamped records for photo correlation.
//
// Build a single binary for the bench machine, e.g. for Windows:
//
//	GOOS=windows GOARCH=amd64 go build -o certool.exe ./cmd/certool
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/MGZ-LLC/CERTOOL/internal/engine"
	"github.com/MGZ-LLC/CERTOOL/internal/web"

	// Register the built-in test apps and connectors via their init().
	_ "github.com/MGZ-LLC/CERTOOL/internal/connector/bench"
	_ "github.com/MGZ-LLC/CERTOOL/internal/connector/keysight"
	_ "github.com/MGZ-LLC/CERTOOL/internal/connector/korad"
	_ "github.com/MGZ-LLC/CERTOOL/internal/connector/rigol"
	_ "github.com/MGZ-LLC/CERTOOL/internal/testapp/earthcontinuity"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "listen address (local only)")
	out := flag.String("out", "campaigns", "output root directory for sessions")
	noOpen := flag.Bool("no-open", false, "do not open a browser")
	flag.Parse()

	root, err := filepath.Abs(*out)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		log.Fatal(err)
	}

	eng := engine.New(root)
	srv, err := web.New(eng)
	if err != nil {
		log.Fatal(err)
	}

	httpSrv := &http.Server{
		Addr:         *addr,
		Handler:      srv.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	url := "http://" + *addr + "/"
	fmt.Printf("certool — output root: %s\n", root)
	fmt.Printf("open %s\n", url)
	if !*noOpen {
		go openBrowser(url)
	}
	log.Fatal(httpSrv.ListenAndServe())
}

// openBrowser best-effort launches the default browser cross-platform.
func openBrowser(url string) {
	time.Sleep(400 * time.Millisecond)
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd, args = "open", []string{url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}
