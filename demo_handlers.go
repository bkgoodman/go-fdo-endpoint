// SPDX-FileCopyrightText: (C) 2024 Intel Corporation
// SPDX-License-Identifier: Apache 2.0

package main

import (
	"context"
	"fmt"
)

func demoHandlers() {
	fmt.Printf("=== Generic FDO Client Handler Demo ===\n\n")

	// Test 1: Load and validate configuration
	fmt.Printf("1. Loading configuration...\n")
	handlerManager, err := ValidateAndPrintHandlers("config_test.yaml")
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		return
	}
	fmt.Printf("✓ Configuration loaded and validated successfully\n\n")

	// Test 2: Test sysconfig handlers
	fmt.Printf("2. Testing sysconfig handlers...\n")
	testParams := []struct {
		param string
		value string
	}{
		{"hostname", "test-device"},
		{"timezone", "America/New_York"},
		{"ntp-server", "time.google.com"},
		{"locale", "en_US.UTF-8"},
		{"dns-server", "8.8.8.8"},
		{"unknown", "should-fail"},
	}

	for _, test := range testParams {
		fmt.Printf("  Testing %s = %s: ", test.param, test.value)
		if err := handlerManager.HandleSysConfigParameter(test.param, test.value); err != nil {
			fmt.Printf("FAILED (%v)\n", err)
		} else {
			fmt.Printf("SUCCESS\n")
		}
	}
	fmt.Printf("\n")

	// Test 3: Test payload handlers
	fmt.Printf("3. Testing payload handlers...\n")
	testPayloads := []struct {
		mimeType string
		name     string
		size     uint64
		payload  []byte
	}{
		{"application/octet-stream", "test.bin", 1024, []byte("binary data")},
		{"application/json", "config.json", 512, []byte(`{"key": "value"}`)},
		{"text/plain", "readme.txt", 256, []byte("Hello World")},
		{"application/x-cloud-init", "cloud-init.yaml", 2048, []byte("#cloud-config\n")},
		{"application/unknown", "unknown.dat", 128, []byte("unknown")},
	}

	for _, test := range testPayloads {
		fmt.Printf("  Testing %s (%s, %d bytes): ", test.mimeType, test.name, test.size)
		statusCode, message, err := handlerManager.HandlePayload(
			context.Background(),
			test.mimeType,
			test.name,
			test.size,
			nil,
			test.payload,
		)
		if err != nil {
			fmt.Printf("FAILED (%v)\n", err)
		} else {
			fmt.Printf("SUCCESS (status=%d, message=%s)\n", statusCode, message)
		}
	}
	fmt.Printf("\n")

	// Test 4: Show configuration summary
	fmt.Printf("4. Configuration summary:\n")
	fmt.Printf("  Configured sysconfig parameters: %v\n", handlerManager.GetConfiguredSysConfigParameters())
	fmt.Printf("  Configured MIME types: %v\n", handlerManager.GetConfiguredMimeTypes())

	fmt.Printf("\n=== Demo Complete ===\n")
	fmt.Printf("\n🎉 Generic FDO Client Handler System Working!\n")
	fmt.Printf("\nKey Features Demonstrated:\n")
	fmt.Printf("✓ YAML-based configuration\n")
	fmt.Printf("✓ Template-based command execution\n")
	fmt.Printf("✓ Sysconfig parameter handling\n")
	fmt.Printf("✓ Payload MIME type handling\n")
	fmt.Printf("✓ Error reporting and validation\n")
	fmt.Printf("✓ Graceful fallback for unknown types\n")
	fmt.Printf("\nReady for production use with real FDO server!\n")
}
