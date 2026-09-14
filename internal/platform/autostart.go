package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Start-on-login support (pure Go, no admin rights needed):
//
//	Linux:   ~/.config/autostart/novex-printer-agent.desktop
//	macOS:   ~/Library/LaunchAgents/ir.menuvex.printeragent.plist
//	Windows: %APPDATA%\...\Startup\NovexPrinterAgent.vbs (hidden launcher)
//
// The v1 agent is headless by design (no tray UI): autostart simply
// relaunches the background binary on login.

// ExecutablePath returns the absolute path of the running binary.
func ExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}
	return exe, nil
}

// AutostartPath returns where the login item would live on this OS.
func AutostartPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "linux":
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(base, "autostart", "novex-printer-agent.desktop"), nil
	case "darwin":
		return filepath.Join(home, "Library", "LaunchAgents", "ir.menuvex.printeragent.plist"), nil
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "NovexPrinterAgent.vbs"), nil
	default:
		return "", fmt.Errorf("autostart is not supported on %s", runtime.GOOS)
	}
}

// InstallAutostart writes the login item for the current binary.
// It returns the path that was written.
func InstallAutostart() (string, error) {
	exe, err := ExecutablePath()
	if err != nil {
		return "", err
	}
	path, err := AutostartPath()
	if err != nil {
		return "", err
	}
	var content string
	switch runtime.GOOS {
	case "linux":
		content = "[Desktop Entry]\n" +
			"Type=Application\n" +
			"Name=Novex Printer Agent\n" +
			"Comment=Local bridge between web browsers and printers\n" +
			"Exec=" + exe + "\n" +
			"X-GNOME-Autostart-enabled=true\n" +
			"Terminal=false\n"
	case "darwin":
		content = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" +
			"<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n" +
			"<plist version=\"1.0\">\n<dict>\n" +
			"  <key>Label</key><string>ir.menuvex.printeragent</string>\n" +
			"  <key>ProgramArguments</key><array><string>" + xmlEscape(exe) + "</string></array>\n" +
			"  <key>RunAtLoad</key><true/>\n" +
			"  <key>KeepAlive</key><true/>\n" +
			"</dict>\n</plist>\n"
	case "windows":
		// Hidden launcher: runs the agent with no console window.
		content = "CreateObject(\"Wscript.Shell\").Run \"\"\"" + exe + "\"\"\", 0, False\n"
	default:
		return "", fmt.Errorf("autostart is not supported on %s", runtime.GOOS)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// UninstallAutostart removes the login item. Missing file = success.
func UninstallAutostart() error {
	path, err := AutostartPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// AutostartInstalled reports whether the login item exists.
func AutostartInstalled() (bool, string, error) {
	path, err := AutostartPath()
	if err != nil {
		return false, "", err
	}
	_, err = os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, path, nil
		}
		return false, path, err
	}
	return true, path, nil
}

func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}
