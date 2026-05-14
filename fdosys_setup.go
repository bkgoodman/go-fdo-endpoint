// SPDX-FileCopyrightText: (C) 2026 Dell Technologies, All Rights Reserved
// SPDX-License-Identifier: Apache-2.0
// Author: Brad Goodman

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/fido-device-onboard/go-fdo/fsim"
	"github.com/fido-device-onboard/go-fdo/serviceinfo"
)

// createFdoSysModule creates and configures the fdo_sys device module based
// on the application config.
func createFdoSysModule(cfg *Config) serviceinfo.DeviceModule {
	outputDir := cfg.FdoSys.OutputDir
	if outputDir == "" {
		outputDir = "/hzp"
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		slog.Warn("fdo_sys: could not create output dir", "dir", outputDir, "error", err)
	}

	return &fsim.FdoSys{
		OutputDir: outputDir,

		FetchHandler: func(name string) ([]byte, error) {
			return fdoSysFetchHandler(cfg, name)
		},

		FileHandler: func(name string, data []byte) error {
			return fdoSysFileHandler(cfg, name, data)
		},

		ExecHandler: func(args []string) (int, error) {
			// Log commands but do not execute them for safety
			slog.Info("fdo_sys: exec requested (not executed)", "args", args)
			fmt.Printf("[FDO_SYS EXEC] Command: %v (logged, not executed)\n", args)
			return 0, nil
		},
	}
}

// fdoSysFetchHandler handles fdo_sys fetch requests.
// The server sends "fetch" with a name like "normal_key.csr" to request a CSR.
func fdoSysFetchHandler(cfg *Config, name string) ([]byte, error) {
	slog.Info("fdo_sys: fetch handler called", "name", name)

	switch {
	case name == "normal_key.csr" || name == "csr":
		return generateCSR(cfg)
	default:
		// Try to read the file from the output directory
		outputDir := cfg.FdoSys.OutputDir
		if outputDir == "" {
			outputDir = "/hzp"
		}
		path := filepath.Join(outputDir, filepath.Base(name))
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("fdo_sys: fetch %q: file not found at %s: %w", name, path, err)
		}
		return data, nil
	}
}

// generateCSR generates a new EC P-384 key pair and CSR, saving the private
// key and returning the PEM-encoded CSR.
func generateCSR(cfg *Config) ([]byte, error) {
	outputDir := cfg.FdoSys.OutputDir
	if outputDir == "" {
		outputDir = "/hzp"
	}

	subjectCN := cfg.FdoSys.CSRSubjectCN
	if subjectCN == "" {
		subjectCN = "mtls.edge.internal.use.only"
	}

	// Check if we have an existing key
	keyPath := cfg.FdoSys.CSRKeyPath
	if keyPath == "" {
		keyPath = filepath.Join(outputDir, "normal_key")
	}

	var privKey *ecdsa.PrivateKey
	var err error

	// Try to load existing key
	if keyData, readErr := os.ReadFile(keyPath); readErr == nil {
		block, _ := pem.Decode(keyData)
		if block != nil {
			if key, parseErr := x509.ParseECPrivateKey(block.Bytes); parseErr == nil {
				privKey = key
				slog.Info("fdo_sys: using existing key", "path", keyPath)
			}
		}
	}

	// Generate new key if needed
	if privKey == nil {
		slog.Info("fdo_sys: generating new EC P-384 key pair")
		privKey, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("fdo_sys: error generating key: %w", err)
		}

		// Save private key
		keyDER, err := x509.MarshalECPrivateKey(privKey)
		if err != nil {
			return nil, fmt.Errorf("fdo_sys: error marshaling key: %w", err)
		}
		keyPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: keyDER,
		})

		// #nosec G306 -- private key needs restricted permissions
		if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
			return nil, fmt.Errorf("fdo_sys: error saving key to %s: %w", keyPath, err)
		}
		slog.Info("fdo_sys: saved private key", "path", keyPath)
	}

	// Create CSR
	template := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: subjectCN,
		},
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, template, privKey)
	if err != nil {
		return nil, fmt.Errorf("fdo_sys: error creating CSR: %w", err)
	}

	csrPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: csrDER,
	})

	// Also save CSR to disk for reference
	csrPath := filepath.Join(outputDir, "normal_csr")
	// #nosec G306 -- CSR is not sensitive
	if err := os.WriteFile(csrPath, csrPEM, 0644); err != nil {
		slog.Warn("fdo_sys: could not save CSR file", "path", csrPath, "error", err)
	}

	slog.Info("fdo_sys: generated CSR", "subject", subjectCN, "csrSize", len(csrPEM))
	fmt.Printf("[FDO_SYS] Generated CSR: CN=%s, key=%s, csr=%s (%d bytes)\n",
		subjectCN, keyPath, csrPath, len(csrPEM))

	// If SSHUsername is configured, wrap the CSR in a JSON envelope so the
	// owner service generates an SSH keypair and writes connection-info to
	// the secrets store.
	if cfg.FdoSys.SSHUsername != "" {
		envelope := struct {
			CSR         string `json:"csr"`
			SSHUsername string `json:"ssh_username"`
		}{
			CSR:         string(csrPEM),
			SSHUsername:  cfg.FdoSys.SSHUsername,
		}
		jsonBytes, err := json.Marshal(envelope)
		if err != nil {
			return nil, fmt.Errorf("fdo_sys: error marshaling CSR envelope: %w", err)
		}
		slog.Info("fdo_sys: wrapped CSR in JSON envelope", "ssh_username", cfg.FdoSys.SSHUsername, "envelopeSize", len(jsonBytes))
		fmt.Printf("[FDO_SYS] SSH credential provisioning enabled: ssh_username=%s\n", cfg.FdoSys.SSHUsername)
		return jsonBytes, nil
	}

	return csrPEM, nil
}

// fdoSysFileHandler handles files received via fdo_sys filedesc+write.
func fdoSysFileHandler(cfg *Config, name string, data []byte) error {
	outputDir := cfg.FdoSys.OutputDir
	if outputDir == "" {
		outputDir = "/hzp"
	}

	// Sanitize filename - only use base name to prevent path traversal
	safeName := filepath.Base(name)
	outPath := filepath.Join(outputDir, safeName)

	// Determine permissions based on file type
	perm := os.FileMode(0644)
	if safeName == "normal_key" || safeName == "device.key" || safeName == "authorized_keys" {
		perm = 0600
	}

	// Write file
	if err := os.WriteFile(outPath, data, perm); err != nil {
		return fmt.Errorf("fdo_sys: error writing %q to %s: %w", name, outPath, err)
	}

	fmt.Printf("[FDO_SYS FILE] Received: %s -> %s (%d bytes)\n", name, outPath, len(data))
	slog.Info("fdo_sys: file received and saved", "name", name, "path", outPath, "size", len(data))

	return nil
}
