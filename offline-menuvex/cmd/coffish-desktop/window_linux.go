//go:build linux

package main

import (
	"os"
	"os/exec"
)

func runNativeDesktopWindow(url string) {
	// Native Linux GTK3 + WebKit2GTK window (never Chrome or Chromium)
	pyScript := `import sys, gi
gi.require_version('Gtk', '3.0')
for v in ('4.1', '4.0'):
    try:
        gi.require_version('WebKit2', v)
        break
    except ValueError:
        pass
from gi.repository import Gtk, WebKit2
win = Gtk.Window(title="منووکس — سامانه جامع مدیریت کافه و رستوران")
win.set_default_size(1400, 880)
win.connect("destroy", Gtk.main_quit)
web = WebKit2.WebView()
web.load_uri(sys.argv[1])
win.add(web)
win.show_all()
Gtk.main()
`
	if p, err := exec.LookPath("python3"); err == nil {
		cmd := exec.Command(p, "-c", pyScript, url)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err == nil {
			return
		}
	}
	select {}
}
