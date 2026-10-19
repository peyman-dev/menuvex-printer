// Command coffish-desktop is the standalone native desktop executable for the
// MenuVex (Coffish) Offline Café & Restaurant Management Desktop Application.
//
// Architecture: the process is a single Go binary that (1) serves the embedded
// HTMX UI and local API on a loopback port and (2) opens a native window that
// points at that port (Cocoa/WKWebView on macOS, GTK/WebKit2GTK on Linux, an
// Edge/Chrome app window on Windows). The HTTP assets are embedded with
// //go:embed, so the binary has no dependency on the source tree or the
// current working directory once installed.
//
// Data (database, license, uploads) always lives outside the install directory
// in the per-user configuration folder, so installers can replace/upgrade or
// uninstall the program without touching the operator's data.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/sinaqaffari/coffish/desktop"
)

// version is injected at build time:
//
//	-ldflags "-X main.version=1.2.3"
var version = "dev"

func init() {
	// Native OS GUI frameworks (macOS Cocoa NSApplication, etc.) require running on the main OS thread.
	runtime.LockOSThread()
}

func main() {
	var (
		addr     = flag.String("addr", envOr("COFFISH_DESKTOP_ADDR", "127.0.0.1:8787"), "Loopback listen address")
		dataDir  = flag.String("data-dir", os.Getenv("COFFISH_DATA_DIR"), "Local database directory (default: per-user config dir)")
		noWindow = flag.Bool("no-window", os.Getenv("COFFISH_NO_WINDOW") == "1", "Run headless without opening a desktop window")
		showVer  = flag.Bool("version", false, "Print version and exit")
	)
	flag.Parse()
	initLogging()
	if *showVer {
		fmt.Printf("MenuVex Offline %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return
	}

	app, err := desktop.New(*dataDir)
	if err != nil {
		fatal("خطا در راه‌اندازی پایگاه داده محلی منووکس: " + err.Error())
	}
	defer app.Close()

	_ = app.SeedIfEmpty(context.Background())

	// Reuse a backend that is already running (double click on the desktop
	// shortcut) instead of starting a second database writer on another port.
	if running, ok := runningInstance(*addr); ok {
		log.Printf("منووکس از قبل در حال اجراست: %s", running)
		if !*noWindow {
			if !openDesktopWindow(running) {
				log.Printf("پنجره باز نشد؛ آدرس را در مرورگر باز کنید: %s", running)
				waitForSignal()
			}
		}
		return
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		// The configured port belongs to another program: pick a free one and
		// tell the operator about it instead of failing to start.
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fatal("خطا در باز کردن پورت داخلی دسکتاپ: " + err.Error())
		}
	}
	url := "http://" + ln.Addr().String()
	log.Printf("سامانه جامع دسکتاپ منووکس %s فعال شد: %s", version, url)

	srv := &http.Server{
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      125 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	serveErr := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			serveErr <- err
		}
	}()

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}

	if *noWindow {
		select {
		case <-stop:
			shutdown()
		case err := <-serveErr:
			log.Printf("خطا در سرویس داخلی: %v", err)
			shutdown()
		}
		return
	}

	windowDone := make(chan bool, 1)
	go func() {
		openedOK := openDesktopWindow(url)
		windowDone <- openedOK
	}()

	select {
	case <-stop:
		shutdown()
	case err := <-serveErr:
		log.Printf("خطا در سرویس داخلی: %v", err)
		shutdown()
	case opened := <-windowDone:
		if !opened {
			// No native host was available (e.g. PyGObject/WebKit missing).
			// Keep serving so the operator can open the printed URL manually.
			log.Printf("پنجرهٔ دسکتاپ در دسترس نبود؛ آدرس زیر را در مرورگر باز کنید: %s", url)
			select {
			case <-stop:
			case <-serveErr:
			}
		}
		shutdown()
	}
}

// runningInstance reports whether another MenuVex backend already answers on
// addr. Only our own process may be reused: the HTTP response is validated with
// the marker header set by the application handler.
func runningInstance(addr string) (string, bool) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "0" {
		return "", false
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	base := "http://" + net.JoinHostPort(host, port)
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(base + "/?page=home")
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK && strings.EqualFold(resp.Header.Get("X-MenuVex-App"), "offline") {
		return base, true
	}
	return "", false
}

// initLogging mirrors the log to the user cache directory when the process was
// started without a terminal (Explorer, Finder, .desktop launcher), so GUI
// startup problems remain diagnosable instead of disappearing silently.
func initLogging() {
	if info, err := os.Stderr.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		return
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return
	}
	dir := filepath.Join(cache, "MenuVexOffline")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "desktop.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.Printf("MenuVex Offline %s starting (%s/%s)", version, runtime.GOOS, runtime.GOARCH)
}

func waitForSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
