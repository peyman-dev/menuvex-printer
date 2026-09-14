// Novex Printer Agent: a lightweight local bridge between web browsers
// and printers (LAN/TCP + USB). Single static binary, no Java, no database.
//
// Typical run:
//
//	novex-printer-agent                      # foreground, default config
//	novex-printer-agent --print-token        # show API token and exit
//	novex-printer-agent --install-autostart  # start on login, then exit
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/menuvex/novex-printer-agent/internal/api"
	"github.com/menuvex/novex-printer-agent/internal/config"
	"github.com/menuvex/novex-printer-agent/internal/platform"
	"github.com/menuvex/novex-printer-agent/internal/printer"
	"github.com/menuvex/novex-printer-agent/internal/usb"
)

func main() {
	os.Exit(run())
}

func run() int {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmsgprefix)
	log.SetPrefix("novex-agent: ")

	fs := flag.NewFlagSet("novex-printer-agent", flag.ContinueOnError)
	configPath := fs.String("config", "", "config file path (default: OS user config dir)")
	host := fs.String("host", "", "bind address (default from config, 127.0.0.1)")
	port := fs.Int("port", 0, "bind port (default from config, 8765)")
	showVersion := fs.Bool("version", false, "print version and exit")
	printToken := fs.Bool("print-token", false, "print the API token and exit")
	installAuto := fs.Bool("install-autostart", false, "install start-on-login and exit")
	uninstallAuto := fs.Bool("uninstall-autostart", false, "remove start-on-login and exit")
	autostartStatus := fs.Bool("autostart-status", false, "show start-on-login status and exit")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	if *showVersion {
		fmt.Printf("Novex Printer Agent v%s\n", platform.Version)
		return 0
	}

	if *installAuto {
		path, err := platform.InstallAutostart()
		if err != nil {
			log.Printf("ERROR: %v", err)
			return 1
		}
		fmt.Printf("Start-on-login installed: %s\n", path)
		return 0
	}
	if *uninstallAuto {
		if err := platform.UninstallAutostart(); err != nil {
			log.Printf("ERROR: %v", err)
			return 1
		}
		fmt.Println("Start-on-login removed.")
		return 0
	}
	if *autostartStatus {
		installed, path, err := platform.AutostartInstalled()
		if err != nil {
			log.Printf("ERROR: %v", err)
			return 1
		}
		fmt.Printf("Start-on-login: %v (%s)\n", installed, path)
		return 0
	}

	cfgPath := *configPath
	if cfgPath == "" {
		var err error
		cfgPath, err = config.DefaultPath()
		if err != nil {
			log.Printf("ERROR: cannot determine config path: %v", err)
			return 1
		}
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Printf("ERROR: cannot load config: %v", err)
		return 1
	}

	// CLI overrides (not persisted).
	if *host != "" {
		cfg.Host = *host
	}
	if *port != 0 {
		cfg.Port = *port
	}
	if err := cfg.Validate(); err != nil {
		log.Printf("ERROR: invalid configuration: %v", err)
		return 1
	}

	if *printToken {
		fmt.Println(cfg.Token)
		return 0
	}

	usbBackend, usbSupported, usbDetail := usb.New()
	if !usbSupported {
		usbBackend = nil
	}

	reg := printer.NewRegistry(cfg, cfgPath, usbBackend)
	srv := api.NewServer(reg, cfg, platform.Version)

	bindAddr := cfg.Addr()
	if ip := net.ParseIP(cfg.Host); ip == nil || !ip.IsLoopback() {
		log.Printf("WARNING: binding to non-loopback address %s — the agent must stay on localhost. Anyone who can reach it with the token can print.", bindAddr)
	}

	log.Printf("Novex Printer Agent v%s starting on http://%s", platform.Version, bindAddr)
	log.Printf("Config: %s", cfgPath)
	log.Printf("API token: %s", cfg.Token)
	log.Printf("Trusted origins: %v", cfg.TrustedOrigins)
	if usbSupported {
		log.Printf("USB: supported (%s)", usbDetail)
	} else {
		log.Printf("USB: UNSUPPORTED (%s)", usbDetail)
	}
	if devs, err := listStartupDevices(reg); err == nil {
		log.Printf("Printers at startup: %d", devs)
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil && err.Error() != "http: Server closed" {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Printf("Received %s, shutting down...", sig)
	case err := <-errCh:
		log.Printf("ERROR: server failed: %v", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reg.CloseAll()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("ERROR: shutdown: %v", err)
		return 1
	}
	log.Printf("Stopped.")
	return 0
}

// listStartupDevices counts visible printers for the startup log.
// Failures are non-fatal (USB discovery may legitimately error).
func listStartupDevices(reg *printer.Registry) (int, error) {
	printers, err := reg.ListPrinters()
	if err != nil {
		return 0, err
	}
	return len(printers), nil
}
