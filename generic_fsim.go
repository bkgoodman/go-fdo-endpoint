// SPDX-FileCopyrightText: (C) 2026 Dell Technologies, All Rights Reserved
// SPDX-License-Identifier: Apache-2.0
// Author: Brad Goodman

package main

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/fido-device-onboard/go-fdo/fsim"
	"github.com/fido-device-onboard/go-fdo/serviceinfo"
)

// GenericFSIMCallbacks creates FSIM callbacks that use the generic handler manager
func GenericFSIMCallbacks(handlerManager *GenericHandlerManager) *FSIMCallbacks {
	return &FSIMCallbacks{
		SysConfigSetParameter: func(parameter, value string) error {
			fmt.Printf("[SYSCONFIG] %s = %s (processing...)\n", parameter, value)

			// Use the generic handler manager
			if err := handlerManager.HandleSysConfigParameter(parameter, value); err != nil {
				fmt.Printf("[ERROR] SysConfig handler failed: %v\n", err)
				return err
			}

			return nil
		},

		DownloadStart: func(filename string) error {
			fmt.Printf("[DOWNLOAD] Started: %s\n", filename)
			return nil
		},
		DownloadData: func(filename string, data []byte) error {
			fmt.Printf("[DOWNLOAD] Data for %s: %d bytes\n", filename, len(data))
			return nil
		},
		DownloadEnd: func(filename string) error {
			fmt.Printf("[DOWNLOAD] Completed: %s\n", filename)
			return nil
		},

		UploadStart: func(filename string) error {
			fmt.Printf("[UPLOAD] Started: %s\n", filename)
			return nil
		},
		UploadData: func(filename string, data []byte) ([]byte, error) {
			fmt.Printf("[UPLOAD] Data for %s: %d bytes\n", filename, len(data))
			return data, nil // Echo back the data
		},
		UploadEnd: func(filename string) error {
			fmt.Printf("[UPLOAD] Completed: %s\n", filename)
			return nil
		},

		CommandExecute: func(command string) error {
			fmt.Printf("[COMMAND] Execute: %s\n", command)
			return nil
		},
		CommandOutput: func(command string, output string) error {
			fmt.Printf("[COMMAND] Output for %s: %s\n", command, output)
			return nil
		},

		// Payload callbacks using generic handler manager
		PayloadHandle: func(ctx context.Context, mimeType, name string, size uint64, metadata map[string]any, payload []byte) (statusCode int, message string, err error) {
			hash := sha256.Sum256(payload)
			fmt.Printf("[📦 PAYLOAD] %s: name=%s size=%d bytes (SHA256: %x)\n", mimeType, name, size, hash)

			// Log metadata if present
			if len(metadata) > 0 {
				fmt.Printf("  -> Metadata: %+v\n", metadata)
			}

			// Use the generic handler manager
			statusCode, message, err = handlerManager.HandlePayload(ctx, mimeType, name, size, metadata, payload)
			if err != nil {
				fmt.Printf("[ERROR] Payload handler failed: %v\n", err)
				return statusCode, message, err
			}

			fmt.Printf("[SUCCESS] Payload processed: %s\n", message)
			return statusCode, message, nil
		},
	}
}

// GenericPayloadAckHandler implements payload acceptance/rejection based on configuration
type GenericPayloadAckHandler struct {
	handlerManager *GenericHandlerManager
}

// NewGenericPayloadAckHandler creates a new payload ack handler
func NewGenericPayloadAckHandler(handlerManager *GenericHandlerManager) *GenericPayloadAckHandler {
	return &GenericPayloadAckHandler{
		handlerManager: handlerManager,
	}
}

// AcceptPayload decides whether to accept or reject a payload based on configuration.
// Returns (accepted, reasonCode, message)
func (h *GenericPayloadAckHandler) AcceptPayload(mimeType string, name string, size uint64, metadata map[string]any) (bool, int, string) {
	fmt.Printf("[🚫 PAYLOAD ACK] Received payload: %s (name=%s, size=%d)\n", mimeType, name, size)

	// Check if we have a handler for this MIME type
	mimeHandler, exists := h.handlerManager.config.Handlers.Payload.MimeTypes[mimeType]

	if !exists || !mimeHandler.Enabled {
		// Handle based on default action
		switch h.handlerManager.config.Handlers.Payload.DefaultAction {
		case "reject":
			fmt.Printf("[❌ PAYLOAD ACK] Rejecting unknown payload type: %s\n", mimeType)
			return false, 1, fmt.Sprintf("Unsupported payload type: %s", mimeType)
		case "accept":
			fmt.Printf("[✅ PAYLOAD ACK] Accepting payload (no specific handler): %s\n", mimeType)
			return true, 0, fmt.Sprintf("Payload accepted: %s", mimeType)
		case "require_handler":
			fmt.Printf("[❌ PAYLOAD ACK] Rejecting payload (handler required): %s\n", mimeType)
			return false, 1, fmt.Sprintf("No handler configured for MIME type: %s", mimeType)
		default:
			fmt.Printf("[❌ PAYLOAD ACK] Rejecting payload (invalid default action): %s\n", mimeType)
			return false, 1, fmt.Sprintf("Invalid default action for payload type: %s", mimeType)
		}
	}

	// We have a handler, accept the payload
	fmt.Printf("[✅ PAYLOAD ACK] Accepting payload with handler: %s\n", mimeType)
	return true, 0, fmt.Sprintf("Payload accepted: %s", mimeType)
}

// CreateGenericFSIMModules creates FSIM modules using the generic handler manager
func CreateGenericFSIMModules(handlerManager *GenericHandlerManager) map[string]serviceinfo.DeviceModule {
	modules := make(map[string]serviceinfo.DeviceModule)

	// Interop module (always included)
	modules["fido_alliance"] = &fsim.Interop{}

	// SysConfig module with generic handlers
	callbacks := GenericFSIMCallbacks(handlerManager)
	if callbacks.SysConfigSetParameter != nil {
		modules["fdo.sysconfig"] = &fsim.SysConfig{
			SetParameter: callbacks.SysConfigSetParameter,
		}
	}

	// Payload module with generic handlers and ack handler
	if callbacks.PayloadHandle != nil {
		payloadDevice := &fsim.Payload{
			UnifiedHandler: &PayloadHandler{callback: callbacks.PayloadHandle},
			AckHandler:     NewGenericPayloadAckHandler(handlerManager),
			Active:         true,
		}
		modules["fdo.payload"] = payloadDevice
	}

	return modules
}

// PrintHandlerConfiguration prints the current handler configuration
func PrintHandlerConfiguration(handlerManager *GenericHandlerManager) {
	fmt.Printf("\n=== Generic Handler Configuration ===\n")

	// Print sysconfig handlers
	fmt.Printf("\nSysConfig Handlers:\n")
	for param, handler := range handlerManager.config.Handlers.SysConfig {
		status := "disabled"
		if handler.Enabled {
			status = "enabled"
		}
		fmt.Printf("  %s: %s (command: %s)\n", param, status, handler.Command)
	}

	// Print payload handlers
	fmt.Printf("\nPayload Handlers:\n")
	fmt.Printf("  Temp Directory: %s\n", handlerManager.config.Handlers.Payload.TempDir)
	fmt.Printf("  Default Action: %s\n", handlerManager.config.Handlers.Payload.DefaultAction)

	fmt.Printf("  MIME Type Handlers:\n")
	for mimeType, handler := range handlerManager.config.Handlers.Payload.MimeTypes {
		status := "disabled"
		if handler.Enabled {
			status = "enabled"
		}
		fmt.Printf("    %s: %s (command: %s)\n", mimeType, status, handler.Command)
	}

	fmt.Printf("\n========================================\n\n")
}

// ValidateAndPrintHandlers validates the configuration and prints it
func ValidateAndPrintHandlers(configPath string) (*GenericHandlerManager, error) {
	handlerManager, err := NewGenericHandlerManager(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create handler manager: %w", err)
	}

	if err := handlerManager.ValidateConfig(); err != nil {
		return nil, fmt.Errorf("handler configuration validation failed: %w", err)
	}

	PrintHandlerConfiguration(handlerManager)
	return handlerManager, nil
}
