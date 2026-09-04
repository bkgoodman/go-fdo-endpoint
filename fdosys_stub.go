// SPDX-FileCopyrightText: (C) 2026 Dell Technologies, All Rights Reserved
// SPDX-License-Identifier: Apache-2.0
// Author: Brad Goodman

//go:build !fdo_sys

package main

import (
	"fmt"

	"github.com/fido-device-onboard/go-fdo/serviceinfo"
)

// createFdoSysModule stub — fdo_sys support requires building with -tags=fdo_sys
func createFdoSysModule(cfg *Config) serviceinfo.DeviceModule {
	panic(fmt.Sprintf("fdo_sys module not available (build without fdo_sys tag); disable fdo_sys in config"))
}
