// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package cli

import (
	"os"
	"syscall"

	"github.com/inconshreveable/mousetrap"
)

var (
	procGetConsoleWindow = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow")
	procAllocConsole     = syscall.NewLazyDLL("kernel32.dll").NewProc("AllocConsole")
	procAttachConsole    = syscall.NewLazyDLL("kernel32.dll").NewProc("AttachConsole")
)

// ensureConsole makes a double-click start able to read input. Explorer gives
// the process no usable console handle on some Windows versions, so one is
// attached or allocated before the prompt opens.
func ensureConsole() bool {
	if hwnd, _, _ := procGetConsoleWindow.Call(); hwnd != 0 {
		return true
	}
	if !mousetrap.StartedByExplorer() {
		return false
	}
	if r, _, _ := procAttachConsole.Call(uintptr(^uint32(0))); r != 0 {
		return reopenStreams()
	}
	if r, _, _ := procAllocConsole.Call(); r != 0 {
		return reopenStreams()
	}
	return false
}

// reopenStreams rebinds the standard streams to the console just created.
func reopenStreams() bool {
	in, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		in.Close()
		return false
	}
	os.Stdin = in
	os.Stdout = out
	os.Stderr = out
	return true
}
