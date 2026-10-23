// SPDX-FileCopyrightText: (C) 2026 Dell Technologies, All Rights Reserved
// SPDX-License-Identifier: Apache-2.0
// Author: Brad Goodman

package main

import (
	"net"
	"os"
	"path/filepath"
)

// fdoRunDir is where the boot environment and the endpoint exchange runtime
// state: ssh-host-keys/ (from -gen-ssh-keys) and device-ip.txt.
// FDO_RUN_DIR overrides the platform default (/run/fdo on Linux,
// %ProgramData%\fdo on Windows).
func fdoRunDir() string {
	if d := os.Getenv("FDO_RUN_DIR"); d != "" {
		return d
	}
	return defaultRunDir
}

// deviceIP returns the device address to report to the owner: the contents of
// <runDir>/device-ip.txt if the boot environment wrote one, else the first
// non-loopback IPv4 address of an up interface. Convenience only; device
// identity is the SSH host keys.
func deviceIP(runDir string) string {
	if ip := readTrimmed(filepath.Join(runDir, "device-ip.txt")); ip != "" {
		return ip
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsGlobalUnicast() {
				return n.IP.String()
			}
		}
	}
	return ""
}
