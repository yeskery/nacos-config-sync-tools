//go:build !windows

package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	portFlag := flag.Int("port", 0, "local HTTP server port (0 for random port)")
	headlessFlag := flag.Bool("headless", false, "run without opening a desktop window")
	flag.Parse()

	app := NewApp()
	app.startup(context.Background())

	distFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load embedded frontend assets: %v\n", err)
		os.Exit(1)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *portFlag))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to listen on local port: %v\n", err)
		os.Exit(1)
	}
	defer listener.Close()

	url := fmt.Sprintf("http://%s", listener.Addr().String())
	server := &http.Server{
		Handler:           newWebHandler(app, distFS),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "server error: %v\n", serveErr)
		}
	}()

	fmt.Printf("Nacos Config Sync running at %s\n", url)

	if !*headlessFlag {
		go func() {
			time.Sleep(100 * time.Millisecond)
			if err := openDesktopWindow(url); err != nil {
				fmt.Fprintf(os.Stderr, "open browser window failed (%v), please visit %s manually\n", err, url)
			}
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func openDesktopWindow(url string) error {
	profileDir := filepath.Join(os.TempDir(), "nacos-config-sync-tools-profile")
	if runtime.GOOS == "darwin" {
		for _, candidate := range []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		} {
			if _, err := os.Stat(candidate); err == nil {
				cmd := exec.Command(candidate,
					"--app="+url,
					"--user-data-dir="+profileDir,
					"--no-first-run",
					"--no-default-browser-check",
					"--window-size=1280,980",
				)
				if err := cmd.Start(); err == nil {
					return nil
				}
			}
		}
		return exec.Command("open", url).Start()
	}

	for _, name := range []string{"google-chrome", "microsoft-edge", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			cmd := exec.Command(path,
				"--app="+url,
				"--user-data-dir="+profileDir,
				"--no-first-run",
				"--no-default-browser-check",
				"--window-size=1280,980",
			)
			if err := cmd.Start(); err == nil {
				return nil
			}
		}
	}
	return exec.Command("xdg-open", url).Start()
}
