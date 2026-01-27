# FDO Client Stub Application

A stub Go application that demonstrates the usage of the [go-fdo](https://github.com/bkgoodman/go-fdo) library for FIDO Device Onboard (FDO) client functionality.

## Overview

This application provides a basic skeleton for an FDO client that can:
- Perform Device Initialization (DI) with a manufacturer server
- Execute TO1 and TO2 protocols for ownership transfer
- Support various cryptographic algorithms and transport configurations

## Project Structure

```
.
├── main.go              # Main client application
├── go.mod              # Go module definition
├── go-fdo/             # go-fdo library as git submodule
├── README.md           # This file
└── fdo-client          # Compiled binary (after build)
```

## Setup

### Prerequisites

- Go 1.21 or later
- Git

### Installation

1. Clone this repository:
```bash
git clone <repository-url>
cd windsurf-project-4
```

2. Initialize the git submodule:
```bash
git submodule update --init --recursive
```

3. Build the application:
```bash
go build -o fdo-client .
```

## Usage

The application supports various command-line flags to configure its behavior:

### Basic Flags

- `-blob string`: File path of device credential blob (default: "cred.bin")
- `-debug`: Print HTTP contents for debugging
- `-di string`: HTTP base URL for DI server
- `-di-key string`: Key type for device credential (options: ec256, ec384, rsa2048, rsa3072; default: "ec384")
- `-di-key-enc string`: Public key encoding (options: x509, x5chain, cose; default: "x509")
- `-cipher string`: Cipher suite for encryption (default: "A128GCM")
- `-kex string`: Key exchange suite (default: "ECDH384")
- `-fdo-version int`: FDO protocol version (101 or 200; default: 101)

### Transport Flags

- `-insecure-tls`: Skip TLS certificate verification
- `-tpm string`: Use a TPM at the specified path for device credential secrets

### Operation Flags

- `-print`: Print device credential blob and stop
- `-rv-only`: Perform TO1 then stop (rendezvous-only mode)

### Service Info Module Flags

- `-download string`: Directory to download files into (enables FSIM download module)
- `-echo-commands`: Echo all commands received to stdout (enables FSIM command module)
- `-wget-dir string`: Directory to wget files into (enables FSIM wget module)
- `-upload string`: List of directories and files to upload from (enables FSIM upload module)

### Examples

#### Device Initialization (DI)

```bash
# Initialize with an EC384 key
./fdo-client -di https://manufacturer.example.com -di-key ec384

# Initialize with RSA2048 key and COSE key encoding
./fdo-client -di https://manufacturer.example.com -di-key rsa2048 -di-key-enc cose
```

#### Ownership Transfer (TO1/TO2)

```bash
# Perform full ownership transfer using existing credential
./fdo-client

# Perform TO1 only (rendezvous-only mode)
./fdo-client -rv-only

# Print credential information
./fdo-client -print
```

#### With Service Info Modules

```bash
# Enable file download capability
./fdo-client -download /tmp/downloads

# Enable command echo capability
./fdo-client -echo-commands

# Enable file upload capability
./fdo-client -upload /path/to/files,/another/path
```

## Implementation Notes

This is a **stub application** that demonstrates the structure and API usage of the go-fdo library. The following functions are implemented as stubs and would need to be completed for a production-ready client:

- `readCred()`: Reads device credential from storage
- `updateCred()`: Updates device credential after successful ownership transfer
- `saveCred()`: Saves new device credential after DI

These functions currently return "not implemented" errors and would need to be implemented based on your specific storage requirements (file system, TPM, secure element, etc.).

## Dependencies

- [go-fdo](https://github.com/bkgoodman/go-fdo): Main FDO library (included as git submodule)
- Standard Go library packages for cryptography, networking, and HTTP transport

## License

This project follows the same license as the go-fdo library: Apache License 2.0

## Contributing

This is a demonstration/stub application. For production use, you would need to:

1. Implement the credential storage/retrieval functions
2. Add proper error handling and logging
3. Implement security best practices for credential management
4. Add comprehensive testing
5. Consider adding configuration file support

## References

- [FIDO Device Onboard Protocol Specification](https://fidoalliance.org/specs/fdo/)
- [go-fdo Library Documentation](https://github.com/bkgoodman/go-fdo)
- [FIDO Alliance](https://fidoalliance.org/)
