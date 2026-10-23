// SPDX-FileCopyrightText: (C) 2026 Dell Technologies, All Rights Reserved
// SPDX-License-Identifier: Apache-2.0
// Author: Brad Goodman

//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// shellCommand runs a handler command line through cmd.exe. The command line
// is passed verbatim (SysProcAttr.CmdLine): Go's default argument quoting
// follows CommandLineToArgvW rules, which cmd.exe does not use, so quotes
// inside handler commands would otherwise be mangled.
func shellCommand(command string) *exec.Cmd {
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		comspec = "cmd.exe"
	}
	cmd := exec.Command(comspec)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /S /C "` + command + `"`}
	return cmd
}

// defaultRunDir holds runtime state shared with the boot environment
// (SSH host keys, device IP); see fdoRunDir. %ProgramData% exists in both
// WinPE (X:\ProgramData) and installed Windows.
var defaultRunDir = func() string {
	if pd := os.Getenv("ProgramData"); pd != "" {
		return filepath.Join(pd, "fdo")
	}
	return filepath.Join(os.TempDir(), "fdo")
}()
