// SPDX-FileCopyrightText: (C) 2026 Dell Technologies, All Rights Reserved
// SPDX-License-Identifier: Apache-2.0
// Author: Brad Goodman

package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFdoRunDirOverrideAndDefault(t *testing.T) {
	t.Setenv("FDO_RUN_DIR", "")
	if got := fdoRunDir(); got != defaultRunDir || got == "" {
		t.Fatalf("default run dir = %q, want %q", got, defaultRunDir)
	}
	if runtime.GOOS != "windows" && defaultRunDir != "/run/fdo" {
		t.Fatalf("non-Windows default changed: %q", defaultRunDir)
	}
	t.Setenv("FDO_RUN_DIR", "/custom/fdo")
	if got := fdoRunDir(); got != "/custom/fdo" {
		t.Fatalf("override ignored: %q", got)
	}
}

func TestDeviceIPPrefersFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "device-ip.txt"), []byte(" 192.0.2.10\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := deviceIP(dir); got != "192.0.2.10" {
		t.Fatalf("deviceIP = %q, want 192.0.2.10 from file", got)
	}
}

func TestDeviceIPFallbackIsGlobalIPv4OrEmpty(t *testing.T) {
	got := deviceIP(t.TempDir())
	if got == "" {
		t.Skip("no global IPv4 address on this host")
	}
	ip := net.ParseIP(got)
	if ip == nil || ip.To4() == nil || ip.IsLoopback() || !ip.IsGlobalUnicast() {
		t.Fatalf("fallback returned %q, want a global unicast IPv4", got)
	}
}

func TestShellCommandRunsPlatformShell(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	cmdline := `echo "quoted arg" > ` + marker
	out, err := shellCommand(cmdline).CombinedOutput()
	if err != nil {
		t.Fatalf("shellCommand: %v: %s", err, out)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("command did not run: %v", err)
	}
	if !strings.Contains(string(data), "quoted arg") {
		t.Fatalf("marker = %q", data)
	}
}

func TestShellCommandReportsFailure(t *testing.T) {
	if err := shellCommand("exit 3").Run(); err == nil {
		t.Fatal("exit 3 reported success")
	}
}
