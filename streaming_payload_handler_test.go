// SPDX-FileCopyrightText: (C) 2026 Dell Technologies, All Rights Reserved
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func newStreamingTestManager(destination, command string) *GenericHandlerManager {
	return &GenericHandlerManager{config: &HandlerConfig{Handlers: HandlerSection{Payload: PayloadHandlerConfig{
		TempDir:       os.TempDir(),
		DefaultAction: "reject",
		MimeTypes: map[string]PayloadMimeTypeHandler{
			"application/octet-stream": {
				Enabled:     true,
				Command:     command,
				Destination: destination,
			},
		},
	}}}}
}

func TestGenericChunkedPayloadHandlerStreamsToFile(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "payload.bin")
	commandMarker := filepath.Join(dir, "command-ran")
	handler := NewGenericChunkedPayloadHandler(newStreamingTestManager(destination, "touch "+commandMarker))

	if err := handler.BeginPayload("application/octet-stream", "payload.bin", 11, nil); err != nil {
		t.Fatalf("BeginPayload failed: %v", err)
	}
	if err := handler.ReceiveChunk([]byte("hello ")); err != nil {
		t.Fatalf("ReceiveChunk failed: %v", err)
	}
	if err := handler.ReceiveChunk([]byte("world")); err != nil {
		t.Fatalf("ReceiveChunk failed: %v", err)
	}
	status, _, err := handler.EndPayload()
	if err != nil || status != 0 {
		t.Fatalf("EndPayload failed: status=%d err=%v", status, err)
	}

	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("unexpected destination data %q", data)
	}
	if _, err := os.Stat(destination + ".partial"); !os.IsNotExist(err) {
		t.Fatalf("partial file still exists: %v", err)
	}
	if _, err := os.Stat(commandMarker); err != nil {
		t.Fatalf("post-transfer command did not run: %v", err)
	}
}

func TestGenericChunkedPayloadHandlerCancelRemovesPartial(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "payload.bin")
	handler := NewGenericChunkedPayloadHandler(newStreamingTestManager(destination, ""))

	if err := handler.BeginPayload("application/octet-stream", "payload.bin", 5, nil); err != nil {
		t.Fatalf("BeginPayload failed: %v", err)
	}
	if err := handler.ReceiveChunk([]byte("hello")); err != nil {
		t.Fatalf("ReceiveChunk failed: %v", err)
	}
	if err := handler.CancelPayload(); err != nil {
		t.Fatalf("CancelPayload failed: %v", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination exists after cancel: %v", err)
	}
	if _, err := os.Stat(destination + ".partial"); !os.IsNotExist(err) {
		t.Fatalf("partial exists after cancel: %v", err)
	}
}

func TestGenericChunkedPayloadHandlerBuffersWithoutDestination(t *testing.T) {
	manager := newStreamingTestManager("", "")
	handler := NewGenericChunkedPayloadHandler(manager)

	if err := handler.BeginPayload("application/octet-stream", "payload.bin", 5, nil); err != nil {
		t.Fatalf("BeginPayload failed: %v", err)
	}
	if err := handler.ReceiveChunk([]byte("hello")); err != nil {
		t.Fatalf("ReceiveChunk failed: %v", err)
	}
	status, _, err := handler.EndPayload()
	if err != nil || status != 0 {
		t.Fatalf("EndPayload failed: status=%d err=%v", status, err)
	}
}
