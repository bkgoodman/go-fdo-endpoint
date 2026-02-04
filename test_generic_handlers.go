// SPDX-FileCopyrightText: (C) 2024 Intel Corporation
// SPDX-License-Identifier: Apache 2.0

package main

import (
	"context"
	"fmt"
	"os"
)

// TestGenericHandlers demonstrates the generic handler functionality
func TestGenericHandlers() {
	fmt.Printf("=== Testing Generic FDO Client Handlers ===\n\n")

	// Test 1: Load and validate configuration
	fmt.Printf("1. Testing configuration loading...\n")
	handlerManager, err := ValidateAndPrintHandlers("config_test.yaml")
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		return
	}
	fmt.Printf("✓ Configuration loaded and validated successfully\n\n")

	// Test 2: Test sysconfig handlers
	fmt.Printf("2. Testing sysconfig handlers...\n")
	testParams := map[string]string{
		"hostname":   "test-device",
		"timezone":   "America/New_York",
		"ntp-server": "time.google.com",
		"locale":     "en_US.UTF-8",
		"dns-server": "8.8.8.8",
		"unknown":    "should-fail",
	}

	for param, value := range testParams {
		fmt.Printf("  Testing %s = %s: ", param, value)
		if err := handlerManager.HandleSysConfigParameter(param, value); err != nil {
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

	fmt.Printf("\n=== Test Complete ===\n")
}

// TestHandlerTemplates tests the command template functionality
func TestHandlerTemplates() {
	fmt.Printf("=== Testing Handler Templates ===\n\n")

	handlerManager, err := NewGenericHandlerManager("config_test.yaml")
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		return
	}

	// Test template execution
	fmt.Printf("Testing template execution...\n")
	testCases := []struct {
		template string
		vars     map[string]interface{}
		expected string
	}{
		{
			template: "echo 'Setting hostname to: {value}'",
			vars:     map[string]interface{}{"value": "test-host"},
			expected: "echo 'Setting hostname to: test-host'",
		},
		{
			template: "echo 'Processing {filename} ({size} bytes)'",
			vars:     map[string]interface{}{"filename": "test.bin", "size": uint64(1024)},
			expected: "echo 'Processing test.bin (1024 bytes)'",
		},
	}

	for i, testCase := range testCases {
		fmt.Printf("  Test %d: ", i+1)
		if err := handlerManager.executeCommandTemplate(testCase.template, testCase.vars); err != nil {
			fmt.Printf("FAILED (%v)\n", err)
		} else {
			fmt.Printf("SUCCESS\n")
		}
	}

	fmt.Printf("\n=== Template Test Complete ===\n")
}

// TestErrorReporting demonstrates error reporting capabilities
func TestErrorReporting() {
	fmt.Printf("=== Testing Error Reporting ===\n\n")

	// Test 1: Invalid configuration
	fmt.Printf("1. Testing invalid configuration handling...\n")
	invalidConfig := `handlers:
  sysconfig:
    hostname:
      enabled: true
      # Missing command - should fail validation
  payload:
    temp_dir: "relative/path"  # Should fail validation
    default_action: "invalid"  # Should fail validation
`

	// Write invalid config to temp file
	tmpFile := "/tmp/invalid_config.yaml"
	if err := os.WriteFile(tmpFile, []byte(invalidConfig), 0644); err != nil {
		fmt.Printf("ERROR: Could not write temp config: %v\n", err)
		return
	}
	defer os.Remove(tmpFile)

	if _, err := ValidateAndPrintHandlers(tmpFile); err != nil {
		fmt.Printf("✓ Invalid configuration properly rejected: %v\n", err)
	} else {
		fmt.Printf("✗ Invalid configuration was accepted (should have failed)\n")
	}

	// Test 2: Missing handler
	fmt.Printf("\n2. Testing missing handler handling...\n")
	handlerManager, _ := NewGenericHandlerManager("config_test.yaml")

	if err := handlerManager.HandleSysConfigParameter("missing-param", "value"); err != nil {
		fmt.Printf("✓ Missing parameter properly rejected: %v\n", err)
	} else {
		fmt.Printf("✗ Missing parameter was accepted (should have failed)\n")
	}

	// Test 3: Disabled handler
	fmt.Printf("\n3. Testing disabled handler handling...\n")
	// Create a config with disabled handler
	disabledConfig := `handlers:
  sysconfig:
    hostname:
      command: "echo 'test'"
      enabled: false
  payload:
    temp_dir: "/tmp/test"
    default_action: "reject"
    mime_types: {}
`

	tmpFile2 := "/tmp/disabled_config.yaml"
	if err := os.WriteFile(tmpFile2, []byte(disabledConfig), 0644); err != nil {
		fmt.Printf("ERROR: Could not write temp config: %v\n", err)
		return
	}
	defer os.Remove(tmpFile2)

	if hm, _ := NewGenericHandlerManager(tmpFile2); hm != nil {
		if err := hm.HandleSysConfigParameter("hostname", "test"); err != nil {
			fmt.Printf("✓ Disabled handler properly rejected: %v\n", err)
		} else {
			fmt.Printf("✗ Disabled handler was accepted (should have failed)\n")
		}
	}

	fmt.Printf("\n=== Error Reporting Test Complete ===\n")
}

func runTests() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "handlers":
			TestGenericHandlers()
		case "templates":
			TestHandlerTemplates()
		case "errors":
			TestErrorReporting()
		default:
			fmt.Printf("Usage: %s [handlers|templates|errors]\n", os.Args[0])
		}
	} else {
		fmt.Printf("Generic FDO Client Handler Test Suite\n")
		fmt.Printf("Usage: %s [handlers|templates|errors]\n", os.Args[0])
		fmt.Printf("\nRunning all tests...\n\n")
		TestGenericHandlers()
		fmt.Printf("\n")
		TestHandlerTemplates()
		fmt.Printf("\n")
		TestErrorReporting()
	}
}
