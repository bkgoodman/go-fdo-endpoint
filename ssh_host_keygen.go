package main

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// GenerateSSHHostKeys generates ed25519, ecdsa (P-256), and rsa (3072-bit) SSH
// host key pairs and writes them to the given directory in standard OpenSSH
// format (private key + ".pub" public key), matching what sshd expects at
// /etc/ssh/ssh_host_*_key. This lets us create the device's permanent SSH
// host identity during FDO onboarding (in the installer environment, which
// has no ssh-keygen binary available) instead of letting cloud-init generate
// throwaway keys on first boot.
func GenerateSSHHostKeys(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}

	comment := "fdo-device-host-key"

	if err := generateEd25519HostKey(dir, comment); err != nil {
		return fmt.Errorf("ed25519: %w", err)
	}
	if err := generateECDSAHostKey(dir, comment); err != nil {
		return fmt.Errorf("ecdsa: %w", err)
	}
	if err := generateRSAHostKey(dir, comment); err != nil {
		return fmt.Errorf("rsa: %w", err)
	}

	return nil
}

func writeHostKeyPair(dir, name string, signer ssh.Signer, priv any, comment string) error {
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return fmt.Errorf("marshal private key: %w", err)
	}
	privPath := filepath.Join(dir, name)
	if err := os.WriteFile(privPath, pem.EncodeToMemory(block), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", privPath, err)
	}

	pubLine := ssh.MarshalAuthorizedKey(signer.PublicKey())
	pubPath := privPath + ".pub"
	pubData := append([]byte{}, pubLine...)
	pubData = append(pubData[:len(pubData)-1], []byte(" "+comment+"\n")...)
	if err := os.WriteFile(pubPath, pubData, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", pubPath, err)
	}

	fmt.Printf("[SSH-HOST-KEYGEN] Generated %s / %s.pub\n", privPath, privPath)
	return nil
}

func generateEd25519HostKey(dir, comment string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return err
	}
	_ = pub
	return writeHostKeyPair(dir, "ssh_host_ed25519_key", signer, priv, comment)
}

func generateECDSAHostKey(dir, comment string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return err
	}
	return writeHostKeyPair(dir, "ssh_host_ecdsa_key", signer, priv, comment)
}

func generateRSAHostKey(dir, comment string) error {
	priv, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return err
	}
	return writeHostKeyPair(dir, "ssh_host_rsa_key", signer, priv, comment)
}
