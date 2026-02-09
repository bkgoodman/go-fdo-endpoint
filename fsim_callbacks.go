// SPDX-FileCopyrightText: (C) 2026 Dell Technologies, All Rights Reserved
// SPDX-License-Identifier: Apache-2.0
// Author: Brad Goodman

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/fido-device-onboard/go-fdo"
	"github.com/fido-device-onboard/go-fdo/cbor"
	"github.com/fido-device-onboard/go-fdo/fsim"
	"github.com/fido-device-onboard/go-fdo/serviceinfo"
)

// FSIMCallbacks holds all the callback implementations for various FSIM modules
type FSIMCallbacks struct {
	// SysConfig callbacks
	SysConfigSetParameter func(parameter, value string) error

	// Add more FSIM callbacks here as needed
	// Download callbacks
	DownloadStart func(filename string) error
	DownloadData  func(filename string, data []byte) error
	DownloadEnd   func(filename string) error

	// Upload callbacks
	UploadStart func(filename string) error
	UploadData  func(filename string, data []byte) ([]byte, error)
	UploadEnd   func(filename string) error

	// Command callbacks
	CommandExecute func(command string) error
	CommandOutput  func(command string, output string) error

	// Payload callbacks - using the correct interface from go-fdo
	PayloadHandle func(ctx context.Context, mimeType, name string, size uint64, metadata map[string]any, payload []byte) (statusCode int, message string, err error)

	// Add more FSIM types as needed
}

// DefaultFSIMCallbacks provides default implementations for all FSIM callbacks
func DefaultFSIMCallbacks() *FSIMCallbacks {
	return &FSIMCallbacks{
		SysConfigSetParameter: func(parameter, value string) error {
			fmt.Printf("[SYSCONFIG] Received parameter: %s = %s\n", parameter, value)
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

		// Payload callbacks
		PayloadHandle: func(ctx context.Context, mimeType, name string, size uint64, metadata map[string]any, payload []byte) (statusCode int, message string, err error) {
			hash := sha256.Sum256(payload)
			fmt.Printf("[PAYLOAD] %s: name=%s size=%d bytes (SHA256: %x)\n", mimeType, name, size, hash)

			// Log metadata if present
			if len(metadata) > 0 {
				fmt.Printf("  -> Metadata: %+v\n", metadata)
			}

			return 0, "Payload received successfully", nil
		},
	}
}

// FDOEventHandler implements the fdo.EventHandler interface to display library events
type FDOEventHandler struct{}

// HandleEvent processes FDO events from the library
func (h *FDOEventHandler) HandleEvent(ctx context.Context, event fdo.Event) {
	fmt.Printf("*** EVENT HANDLER CALLED *** Type: %s\n", event.Type.String())

	// Dump ALL event data comprehensively
	fmt.Printf("\n    Timestamp: %s", event.Timestamp.Format("2006-01-02 15:04:05.000"))
	if event.ProtocolVersion > 0 {
		fmt.Printf("\n    Protocol: v%d", event.ProtocolVersion)
	}
	if event.GUID != nil {
		fmt.Printf("\n    GUID: %s", event.GUID.String())
	}
	if event.MessageType != nil {
		fmt.Printf("\n    Message Type: %d", *event.MessageType)
	}
	if event.Error != nil {
		fmt.Printf("\n    Error: %v", event.Error)
	}

	// Type-specific data
	switch event.Type {
	case fdo.EventTypeDIStarted:
		fmt.Printf("\n    🚀 DI Started - Device beginning initialization")
	case fdo.EventTypeDIAppStartReceived:
		fmt.Printf("\n    📨 DI AppStart Received - Manufacturer info processed")
	case fdo.EventTypeDIVoucherCreated:
		fmt.Printf("\n    📋 DI Voucher Created - Device voucher generated")
	case fdo.EventTypeDICompleted:
		if data, ok := event.Data.(fdo.DIEventData); ok {
			fmt.Printf("\n    ✅ DI Completed - Device: %s", data.DeviceInfo)
		}
	case fdo.EventTypeDIFailed:
		fmt.Printf("\n    ❌ DI Failed - Initialization error")
	case fdo.EventTypeTO2Started:
		fmt.Printf("\n    🚀 TO2 Started!")
	case fdo.EventTypeTO2Completed:
		if data, ok := event.Data.(fdo.TO2EventData); ok {
			fmt.Printf("\n    ✅ TO2 Completed - Credential Reuse: %t, Attestation Mode: %v", data.CredentialReuse, data.AttestationMode)
		}
	case fdo.EventTypeTO2Failed:
		if data, ok := event.Data.(fdo.TO2EventData); ok {
			fmt.Printf("\n    ❌ TO2 Failed - Credential Reuse: %t, Attestation Mode: %v", data.CredentialReuse, data.AttestationMode)
		}
	}

	fmt.Printf("\n")
}

// PayloadHandler implements the UnifiedPayloadHandler interface
type PayloadHandler struct {
	callback func(ctx context.Context, mimeType, name string, size uint64, metadata map[string]any, payload []byte) (statusCode int, message string, err error)
}

// HandlePayload implements the UnifiedPayloadHandler interface
func (h *PayloadHandler) HandlePayload(ctx context.Context, mimeType string, name string, size uint64, metadata map[string]any, payload []byte) (statusCode int, message string, err error) {
	if h.callback == nil {
		return 0, "success", nil
	}
	return h.callback(ctx, mimeType, name, size, metadata, payload)
}

// PayloadAckHandler implements the PayloadAckHandler interface to accept/reject payloads
type PayloadAckHandler struct{}

// AcceptPayload decides whether to accept or reject a payload based on metadata.
// Returns (accepted, reasonCode, message)
func (h *PayloadAckHandler) AcceptPayload(mimeType string, name string, size uint64, metadata map[string]any) (bool, int, string) {
	fmt.Printf("[🚫 PAYLOAD ACK] Received payload: %s (name=%s, size=%d)\n", mimeType, name, size)

	// Accept known payload types
	switch mimeType {
	case "application/json":
		fmt.Printf("[✅ PAYLOAD ACK] Accepting JSON payload\n")
		return true, 0, "JSON payload accepted"
	case "text/plain":
		fmt.Printf("[✅ PAYLOAD ACK] Accepting text payload\n")
		return true, 0, "Text payload accepted"
	case "application/octet-stream":
		fmt.Printf("[✅ PAYLOAD ACK] Accepting binary payload\n")
		return true, 0, "Binary payload accepted"
	default:
		fmt.Printf("[❌ PAYLOAD ACK] Rejecting unknown payload type: %s\n", mimeType)
		return false, 1, fmt.Sprintf("Unsupported payload type: %s", mimeType)
	}
}

// PayloadDeviceWrapper wraps fsim.Payload to add debugging
type PayloadDeviceWrapper struct {
	*fsim.Payload
	callbacks *FSIMCallbacks
}

// Transition wraps the original Transition
func (p *PayloadDeviceWrapper) Transition(active bool) error {
	return p.Payload.Transition(active)
}

// Receive wraps the original Receive to add debugging and RequireAck support
func (p *PayloadDeviceWrapper) Receive(ctx context.Context, messageName string, messageBody io.Reader, respond func(string) io.Writer, yield func()) error {
	fmt.Printf("[DEBUG] PayloadDeviceWrapper.Receive called: messageName=%s\n", messageName)

	// Read the message body
	data, err := io.ReadAll(messageBody)
	if err != nil {
		return err
	}

	// Trigger events for payload messages
	switch messageName {
	case "payload-begin":
		fmt.Printf("[DEBUG] Processing payload-begin message\n")
		fmt.Printf("[DEBUG] CBOR data: %x\n", data)

		// Parse the payload-begin message for callback purposes
		if err := p.parsePayloadBeginForCallback(data); err != nil {
			fmt.Printf("[DEBUG] Payload-begin parsing failed: %v\n", err)
		}

	case "payload-data-0", "payload-data-1", "payload-data-2":
		// Payload data chunks
		chunkNum := 0
		if _, err := fmt.Sscanf(messageName, "payload-data-%d", &chunkNum); err == nil {
			fmt.Printf("[DEBUG] Received payload chunk %d: %d bytes\n", chunkNum, len(data))
			fmt.Printf("[EVENT] payload_data_received: chunk=%d, size=%d\n", chunkNum, len(data))
		}

	case "payload-end":
		// Payload transfer complete
		fmt.Printf("[DEBUG] Payload transfer completed\n")
		fmt.Printf("[EVENT] payload_complete: status=completed\n")
	}

	// Call the original receive method - let the library handle RequireAck automatically
	return p.Payload.Receive(ctx, messageName, bytes.NewReader(data), respond, yield)
}

// Yield wraps the original Yield
func (p *PayloadDeviceWrapper) Yield(ctx context.Context, respond func(string) io.Writer, yield func()) error {
	return p.Payload.Yield(ctx, respond, yield)
}

// parsePayloadBeginForCallback parses payload-begin for callback purposes only
func (p *PayloadDeviceWrapper) parsePayloadBeginForCallback(data []byte) error {
	// Parse the CBOR data to extract metadata for callback
	var metadata map[string]interface{}

	// Try to parse with string keys
	if err := cbor.Unmarshal(data, &metadata); err != nil {
		fmt.Printf("[DEBUG] CBOR unmarshal failed in callback: %v\n", err)
		return nil // Don't fail the callback parsing
	}

	fmt.Printf("[DEBUG] Parsed payload metadata for callback: %+v\n", metadata)

	// Extract metadata fields
	mimeType := ""
	name := ""
	size := uint64(0)

	if mt, ok := metadata["mimeType"].(string); ok {
		mimeType = mt
	}
	if n, ok := metadata["name"].(string); ok {
		name = n
	}
	if s, ok := metadata["size"].(uint64); ok {
		size = s
	}

	fmt.Printf("[EVENT] payload_received: mimeType=%s, name=%s, size=%d\n", mimeType, name, size)

	// Trigger custom callback
	if p.callbacks.PayloadHandle != nil {
		fmt.Printf("[DEBUG] Calling PayloadHandle callback\n")
		statusCode, message, err := p.callbacks.PayloadHandle(context.Background(), mimeType, name, size, metadata, nil)
		fmt.Printf("[DEBUG] PayloadHandle returned: status=%d, message=%s, err=%v\n", statusCode, message, err)
	}

	return nil
}

// getModuleNames extracts module names from the modules map
func getModuleNames(modules map[string]serviceinfo.DeviceModule) []string {
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	return names
}

// CreateFSIMModules creates and configures all FSIM modules with the provided callbacks
func CreateFSIMModules(callbacks *FSIMCallbacks) map[string]serviceinfo.DeviceModule {
	modules := make(map[string]serviceinfo.DeviceModule)

	// Interop module (always included)
	modules["fido_alliance"] = &fsim.Interop{}

	// SysConfig module
	if callbacks.SysConfigSetParameter != nil {
		// Wrap the original callback to include events
		originalCallback := callbacks.SysConfigSetParameter
		callbacks.SysConfigSetParameter = func(parameter, value string) error {
			// Call original callback
			err := originalCallback(parameter, value)
			return err
		}

		modules["fdo.sysconfig"] = &fsim.SysConfig{
			SetParameter: callbacks.SysConfigSetParameter,
		}
	}

	// Payload module
	if callbacks.PayloadHandle != nil {
		// Create the payload module with the unified handler and ack handler (no wrapper)
		payloadDevice := &fsim.Payload{
			UnifiedHandler: &PayloadHandler{callback: callbacks.PayloadHandle},
			AckHandler:     &PayloadAckHandler{}, // Add ack handler to reject unknown types
			Active:         true,
		}
		fmt.Printf("[DEBUG] Creating payload module: %T\n", payloadDevice)
		modules["fdo.payload"] = payloadDevice
	}

	return modules
}

// RegisterFDOEventHandler registers the library event handler to show FDO events
func RegisterFDOEventHandler() {
	eventHandler := &FDOEventHandler{}
	fdo.RegisterEventHandler(eventHandler)
	fmt.Printf("[DEBUG] Registered FDO event handler with library\n")
}

// CustomFSIMCallbacks allows partial override of default callbacks
type CustomFSIMCallbacks struct {
	*FSIMCallbacks
	// Override any specific callbacks here
}

// Example of how to create custom callbacks for specific use cases
func CreateCustomFSIMCallbacks() *FSIMCallbacks {
	defaults := DefaultFSIMCallbacks()

	// Override specific callbacks
	defaults.SysConfigSetParameter = func(parameter, value string) error {
		fmt.Printf("[CUSTOM SYSCONFIG] %s = %s (processing...)\n", parameter, value)

		// Add custom processing logic here
		switch parameter {
		case "hostname":
			// Custom hostname processing
			fmt.Printf("  -> Setting hostname to: %s\n", value)
		case "timezone":
			// Custom timezone processing
			fmt.Printf("  -> Setting timezone to: %s\n", value)
		case "ntp-server":
			// Custom NTP server processing
			fmt.Printf("  -> Setting NTP server to: %s\n", value)
		case "locale":
			// Custom locale processing
			fmt.Printf("  -> Setting locale to: %s\n", value)
		default:
			fmt.Printf("  -> Unknown parameter: %s\n", parameter)
		}

		return nil
	}

	// Override payload callbacks with custom logic
	defaults.PayloadHandle = func(ctx context.Context, mimeType, name string, size uint64, metadata map[string]any, payload []byte) (statusCode int, message string, err error) {
		hash := sha256.Sum256(payload)
		fmt.Printf("[📦 PAYLOAD] %s: name=%s size=%d bytes (SHA256: %x)\n", mimeType, name, size, hash)

		// Log metadata if present
		if len(metadata) > 0 {
			fmt.Printf("  -> Metadata: %+v\n", metadata)
		}

		// Custom processing based on MIME type
		switch mimeType {
		case "application/json":
			fmt.Printf("  -> JSON payload detected\n")
		case "text/plain":
			fmt.Printf("  -> Text payload detected\n")
		case "application/octet-stream":
			fmt.Printf("  -> Binary payload detected\n")
		default:
			fmt.Printf("  -> Unknown payload type: %s\n", mimeType)
		}

		return 0, "Payload processed successfully", nil
	}

	return defaults
}
