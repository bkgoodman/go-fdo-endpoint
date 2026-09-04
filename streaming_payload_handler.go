// SPDX-FileCopyrightText: (C) 2026 Dell Technologies, All Rights Reserved
// SPDX-License-Identifier: Apache-2.0
// Author: Brad Goodman

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fido-device-onboard/go-fdo/fsim"
)

type GenericChunkedPayloadHandler struct {
	manager     *GenericHandlerManager
	handler     PayloadMimeTypeHandler
	mimeType    string
	name        string
	size        uint64
	metadata    map[string]any
	buffer      bytes.Buffer
	file        *os.File
	destination string
	partial     string
	blockDevice bool
}

var _ fsim.ChunkedPayloadHandler = (*GenericChunkedPayloadHandler)(nil)

func NewGenericChunkedPayloadHandler(manager *GenericHandlerManager) *GenericChunkedPayloadHandler {
	return &GenericChunkedPayloadHandler{manager: manager}
}

func (h *GenericChunkedPayloadHandler) SupportsMimeType(mimeType string) bool {
	handler, ok := h.manager.config.Handlers.Payload.MimeTypes[mimeType]
	return ok && handler.Enabled || h.manager.config.Handlers.Payload.DefaultAction == "accept"
}

func (h *GenericChunkedPayloadHandler) BeginPayload(mimeType, name string, size uint64, metadata map[string]any) error {
	handler, ok := h.manager.config.Handlers.Payload.MimeTypes[mimeType]
	if (!ok || !handler.Enabled) && h.manager.config.Handlers.Payload.DefaultAction != "accept" {
		return fmt.Errorf("no enabled handler for MIME type %s", mimeType)
	}

	h.handler = handler
	h.mimeType = mimeType
	h.name = filepath.Base(name)
	h.size = size
	h.metadata = metadata
	h.buffer.Reset()
	h.destination = ""
	h.partial = ""
	h.blockDevice = false

	if handler.Destination == "" {
		return nil
	}

	destination, err := h.manager.ProcessVariableWithModifiers(handler.Destination, map[string]interface{}{
		"filename": handler.Destination,
		"mimetype": mimeType,
		"size":     size,
		"name":     h.name,
	})
	if err != nil {
		return fmt.Errorf("failed to process destination template: %w", err)
	}
	if !filepath.IsAbs(destination) {
		return fmt.Errorf("payload destination must be absolute")
	}

	h.destination = filepath.Clean(destination)
	info, statErr := os.Lstat(h.destination)
	if statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
			return fmt.Errorf("unsupported payload destination: %s", h.destination)
		}
		if info.Mode()&os.ModeDevice != 0 && info.Mode()&os.ModeCharDevice == 0 {
			h.blockDevice = true
			h.file, err = os.OpenFile(h.destination, os.O_WRONLY, 0)
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported payload destination: %s", h.destination)
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}

	if err := os.MkdirAll(filepath.Dir(h.destination), 0755); err != nil {
		return err
	}
	h.partial = h.destination + ".partial"
	h.file, err = os.OpenFile(h.partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	return err
}

func (h *GenericChunkedPayloadHandler) ReceiveChunk(data []byte) error {
	if h.handler.Destination == "" {
		_, err := h.buffer.Write(data)
		return err
	}
	if h.file == nil {
		return fmt.Errorf("payload destination is not open")
	}
	n, err := h.file.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return fmt.Errorf("short payload write: wrote %d of %d bytes", n, len(data))
	}
	return nil
}

func (h *GenericChunkedPayloadHandler) EndPayload() (int, string, error) {
	if h.handler.Destination == "" {
		return h.manager.HandlePayload(context.Background(), h.mimeType, h.name, h.size, h.metadata, h.buffer.Bytes())
	}

	if h.file == nil {
		return 2, "Payload destination is not open", fmt.Errorf("payload destination is not open")
	}
	if err := h.file.Sync(); err != nil {
		_ = h.CancelPayload()
		return 2, "Failed to sync payload destination", err
	}
	if err := h.file.Close(); err != nil {
		h.file = nil
		_ = h.CancelPayload()
		return 2, "Failed to close payload destination", err
	}
	h.file = nil

	if !h.blockDevice {
		if err := os.Rename(h.partial, h.destination); err != nil {
			_ = os.Remove(h.partial)
			return 2, "Failed to finalize payload destination", err
		}
	}

	if err := h.manager.executeCommandTemplate(h.handler.Command, map[string]interface{}{
		"filename": h.destination,
		"mimetype": h.mimeType,
		"size":     h.size,
		"name":     h.name,
	}); err != nil {
		return 2, fmt.Sprintf("Handler execution failed: %v", err), err
	}

	return 0, fmt.Sprintf("Payload streamed to %s", h.destination), nil
}

func (h *GenericChunkedPayloadHandler) CancelPayload() error {
	if h.file != nil {
		_ = h.file.Close()
		h.file = nil
	}
	if h.partial != "" {
		if err := os.Remove(h.partial); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	h.buffer.Reset()
	return nil
}
