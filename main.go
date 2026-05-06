// SPDX-FileCopyrightText: (C) 2026 Dell Technologies, All Rights Reserved
// SPDX-License-Identifier: Apache-2.0
// Author: Brad Goodman

package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/big"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/fido-device-onboard/go-fdo"
	"github.com/fido-device-onboard/go-fdo/cbor"
	"github.com/fido-device-onboard/go-fdo/cose"
	"github.com/fido-device-onboard/go-fdo/cred"
	"github.com/fido-device-onboard/go-fdo/custom"
	fdohttp "github.com/fido-device-onboard/go-fdo/http"
	"github.com/fido-device-onboard/go-fdo/kex"
	"github.com/fido-device-onboard/go-fdo/protocol"
	"github.com/fido-device-onboard/go-fdo/serviceinfo"
)

// Global configuration
var config *Config

// Global credential store - build tags select backend (blob, tpm, tpmsim)
var credStore cred.Store

func init() {
	// No flag initialization needed - using config file
}

// openCredStore opens the credential store using the configured path
func openCredStore() error {
	var err error
	credStore, err = cred.Open(config.BlobPath)
	if err != nil {
		return fmt.Errorf("opening credential store: %w", err)
	}
	return nil
}

// ensureCredStore lazily opens the credential store if not already open
func ensureCredStore() error {
	if credStore != nil {
		return nil
	}
	return openCredStore()
}

func main() {
	// Register the library's event handler to see FDO events
	RegisterFDOEventHandler()

	// Parse command line for config file path and special operation modes
	configPath := "fdoclient.cfg"
	var directTO2Addr string
	var diOnly bool
	var runDemo bool

	flag.StringVar(&configPath, "config", "fdoclient.cfg", "Path to configuration file")
	flag.StringVar(&directTO2Addr, "to2", "", "Skip RV and directly attempt TO2 at specified address")
	flag.BoolVar(&diOnly, "di", false, "Run only device initialization then stop")
	flag.BoolVar(&runDemo, "demo", false, "Run generic handler demo")
	flag.Parse()

	// If demo mode is requested, run demo and exit
	if runDemo {
		demoHandlers()
		return
	}

	// Load configuration
	var err error
	config, err = LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		return
	}

	if config.Debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	// Ensure credential store is closed on exit
	defer closeCredStore()

	ctx := context.Background()
	if err := runClient(ctx, directTO2Addr, diOnly); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runClient(ctx context.Context, directTO2Addr string, diOnly bool) error {
	// If DI-only mode is requested, perform DI and stop
	if diOnly {
		if config.DI.URL == "" {
			return fmt.Errorf("DI URL not configured in config file")
		}
		return performDI(ctx)
	}

	// If direct TO2 address is provided, skip RV and attempt TO2 directly
	if directTO2Addr != "" {
		return performDirectTO2(ctx, directTO2Addr)
	}

	// Read device credential using credential store
	if err := ensureCredStore(); err != nil {
		return err
	}
	dc, hmacSha256, hmacSha384, privateKey, err := credStore.Load()
	if err != nil {
		// If credentials not found, automatically run DI only (factory initialization)
		if strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), "file not found") || strings.Contains(err.Error(), "error reading") || strings.Contains(err.Error(), "DCTPM") {
			fmt.Printf("Credentials not found - running DI only (factory initialization)\n")
			if config.DI.URL == "" {
				return fmt.Errorf("credentials not found and DI URL not configured")
			}
			return performDI(ctx)
		} else {
			return err
		}
	}
	if config.Operation.PrintDevice {
		return nil
	}

	// Skip manual DI - credentials already exist
	// DI should only happen when credentials don't exist (auto) or with -di flag (explicit)

	// Try TO1+TO2
	kexCipherSuiteID, ok := kex.CipherSuiteByName(config.Crypto.CipherSuite)
	if !ok {
		return fmt.Errorf("invalid key exchange cipher suite: %s", config.Crypto.CipherSuite)
	}

	fmt.Printf("Starting device onboarding process...\n")
	newDC := transferOwnership(ctx, dc.RvInfo, fdo.TO2Config{
		Cred:       *dc,
		HmacSha256: hmacSha256,
		HmacSha384: hmacSha384,
		Key:        privateKey,
		Devmod: serviceinfo.Devmod{
			Os:      runtime.GOOS,
			Arch:    runtime.GOARCH,
			Version: "Stub Client",
			Device:  "go-fdo-stub",
			FileSep: ";",
			Bin:     "/bin",
		},
		KeyExchange:                kex.Suite(config.Crypto.KexSuite),
		CipherSuite:                kexCipherSuiteID,
		AllowCredentialReuse:       true,
		IgnoreCredentialReplacement: config.Operation.IgnoreCredentialRotation,
	})

	// Flush all pending events before exit to ensure event handlers complete
	fdo.FlushEvents()

	if config.Operation.RVOnly {
		return nil
	}

	if newDC == nil {
		fmt.Println("Credential not updated (either due to failure of TO2 or the Credential Reuse Protocol)")
		return nil
	}

	// Store new credential
	fmt.Println("Success")
	return credStore.Save(*newDC)
}

func performDI(ctx context.Context) error {
	// Open credential store and generate keys using TPM or blob backend
	if err := ensureCredStore(); err != nil {
		return err
	}

	var keyType protocol.KeyType
	switch config.DI.Key {
	case "ec256":
		keyType = protocol.Secp256r1KeyType
	case "ec384":
		keyType = protocol.Secp384r1KeyType
	case "rsa2048":
		keyType = protocol.Rsa2048RestrKeyType
	case "rsa3072":
		keyType = protocol.RsaPkcsKeyType
	default:
		return fmt.Errorf("unknown key type: %s", config.DI.Key)
	}

	hmacSha256, hmacSha384, key, err := credStore.NewDI(keyType)
	if err != nil {
		return fmt.Errorf("error generating device keys: %w", err)
	}

	// Generate CSR
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "device.go-fdo-stub"},
	}, key)
	if err != nil {
		return fmt.Errorf("error creating CSR: %w", err)
	}
	_, err = x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return fmt.Errorf("error parsing CSR: %w", err)
	}

	// Parse the CSR to get the actual certificate request object
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return fmt.Errorf("error parsing CSR for CertInfo: %w", err)
	}

	// Generate serial number
	sn, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		return fmt.Errorf("error generating serial number: %w", err)
	}

	var keyEncoding protocol.KeyEncoding
	switch strings.ToLower(config.DI.KeyEnc) {
	case "x509":
		keyEncoding = protocol.X509KeyEnc
	case "x5chain":
		keyEncoding = protocol.X5ChainKeyEnc
	case "cose":
		keyEncoding = protocol.CoseKeyEnc
	default:
		return fmt.Errorf("unsupported key encoding: %s", config.DI.KeyEnc)
	}

	// Call DI server
	transport := tlsTransportWithVersion(config.DI.URL, nil, protocol.Version(config.FDOVersion))
	fmt.Printf("Initializing device with manufacturer...\n")

	// Ensure context has the correct protocol version
	ctx = protocol.ContextWithVersion(ctx, protocol.Version(config.FDOVersion))

	cred, err := fdo.DI(ctx, transport, custom.DeviceMfgInfo{
		KeyType:      keyType,
		KeyEncoding:  keyEncoding,
		SerialNumber: strconv.FormatInt(sn.Int64(), 10),
		DeviceInfo:   "Generic FDO Device",
		CertInfo:     cbor.X509CertificateRequest(*csr),
	}, fdo.DIConfig{
		HmacSha256: hmacSha256,
		HmacSha384: hmacSha384,
		Key:        key,
	})
	if err != nil {
		return err
	}

	if cred != nil {
		fmt.Printf("Device initialization completed successfully\n")
	}

	// Save credential using the store (TPM or blob)
	return credStore.Save(*cred)
}

func performDirectTO2(ctx context.Context, to2Addr string) error {
	// Read device credential using credential store
	if err := ensureCredStore(); err != nil {
		return err
	}
	dc, hmacSha256, hmacSha384, privateKey, err := credStore.Load()
	if err != nil {
		return fmt.Errorf("failed to read credentials: %w", err)
	}

	// Configure TO2
	kexCipherSuiteID, ok := kex.CipherSuiteByName(config.Crypto.CipherSuite)
	if !ok {
		return fmt.Errorf("invalid key exchange cipher suite: %s", config.Crypto.CipherSuite)
	}

	conf := fdo.TO2Config{
		Cred:       *dc,
		HmacSha256: hmacSha256,
		HmacSha384: hmacSha384,
		Key:        privateKey,
		Devmod: serviceinfo.Devmod{
			Os:      runtime.GOOS,
			Arch:    runtime.GOARCH,
			Version: "Stub Client",
			Device:  "go-fdo-stub",
			FileSep: ";",
			Bin:     "/bin",
		},
		KeyExchange:                kex.Suite(config.Crypto.KexSuite),
		CipherSuite:                kexCipherSuiteID,
		AllowCredentialReuse:       true,
		IgnoreCredentialReplacement: config.Operation.IgnoreCredentialRotation,
	}

	// Attempt TO2 directly at the specified address
	version := protocol.Version(config.FDOVersion)
	transport := tlsTransportWithVersion(to2Addr, nil, version)
	newDC := performTO2(ctx, transport, nil, conf)

	if newDC == nil {
		// Check if this is successful credential reuse or a failure
		if uintptr(unsafe.Pointer(newDC)) == 1 {
			// This is a failure case
			return fmt.Errorf("TO2 failed at address %s", to2Addr)
		}
		// Credential reuse - this is success, not failure
		fmt.Println("✅ TO2 completed successfully with credential reuse")
		return nil
	}

	// Store new credential
	if uintptr(unsafe.Pointer(newDC)) == 1 {
		// This is a failure case
		return fmt.Errorf("TO2 failed at address %s", to2Addr)
	}
	fmt.Println("✅ TO2 completed successfully with new credential")
	return credStore.Save(*newDC)
}

func transferOwnership(ctx context.Context, rvInfo [][]protocol.RvInstruction, conf fdo.TO2Config) *fdo.DeviceCredential {
	var to2URLs []string
	directives := protocol.ParseDeviceRvInfo(rvInfo)

	// Collect TO2 URLs from directives
	for _, directive := range directives {
		if directive.Bypass {
			for _, url := range directive.URLs {
				to2URLs = append(to2URLs, url.String())
			}
		}
	}

	// Try TO1
	var to1d *cose.Sign1[protocol.To1d, []byte]
	for _, directive := range directives {
		if directive.Bypass {
			continue
		}

		for _, url := range directive.URLs {
			var err error
			to1d, err = fdo.TO1(ctx, tlsTransport(url.String(), nil), conf.Cred, conf.Key, nil)
			if err != nil {
				slog.Error("TO1 failed", "base URL", url.String(), "error", err)
				continue
			}
			break
		}

		if directive.Delay != 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(directive.Delay):
			}
		}
	}

	// Extract TO2 URLs from TO1D response
	if to1d != nil {
		for _, to2Addr := range to1d.Payload.Val.RV {
			var host string
			switch {
			case to2Addr.DNSAddress != nil:
				host = *to2Addr.DNSAddress
			case to2Addr.IPAddress != nil:
				host = to2Addr.IPAddress.String()
			default:
				continue
			}

			var scheme, port string
			switch to2Addr.TransportProtocol {
			case protocol.HTTPTransport:
				scheme, port = "http://", "80"
			case protocol.HTTPSTransport:
				scheme, port = "https://", "443"
			default:
				continue
			}
			if to2Addr.Port != 0 {
				port = strconv.Itoa(int(to2Addr.Port))
			}

			to2URLs = append(to2URLs, scheme+net.JoinHostPort(host, port))
		}
	}

	// Print TO2 addrs if RV-only
	if config.Operation.RVOnly {
		if to1d != nil {
			fmt.Printf("TO1 Blob: %+v\n", to1d.Payload.Val)
		}
		return nil
	}

	// Try TO2 on each address only once
	for i, baseURL := range to2URLs {
		fmt.Printf("Attempting TO2 with server %d: %s\n", i+1, baseURL)
		// Use version-aware transport for TO2
		version := protocol.Version(config.FDOVersion)
		transport := tlsTransportWithVersion(baseURL, nil, version)
		newDC := performTO2(ctx, transport, to1d, conf)
		if newDC != nil {
			return newDC
		}
	}

	return nil
}

func performTO2(ctx context.Context, transport fdo.Transport, to1d *cose.Sign1[protocol.To1d, []byte], conf fdo.TO2Config) *fdo.DeviceCredential {
	// Ensure context has the correct protocol version
	ctx = protocol.ContextWithVersion(ctx, protocol.Version(config.FDOVersion))

	// Try to load generic handler configuration
	handlerManager, handlerErr := ValidateAndPrintHandlers("config_generic.yaml")
	if handlerErr != nil {
		fmt.Printf("[WARNING] Failed to load generic handlers: %v\n", handlerErr)
		fmt.Printf("[INFO] Falling back to default FSIM callbacks\n")

		// Use default callbacks
		callbacks := CreateCustomFSIMCallbacks()
		conf.DeviceModules = CreateFSIMModules(callbacks)
	} else {
		// Use generic handlers
		conf.DeviceModules = CreateGenericFSIMModules(handlerManager)
	}

	// Wrap modules to add debug logging for transitions
	wrappedFsims := make(map[string]serviceinfo.DeviceModule)
	for name, module := range conf.DeviceModules {
		wrappedFsims[name] = &debugModuleWrapper{
			DeviceModule: module,
			name:         name,
		}
	}

	conf.DeviceModules = wrappedFsims

	// Call version-specific TO2 function
	var cred *fdo.DeviceCredential
	var err error

	if config.FDOVersion == 200 {
		cred, err = fdo.TO2v200(ctx, transport, to1d, &conf)
	} else {
		cred, err = fdo.TO2(ctx, transport, to1d, conf)
	}
	if err != nil {
		slog.Error("TO2 failed", "error", err)
		fmt.Printf("TO2 failed with error: %v\n", err)
		// Return a special value to indicate failure vs successful credential reuse
		return (*fdo.DeviceCredential)(unsafe.Pointer(uintptr(1)))
	}

	// Handle credential reuse case
	if cred == nil {
		fmt.Printf("✅ TO2 Completed - Credential Reuse: true\n")
		return nil
	}

	fmt.Printf("✅ TO2 Completed - Credential Reuse: false\n")
	return cred
}

// debugModuleWrapper wraps a DeviceModule to add debug logging
type debugModuleWrapper struct {
	serviceinfo.DeviceModule
	name string
}

func (d *debugModuleWrapper) Transition(active bool) error {
	return d.DeviceModule.Transition(active)
}

func (d *debugModuleWrapper) Receive(ctx context.Context, messageName string, messageBody io.Reader, respond func(string) io.Writer, yield func()) error {
	return d.DeviceModule.Receive(ctx, messageName, messageBody, respond, yield)
}

func (d *debugModuleWrapper) Yield(ctx context.Context, respond func(string) io.Writer, yield func()) error {
	return d.DeviceModule.Yield(ctx, respond, yield)
}

// closeCredStore closes the credential store if open
func closeCredStore() error {
	if credStore != nil {
		return credStore.Close()
	}
	return nil
}

func tlsTransport(url string, tlsConfig interface{}) fdo.Transport {
	return tlsTransportWithVersion(url, tlsConfig, protocol.Version101)
}

func tlsTransportWithVersion(url string, tlsConfig interface{}, version protocol.Version) fdo.Transport {
	var tlsConf *tls.Config
	if tlsConfig == nil {
		// FDO provides its own security attestation, so TLS certificate verification
		// is not required. Always skip TLS verification for FDO operations.
		tlsConf = &tls.Config{
			InsecureSkipVerify: true,
		}
	} else {
		tlsConf = tlsConfig.(*tls.Config)
	}

	return &fdohttp.Transport{
		BaseURL:    url,
		FdoVersion: version,
		Client: &http.Client{Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			TLSClientConfig:       tlsConf,
		}},
	}
}
