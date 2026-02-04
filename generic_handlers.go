// SPDX-FileCopyrightText: (C) 2024 Intel Corporation
// SPDX-License-Identifier: Apache 2.0

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// HandlerConfig defines the structure for generic handlers
type HandlerConfig struct {
	Handlers HandlerSection `yaml:"handlers"`
}

// HandlerSection contains all handler configurations
type HandlerSection struct {
	SysConfig map[string]SysConfigHandler `yaml:"sysconfig"`
	Payload   PayloadHandlerConfig        `yaml:"payload"`
}

// SysConfigHandler defines a handler for sysconfig parameters
type SysConfigHandler struct {
	Command string `yaml:"command"`
	Enabled bool   `yaml:"enabled"`
}

// PayloadHandlerConfig defines payload handling configuration
type PayloadHandlerConfig struct {
	TempDir       string                            `yaml:"temp_dir"`
	DefaultAction string                            `yaml:"default_action"`
	MimeTypes     map[string]PayloadMimeTypeHandler `yaml:"mime_types"`
}

// PayloadMimeTypeHandler defines a handler for specific MIME types
type PayloadMimeTypeHandler struct {
	Enabled bool   `yaml:"enabled"`
	Command string `yaml:"command"`
}

// GenericHandlerManager manages all the generic handlers
type GenericHandlerManager struct {
	config *HandlerConfig
}

// NewGenericHandlerManager creates a new handler manager from config
func NewGenericHandlerManager(configPath string) (*GenericHandlerManager, error) {
	config := &HandlerConfig{}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Ensure temp directory exists
	if config.Handlers.Payload.TempDir != "" {
		if err := os.MkdirAll(config.Handlers.Payload.TempDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create temp directory: %w", err)
		}
	}

	return &GenericHandlerManager{config: config}, nil
}

// HandleSysConfigParameter handles a sysconfig parameter using the configured command
func (ghm *GenericHandlerManager) HandleSysConfigParameter(parameter, value string) error {
	handler, exists := ghm.config.Handlers.SysConfig[parameter]
	if !exists {
		return fmt.Errorf("no handler configured for sysconfig parameter: %s", parameter)
	}

	if !handler.Enabled {
		return fmt.Errorf("handler disabled for sysconfig parameter: %s", parameter)
	}

	// Execute the command template
	return ghm.executeCommandTemplate(handler.Command, map[string]interface{}{
		"parameter": parameter,
		"value":     value,
	})
}

// HandlePayload handles a payload using the configured handlers
func (ghm *GenericHandlerManager) HandlePayload(ctx context.Context, mimeType, name string, size uint64, metadata map[string]any, payload []byte) (statusCode int, message string, err error) {
	// Check if we have a handler for this MIME type
	mimeHandler, exists := ghm.config.Handlers.Payload.MimeTypes[mimeType]

	if !exists || !mimeHandler.Enabled {
		// Handle based on default action
		switch ghm.config.Handlers.Payload.DefaultAction {
		case "reject":
			return 1, fmt.Sprintf("Unsupported payload type: %s", mimeType), nil
		case "accept":
			return 0, "Payload accepted (no handler)", nil
		case "require_handler":
			return 1, fmt.Sprintf("No handler configured for MIME type: %s", mimeType), nil
		default:
			return 1, fmt.Sprintf("Unknown default action: %s", ghm.config.Handlers.Payload.DefaultAction), nil
		}
	}

	// Create temporary file for payload
	filename := name
	if filename == "" {
		filename = fmt.Sprintf("payload_%d", size)
	}

	tempFile := filepath.Join(ghm.config.Handlers.Payload.TempDir, filename)

	// Write payload to temporary file
	if err := os.WriteFile(tempFile, payload, 0644); err != nil {
		return 1, fmt.Sprintf("Failed to write payload to temp file: %v", err), err
	}

	// Execute the command template
	if err := ghm.executeCommandTemplate(mimeHandler.Command, map[string]interface{}{
		"filename": tempFile,
		"mimetype": mimeType,
		"size":     size,
		"name":     name,
	}); err != nil {
		return 1, fmt.Sprintf("Handler execution failed: %v", err), err
	}

	// Clean up temp file
	defer os.Remove(tempFile)

	return 0, "Payload processed successfully", nil
}

// executeCommandTemplate executes a command template with the provided variables
func (ghm *GenericHandlerManager) executeCommandTemplate(commandTemplate string, vars map[string]interface{}) error {
	// Convert {var} syntax to {{.var}} syntax for Go templates
	goTemplate := strings.ReplaceAll(commandTemplate, "{value}", "{{.value}}")
	goTemplate = strings.ReplaceAll(goTemplate, "{filename}", "{{.filename}}")
	goTemplate = strings.ReplaceAll(goTemplate, "{mimetype}", "{{.mimetype}}")
	goTemplate = strings.ReplaceAll(goTemplate, "{size}", "{{.size}}")
	goTemplate = strings.ReplaceAll(goTemplate, "{parameter}", "{{.parameter}}")

	// Parse the command template
	tmpl, err := template.New("command").Parse(goTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse command template: %w", err)
	}

	// Execute the template
	var commandBuf strings.Builder
	if err := tmpl.Execute(&commandBuf, vars); err != nil {
		return fmt.Errorf("failed to execute command template: %w", err)
	}

	command := commandBuf.String()

	// Show both the template and the resolved command
	fmt.Printf("[HANDLER] Template: %s\n", commandTemplate)
	fmt.Printf("[HANDLER] Resolved: %s\n", command)

	// For now, just echo the command (safe default)
	// To enable actual command execution, uncomment the following:
	/*
		cmd := exec.Command("sh", "-c", command)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("command execution failed: %w", err)
		}
	*/

	return nil
}

// GetConfiguredSysConfigParameters returns a list of all configured sysconfig parameters
func (ghm *GenericHandlerManager) GetConfiguredSysConfigParameters() []string {
	params := make([]string, 0, len(ghm.config.Handlers.SysConfig))
	for param := range ghm.config.Handlers.SysConfig {
		params = append(params, param)
	}
	return params
}

// GetConfiguredMimeTypes returns a list of all configured MIME types
func (ghm *GenericHandlerManager) GetConfiguredMimeTypes() []string {
	mimeTypes := make([]string, 0, len(ghm.config.Handlers.Payload.MimeTypes))
	for mimeType := range ghm.config.Handlers.Payload.MimeTypes {
		mimeTypes = append(mimeTypes, mimeType)
	}
	return mimeTypes
}

// ValidateConfig validates the handler configuration
func (ghm *GenericHandlerManager) ValidateConfig() error {
	// Check sysconfig handlers
	for param, handler := range ghm.config.Handlers.SysConfig {
		if handler.Enabled && handler.Command == "" {
			return fmt.Errorf("sysconfig handler '%s' is enabled but has no command", param)
		}
	}

	// Check payload handlers
	for mimeType, handler := range ghm.config.Handlers.Payload.MimeTypes {
		if handler.Enabled && handler.Command == "" {
			return fmt.Errorf("payload handler for MIME type '%s' is enabled but has no command", mimeType)
		}
	}

	// Check temp directory
	if ghm.config.Handlers.Payload.TempDir != "" {
		if !filepath.IsAbs(ghm.config.Handlers.Payload.TempDir) {
			return fmt.Errorf("payload temp_dir must be an absolute path")
		}
	}

	// Check default action
	validActions := map[string]bool{"reject": true, "accept": true, "require_handler": true}
	if !validActions[ghm.config.Handlers.Payload.DefaultAction] {
		return fmt.Errorf("invalid default_action '%s', must be one of: reject, accept, require_handler", ghm.config.Handlers.Payload.DefaultAction)
	}

	return nil
}
