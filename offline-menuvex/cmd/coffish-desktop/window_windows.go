//go:build windows

package main

import (
	"os/exec"
)

func runNativeDesktopWindow(url string) {
	// Native Windows Desktop Form window (never Chrome or Chromium)
	psScript := `Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()
$form = New-Object System.Windows.Forms.Form
$form.Text = "منووکس — سامانه جامع مدیریت کافه و رستوران"
$form.Size = New-Object System.Drawing.Size(1400, 880)
$form.StartPosition = "CenterScreen"
$web = New-Object System.Windows.Forms.WebBrowser
$web.Dock = "Fill"
$web.ScriptErrorsSuppressed = $true
$web.Navigate("` + url + `")
$form.Controls.Add($web)
[System.Windows.Forms.Application]::Run($form)`
	_ = exec.Command("powershell", "-NoProfile", "-STA", "-WindowStyle", "Hidden", "-Command", psScript).Run()
}
