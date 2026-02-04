// SPDX-FileCopyrightText: (C) 2024 Intel Corporation
// SPDX-License-Identifier: Apache 2.0

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// ModifierType represents the type of modifier
type ModifierType int

const (
	FilterModifier ModifierType = iota
	ValidatorModifier
)

// Modifier represents a single variable modifier
type Modifier struct {
	Type ModifierType
	Name string
	Func func(string) (string, error)
}

// ParsedVariable represents a variable with its modifiers
type ParsedVariable struct {
	Variable  string
	Modifiers []Modifier
}

// Built-in modifiers
var builtInModifiers = map[string]Modifier{
	// Filter modifiers
	"nospace": {FilterModifier, "nospace", func(s string) (string, error) {
		return strings.ReplaceAll(s, " ", ""), nil
	}},
	"underscore": {FilterModifier, "underscore", func(s string) (string, error) {
		return strings.ReplaceAll(s, " ", "_"), nil
	}},
	"dash": {FilterModifier, "dash", func(s string) (string, error) {
		return strings.ReplaceAll(s, " ", "-"), nil
	}},
	"lowercase": {FilterModifier, "lowercase", func(s string) (string, error) {
		return strings.ToLower(s), nil
	}},
	"uppercase": {FilterModifier, "uppercase", func(s string) (string, error) {
		return strings.ToUpper(s), nil
	}},
	"trim": {FilterModifier, "trim", func(s string) (string, error) {
		return strings.TrimSpace(s), nil
	}},
	"slugify": {FilterModifier, "slugify", func(s string) (string, error) {
		// Convert to lowercase, replace spaces with dashes, remove special chars
		s = strings.ToLower(s)
		s = strings.ReplaceAll(s, " ", "-")
		reg := regexp.MustCompile(`[^a-z0-9\-]`)
		return reg.ReplaceAllString(s, ""), nil
	}},

	// Validator modifiers
	"username": {ValidatorModifier, "username", func(s string) (string, error) {
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(s) {
			return "", fmt.Errorf("invalid username format: only alphanumeric, underscore, and hyphen allowed")
		}
		return s, nil
	}},
	"password": {ValidatorModifier, "password", func(s string) (string, error) {
		if strings.ContainsAny(s, "&|<>'$`") {
			return "", fmt.Errorf("password contains unsafe characters")
		}
		return s, nil
	}},
	"email": {ValidatorModifier, "email", func(s string) (string, error) {
		if !regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`).MatchString(s) {
			return "", fmt.Errorf("invalid email format")
		}
		return s, nil
	}},
	"hostname": {ValidatorModifier, "hostname", func(s string) (string, error) {
		if len(s) > 253 || len(s) == 0 {
			return "", fmt.Errorf("hostname length must be between 1 and 253 characters")
		}
		if strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") {
			return "", fmt.Errorf("hostname cannot start or end with hyphen")
		}
		if !regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`).MatchString(s) {
			return "", fmt.Errorf("invalid hostname format")
		}
		return s, nil
	}},
	"required": {ValidatorModifier, "required", func(s string) (string, error) {
		if strings.TrimSpace(s) == "" {
			return "", fmt.Errorf("value is required")
		}
		return s, nil
	}},
}

// ParseVariableSyntax parses a variable string with modifiers
// Syntax: {variable:modifier1:modifier2}
func ParseVariableSyntax(input string) (*ParsedVariable, error) {
	// Check if it's a variable with modifiers
	if !strings.HasPrefix(input, "{") || !strings.HasSuffix(input, "}") {
		return nil, fmt.Errorf("not a variable syntax")
	}

	content := strings.Trim(input, "{}")
	parts := strings.Split(content, ":")

	if len(parts) < 1 {
		return nil, fmt.Errorf("invalid variable syntax")
	}

	variable := parts[0]
	var modifiers []Modifier

	for i := 1; i < len(parts); i++ {
		modName := parts[i]
		modifier, exists := builtInModifiers[modName]
		if !exists {
			return nil, fmt.Errorf("unknown modifier: %s", modName)
		}
		modifiers = append(modifiers, modifier)
	}

	return &ParsedVariable{
		Variable:  variable,
		Modifiers: modifiers,
	}, nil
}

// ApplyModifiers applies all modifiers to a value
func ApplyModifiers(value string, modifiers []Modifier) (string, error) {
	currentValue := value

	for _, modifier := range modifiers {
		if modifier.Type == FilterModifier {
			// Filter modifiers change the value
			result, err := modifier.Func(currentValue)
			if err != nil {
				return "", fmt.Errorf("filter modifier %s failed: %w", modifier.Name, err)
			}
			currentValue = result
		} else if modifier.Type == ValidatorModifier {
			// Validator modifiers can reject the value
			_, err := modifier.Func(currentValue)
			if err != nil {
				return "", fmt.Errorf("validation failed: %w", err)
			}
		}
	}

	return currentValue, nil
}

// ProcessVariableWithModifiers processes a template variable with modifiers
func (ghm *GenericHandlerManager) ProcessVariableWithModifiers(input string, vars map[string]interface{}) (string, error) {
	// Check if it's a variable with modifiers
	if strings.HasPrefix(input, "{") && strings.HasSuffix(input, "}") {
		parsed, err := ParseVariableSyntax(input)
		if err != nil {
			// Not a valid variable syntax, treat as regular template
			return ghm.processTemplate(input, vars)
		}

		// Get the raw value from vars
		rawValue, exists := vars[parsed.Variable]
		if !exists {
			return "", fmt.Errorf("variable %s not found", parsed.Variable)
		}

		// Convert to string
		valueStr, ok := rawValue.(string)
		if !ok {
			return "", fmt.Errorf("variable %s is not a string", parsed.Variable)
		}

		// Apply modifiers
		finalValue, err := ApplyModifiers(valueStr, parsed.Modifiers)
		if err != nil {
			return "", err
		}

		return finalValue, nil
	}

	// Regular template processing
	return ghm.processTemplate(input, vars)
}

// processTemplate handles regular template processing without modifiers
func (ghm *GenericHandlerManager) processTemplate(input string, vars map[string]interface{}) (string, error) {
	// Convert {var} syntax to {{.var}} syntax for Go templates
	goTemplate := strings.ReplaceAll(input, "{value}", "{{.value}}")
	goTemplate = strings.ReplaceAll(goTemplate, "{filename}", "{{.filename}}")
	goTemplate = strings.ReplaceAll(goTemplate, "{mimetype}", "{{.mimetype}}")
	goTemplate = strings.ReplaceAll(goTemplate, "{size}", "{{.size}}")
	goTemplate = strings.ReplaceAll(goTemplate, "{parameter}", "{{.parameter}}")

	// Parse the command template
	tmpl, err := template.New("command").Parse(goTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse command template: %w", err)
	}

	// Execute the template
	var commandBuf strings.Builder
	if err := tmpl.Execute(&commandBuf, vars); err != nil {
		return "", fmt.Errorf("failed to execute command template: %w", err)
	}

	return commandBuf.String(), nil
}

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
	// Process the entire command template to handle variables with modifiers
	processedCommand, err := ghm.ProcessVariableWithModifiers(commandTemplate, vars)
	if err != nil {
		return fmt.Errorf("failed to process command template: %w", err)
	}

	// Show both the template and the resolved command
	fmt.Printf("[HANDLER] Template: %s\n", commandTemplate)
	fmt.Printf("[HANDLER] Resolved: %s\n", processedCommand)

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
