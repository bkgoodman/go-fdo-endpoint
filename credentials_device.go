package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fido-device-onboard/go-fdo/fsim"
)

// deviceInfoPayload bundles everything we want to convey back to the owner
// during TO2 onboarding into a single Registered Credentials round trip:
// the device's permanent SSH host public keys (generated during onboarding,
// see ssh_host_keygen.go) and its current IP address (convenience only, not
// security-relevant - identity is established via the SSH host keys).
type deviceInfoPayload struct {
	SSHHostKeyEd25519 string `json:"ssh_host_ed25519_key,omitempty"`
	SSHHostKeyECDSA   string `json:"ssh_host_ecdsa_key,omitempty"`
	SSHHostKeyRSA     string `json:"ssh_host_rsa_key,omitempty"`
	IPAddress         string `json:"ip_address,omitempty"`
}

func readTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// RegisterCredentialsDevice creates and registers a credentials device module
// for sending the device's SSH host public keys + IP address to the onboarding
// service via the Registered Credentials flow (fdo.credentials FSIM).
//
// All four values are bundled into a single JSON blob under one credential ID
// ("device_info") rather than four separate pubkey-request/response round
// trips. This keeps onboarding to one exchange instead of chaining multiple
// requests through the same TO2 ServiceInfo loop.
func RegisterCredentialsDevice(sshKeyDir string) *fsim.CredentialsDevice {
	if sshKeyDir == "" {
		sshKeyDir = "/run/fdo/ssh-host-keys"
	}

	// Create credentials device with callback for public key requests
	credentialsDevice := fsim.NewCredentialsDevice(
		// OnCredentialReceived - handle credentials from owner (server→device)
		func(credentialID string, credentialType int, data []byte, metadata map[string]any) error {
			fmt.Printf("[CREDENTIALS] Received credential: ID=%s, Type=%d, Size=%d\n", credentialID, credentialType, len(data))
			// We don't currently expect the owner to provision any credentials to
			// the device; this exists to satisfy the interface / for future use.
			return nil
		},
	)

	// Set the public key request callback (device→server). Note: the reference
	// server (examples/cmd/server.go) always types "Registered Credentials"
	// requests as CredentialTypeSSHPublicKey regardless of the -request-pubkey
	// "type" argument; we reuse this same generic byte-transfer mechanism to
	// also convey the device's IP address.
	credentialsDevice.OnPublicKeyRequested = func(credentialID string, credentialType int, metadata map[string]any) ([]byte, error) {
		fmt.Printf("[CREDENTIALS] Owner requesting device info: ID=%s, Type=%d\n", credentialID, credentialType)

		if credentialID != "device_info" {
			return nil, fmt.Errorf("unknown credential ID: %s", credentialID)
		}

		info := deviceInfoPayload{
			SSHHostKeyEd25519: readTrimmed(filepath.Join(sshKeyDir, "ssh_host_ed25519_key.pub")),
			SSHHostKeyECDSA:   readTrimmed(filepath.Join(sshKeyDir, "ssh_host_ecdsa_key.pub")),
			SSHHostKeyRSA:     readTrimmed(filepath.Join(sshKeyDir, "ssh_host_rsa_key.pub")),
			IPAddress:         readTrimmed("/run/fdo/device-ip.txt"),
		}

		data, err := json.Marshal(info)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal device info: %w", err)
		}

		fmt.Printf("[CREDENTIALS] Sending device info (%d bytes): %s\n", len(data), string(data))

		return data, nil
	}

	fmt.Printf("[CREDENTIALS] Registered credentials device with SSH key dir: %s\n", sshKeyDir)
	return credentialsDevice
}
