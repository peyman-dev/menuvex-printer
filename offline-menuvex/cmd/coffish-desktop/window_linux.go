//go:build linux

package main

import (
	"log"
	"os"
	"os/exec"
	"time"
)

// openDesktopWindow runs a native GTK3 + WebKit2GTK window through PyGObject
// and blocks until the window is closed. It never launches Chromium in kiosk
// mode unless no native host is available.
//
// Return value: true when a window was actually displayed (the caller shuts the
// backend down afterwards), false when no host could be started so the caller
// keeps serving and prints the URL.
func openDesktopWindow(url string) bool {
	if hasWebKit() {
		if runGTKWindow(url) {
			return true
		}
	}

	// Modern Chromium/Edge app window: behaves like a native window (no
	// address bar) and exits when the user closes it.
	for _, bin := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"} {
		path, err := exec.LookPath(bin)
		if err != nil {
			continue
		}
		cmd := exec.Command(path,
			"--app="+url,
			"--window-size=1440,900",
			"--user-data-dir="+appProfileDir(),
			"--no-first-run",
			"--no-default-browser-check",
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			continue
		}
		if err := cmd.Wait(); err == nil || cmd.ProcessState != nil {
			return true
		}
	}

	// Last resort: hand the URL to the desktop environment's default browser.
	// The window lifetime is not observable, so the caller keeps serving.
	if opener, err := exec.LookPath("xdg-open"); err == nil {
		if err := exec.Command(opener, url).Start(); err == nil {
			log.Printf("مرورگر پیش‌فرض باز شد: %s", url)
			return false
		}
	}
	return false
}

func appProfileDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := base + "/MenuVexOffline/window-profile"
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// hasWebKit verifies that PyGObject plus a WebKit2 typelib are importable
// before spawning python3, so a missing runtime degrades to another host instead
// of showing an exception in a terminal.
func hasWebKit() bool {
	py, err := exec.LookPath("python3")
	if err != nil {
		return false
	}
	probe := `import gi
gi.require_version('Gtk','3.0')
try:
    gi.require_version('WebKit2','4.1')
except ValueError:
    gi.require_version('WebKit2','4.0')
from gi.repository import Gtk, WebKit2`
	cmd := exec.Command(py, "-c", probe)
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return false
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err == nil
	case <-time.After(6 * time.Second):
		_ = cmd.Process.Kill()
		return false
	}
}

func runGTKWindow(url string) bool {
	py, err := exec.LookPath("python3")
	if err != nil {
		return false
	}
	script := `import sys, gi
gi.require_version('Gtk', '3.0')
for v in ('4.1', '4.0'):
    try:
        gi.require_version('WebKit2', v)
        break
    except ValueError:
        pass
from gi.repository import Gtk, WebKit2
win = Gtk.Window(title="منووکس — سامانه جامع مدیریت کافه و رستوران")
win.set_default_size(1440, 900)
win.connect("destroy", Gtk.main_quit)
web = WebKit2.WebView()
web.load_uri(sys.argv[1])
win.add(web)
win.show_all()
Gtk.main()
`
	cmd := exec.Command(py, "-c", script, url)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return false
	}
	// If the interpreter dies immediately (missing display, import error) treat
	// it as "no window" so a browser host can be tried next.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err == nil && cmd.ProcessState.ExitCode() == 0
	case <-time.After(2500 * time.Millisecond):
		// Still running after the grace period: a real window is up. Block
		// until the user closes it, then let the caller stop the backend.
		<-done
		return true
	}
}
