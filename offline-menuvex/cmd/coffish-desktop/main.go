// Command coffish-desktop is the standalone native desktop executable for the
// Coffish Offline Café & Restaurant Management Desktop Application.
// It runs a native OS desktop window (Apple Cocoa/WKWebView on macOS, GTK/WebKit2GTK
// on Linux, WinForms on Windows) and NEVER launches Chrome or Chromium.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/sinaqaffari/coffish/desktop"
)

func init() {
	// Native OS GUI frameworks (macOS Cocoa NSApplication, etc.) require running on the main OS thread.
	runtime.LockOSThread()
}

func main() {
	var (
		addr     = flag.String("addr", envOr("COFFISH_DESKTOP_ADDR", "127.0.0.1:8787"), "Loopback listen address")
		dataDir  = flag.String("data-dir", os.Getenv("COFFISH_DATA_DIR"), "Local database directory")
		noWindow = flag.Bool("no-window", os.Getenv("COFFISH_NO_WINDOW") == "1", "Run headless without opening native desktop window")
	)
	flag.Parse()

	app, err := desktop.New(*dataDir)
	if err != nil {
		log.Fatalf("خطا در راه‌اندازی پایگاه داده محلی منووکس: %v", err)
	}
	defer app.Close()

	_ = app.SeedIfEmpty(context.Background())

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			log.Fatalf("خطا در باز کردن پورت داخلی دسکتاپ: %v", err)
		}
	}

	url := "http://" + ln.Addr().String()
	fmt.Printf("سامانه جامع دسکتاپ منووکس فعال شد (پنجره بومی سیستم‌عامل): %s\n", url)

	srv := &http.Server{
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      125 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	if *noWindow {
		go func() {
			<-stop
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(ctx)
		}()
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
		return
	}

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("خطا در سرویس داخلی: %v", err)
		}
	}()

	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		os.Exit(0)
	}()

	// Run the native OS desktop window on the main thread (blocks until window is closed).
	runNativeDesktopWindow(url)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
