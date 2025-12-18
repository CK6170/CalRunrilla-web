// Command `calrunrilla-server` runs the CalRunrilla web UI + HTTP API locally.
//
// It serves static assets from `-web` (defaults to `./web`) and exposes JSON APIs
// + WebSocket streams used by the frontend to connect to the device, run
// calibration sampling, compute matrices, flash calibration, and run test mode.
//
// Flags:
//
//	-addr: TCP address to listen on (default 127.0.0.1:8080)
//	-web:  path to web root containing index.html
//	-open: open the UI URL in your default browser at startup
//
// Env:
//
//	CALRUNRILLA_NO_OPEN=1 disables browser auto-open even when -open is set.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/CK6170/Calrunrilla-go/internal/server"
)

func main() {
	var (
		addr = flag.String("addr", "127.0.0.1:8080", "http listen address")
		web  = flag.String("web", "./web", "path to web root (index.html)")
		open = flag.Bool("open", false, "open the web UI in your default browser on startup")
	)
	flag.Parse()

	// Resolve web directory to absolute path
	webDir, err := filepath.Abs(*web)
	if err != nil {
		log.Fatalf("Failed to resolve web directory: %v", err)
	}
	if st, err := os.Stat(webDir); err != nil || !st.IsDir() {
		log.Fatalf("Web directory does not exist: %s", webDir)
	}

	s := server.New(webDir)
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", *addr, err)
	}

	uiURL := makeUIURL(*addr)
	log.Printf("Serving on http://%s", *addr)
	log.Printf("UI:        %s", uiURL)

	// Open browser unless disabled by flag or env var.
	if *open && os.Getenv("CALRUNRILLA_NO_OPEN") == "" {
		if err := openBrowser(uiURL); err != nil {
			log.Printf("WARN: failed to open browser: %v", err)
		}
	}

	if err := http.Serve(ln, s.Handler()); err != nil {
		fmt.Println(err)
	}
}

// makeUIURL turns a listen address (host:port) into a browser-friendly URL.
//
// If the server is bound to 0.0.0.0 / ::, the returned URL uses 127.0.0.1
// because wildcard addresses are not reachable targets in browsers.
func makeUIURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// If the user passed something odd, keep existing behavior.
		return fmt.Sprintf("http://%s/", strings.TrimSpace(addr))
	}
	// 0.0.0.0/:: are not reachable in browsers—use localhost.
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%s/", host, port)
}

// openBrowser tries to open the given URL in the OS default browser.
//
// It is intentionally non-blocking (uses exec.Command(...).Start()) so the
// server startup path is not delayed by browser launch behavior.
func openBrowser(url string) error {
	// Non-blocking: Start() returns immediately.
	switch runtime.GOOS {
	case "windows":
		// `start` is a cmd.exe built-in. The empty title argument prevents quoting issues.
		return exec.Command("cmd", "/c", "start", "", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
