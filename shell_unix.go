// SPDX-FileCopyrightText: (C) 2026 Dell Technologies, All Rights Reserved
// SPDX-License-Identifier: Apache-2.0
// Author: Brad Goodman

//go:build !windows

package main

import "os/exec"

// shellCommand runs a handler command line through the platform shell.
func shellCommand(command string) *exec.Cmd {
	return exec.Command("sh", "-c", command)
}

// defaultRunDir holds runtime state shared with the boot environment
// (SSH host keys, device IP); see fdoRunDir.
const defaultRunDir = "/run/fdo"
