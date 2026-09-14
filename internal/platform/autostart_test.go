package platform

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestAutostartPath(t *testing.T) {
	path, err := AutostartPath()
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	t.Logf("autostart path: %s", path)
	switch runtime.GOOS {
	case "linux":
		if !strings.HasSuffix(path, "novex-printer-agent.desktop") {
			t.Fatalf("unexpected linux path %s", path)
		}
	case "darwin":
		if !strings.HasSuffix(path, "ir.menuvex.printeragent.plist") {
			t.Fatalf("unexpected darwin path %s", path)
		}
	case "windows":
		if !strings.HasSuffix(path, "NovexPrinterAgent.vbs") {
			t.Fatalf("unexpected windows path %s", path)
		}
	}
}

func TestInstallUninstallAutostart(t *testing.T) {
	if os.Getenv("NOVEX_TEST_AUTOSTART") == "" && testing.Short() {
		t.Skip("skipping autostart roundtrip in short mode")
	}
	t.Cleanup(func() { _ = UninstallAutostart() })
	path, err := InstallAutostart()
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	installed, gotPath, err := AutostartInstalled()
	if err != nil || !installed || gotPath != path {
		t.Fatalf("installed=%v path=%s err=%v", installed, gotPath, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := ExecutablePath()
	if exe != "" && !strings.Contains(string(data), exe) {
		t.Fatalf("login item does not reference %s:\n%s", exe, data)
	}
	if err := UninstallAutostart(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	installed, _, _ = AutostartInstalled()
	if installed {
		t.Fatal("still installed after uninstall")
	}
	// Second uninstall is a no-op success.
	if err := UninstallAutostart(); err != nil {
		t.Fatalf("second uninstall: %v", err)
	}
}
