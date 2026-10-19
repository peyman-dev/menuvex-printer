//go:build windows

package main

import (
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

// openDesktopWindow opens the UI in a modern Chromium-based app window (Edge is
// present on every supported Windows 10/11 installation). The window has no
// address bar and the process waits until it is closed, so closing the window
// stops the local backend — the same lifecycle as the macOS and Linux builds.
//
// The legacy WinForms/IE window is deliberately not used: the MenuVex UI relies
// on modern CSS (grid, custom properties) that the Trident engine cannot render.
func openDesktopWindow(url string) bool {
	profile := appProfileDir()
	candidates := []string{
		filepathJoin(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
		filepathJoin(os.Getenv("ProgramFiles"), `Microsoft\Edge\Application\msedge.exe`),
		filepathJoin(os.Getenv("ProgramFiles"), `Google\Chrome\Application\chrome.exe`),
		filepathJoin(os.Getenv("ProgramFiles(x86)"), `Google\Chrome\Application\chrome.exe`),
		filepathJoin(os.Getenv("LOCALAPPDATA"), `Google\Chrome\Application\chrome.exe`),
	}
	for _, path := range candidates {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		cmd := exec.Command(path,
			"--app="+url,
			"--window-size=1440,900",
			"--user-data-dir="+profile,
			"--no-first-run",
			"--no-default-browser-check",
		)
		if err := cmd.Start(); err != nil {
			continue
		}
		_ = cmd.Wait()
		return true
	}

	// No Chromium host: open the default browser. The window lifetime cannot be
	// observed, so keep the backend alive and let the operator close the app.
	if err := shellOpen(url); err == nil {
		log.Printf("مرورگر پیش‌فرض باز شد: %s", url)
		return false
	}
	return false
}

func filepathJoin(base, suffix string) string {
	if base == "" {
		return ""
	}
	return base + `\` + suffix
}

func appProfileDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := base + `\MenuVexOffline\window-profile`
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

var (
	shell32       = syscall.NewLazyDLL("shell32.dll")
	procShellExec = shell32.NewProc("ShellExecuteW")
)

// shellOpen runs ShellExecuteW("open", url) so no console window is created and
// the default browser handles the URL.
func shellOpen(target string) error {
	verb, _ := syscall.UTF16PtrFromString("open")
	file, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	ret, _, callErr := procShellExec.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		0, 0, 1, // SW_SHOWNORMAL
	)
	if ret <= 32 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return callErr
		}
		return os.ErrInvalid
	}
	// Give the browser a moment to come up before the caller decides what to do.
	time.Sleep(1200 * time.Millisecond)
	return nil
}
