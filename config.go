// SPDX-FileCopyrightText: (C) 2026 Dell Technologies, All Rights Reserved
// SPDX-License-Identifier: Apache-2.0
// Author: Brad Goodman

package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	// Basic configuration
	BlobPath   string `yaml:"blob_path"`
	Debug      bool   `yaml:"debug"`
	FDOVersion int    `yaml:"fdo_version"`

	// Device Initialization (DI) configuration
	DI struct {
		URL    string `yaml:"url"`
		Key    string `yaml:"key"`
		KeyEnc string `yaml:"key_enc"`
	} `yaml:"di"`

	// Cryptographic configuration
	Crypto struct {
		CipherSuite string `yaml:"cipher_suite"`
		KexSuite    string `yaml:"kex_suite"`
	} `yaml:"crypto"`

	// Transport configuration
	Transport struct {
		TPMPath string `yaml:"tpm_path"`
	} `yaml:"transport"`

	// Operation configuration
	Operation struct {
		PrintDevice              bool `yaml:"print_device"`
		RVOnly                   bool `yaml:"rv_only"`
		IgnoreCredentialRotation bool `yaml:"ignore_credential_rotation"`
	} `yaml:"operation"`

	// Service Info Modules configuration
	ServiceInfo struct {
		DownloadDir  string   `yaml:"download_dir"`
		EchoCommands bool     `yaml:"echo_commands"`
		WgetDir      string   `yaml:"wget_dir"`
		UploadPaths  []string `yaml:"upload_paths"`
	} `yaml:"service_info"`
}

// DefaultConfig returns a configuration with default values
func DefaultConfig() *Config {
	return &Config{
		BlobPath:   "cred.bin",
		Debug:      false,
		FDOVersion: 101,
		DI: struct {
			URL    string `yaml:"url"`
			Key    string `yaml:"key"`
			KeyEnc string `yaml:"key_enc"`
		}{
			URL:    "",
			Key:    "ec384",
			KeyEnc: "x509",
		},
		Crypto: struct {
			CipherSuite string `yaml:"cipher_suite"`
			KexSuite    string `yaml:"kex_suite"`
		}{
			CipherSuite: "A128GCM",
			KexSuite:    "ECDH384",
		},
		Transport: struct {
			TPMPath string `yaml:"tpm_path"`
		}{
			TPMPath: "",
		},
		Operation: struct {
			PrintDevice              bool `yaml:"print_device"`
			RVOnly                   bool `yaml:"rv_only"`
			IgnoreCredentialRotation bool `yaml:"ignore_credential_rotation"`
		}{
			PrintDevice:              false,
			RVOnly:                   false,
			IgnoreCredentialRotation: false,
		},
		ServiceInfo: struct {
			DownloadDir  string   `yaml:"download_dir"`
			EchoCommands bool     `yaml:"echo_commands"`
			WgetDir      string   `yaml:"wget_dir"`
			UploadPaths  []string `yaml:"upload_paths"`
		}{
			DownloadDir:  "",
			EchoCommands: false,
			WgetDir:      "",
			UploadPaths:  []string{},
		},
	}
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(configPath string) (*Config, error) {
	config := DefaultConfig()

	if configPath == "" {
		configPath = "config.yaml"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Config file doesn't exist, return defaults
			return config, nil
		}
		return nil, fmt.Errorf("error reading config file %q: %w", configPath, err)
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("error parsing config file %q: %w", configPath, err)
	}

	return config, nil
}

// SaveConfig saves the configuration to a YAML file
func SaveConfig(config *Config, configPath string) error {
	if configPath == "" {
		configPath = "config.yaml"
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("error marshaling config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("error writing config file %q: %w", configPath, err)
	}

	return nil
}
