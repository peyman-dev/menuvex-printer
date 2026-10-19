//go:build windows

package main

import (
	"log"
	"syscall"
	"unsafe"
)

// fatal shows a blocking MessageBox for unrecoverable startup errors because a
// GUI-subsystem binary has no console to print to.
func fatal(msg string) {
	log.Print(msg)
	user32 := syscall.NewLazyDLL("user32.dll")
	title, _ := syscall.UTF16PtrFromString("MenuVex Offline")
	text, err := syscall.UTF16PtrFromString(msg)
	if err != nil {
		return
	}
	user32.NewProc("MessageBoxW").Call(0,
		uintptr(unsafe.Pointer(text)),
		uintptr(unsafe.Pointer(title)),
		0x10, // MB_ICONERROR
	)
	panic(msg)
}
