# FSIM Credentialing Handler Design - Detailed Implementation

## Overview

This design provides a comprehensive handler system for the FDO `fdo.credentials` FSIM module, supporting the three distinct protocol flows defined in the specification:

1. **Provisioned Credentials** - Owner → Device (shared secrets)
2. **Enrolled Credentials** - Device ↔ Owner (CSR/certificate flows)  
3. **Registered Credentials** - Device → Owner (public key registration)

## Real FSIM Message Flows

### Provisioned Credentials (Owner → Device)
```
Owner → Device: fdo.credentials:credential-begin (metadata)
Owner → Device: fdo.credentials:credential-data-0..N (chunked credential data)
Owner → Device: fdo.credentials:credential-end (completion)
Device → Owner: fdo.credentials:credential-result (acknowledgment)
```

### Enrolled Credentials (Device ↔ Owner)
```
Device → Owner: fdo.credentials:request-begin (metadata)
Device → Owner: fdo.credentials:request-data-0..N (chunked CSR/JWK)
Device → Owner: fdo.credentials:request-end (completion)
Owner → Device: fdo.credentials:response-begin (metadata)
Owner → Device: fdo.credentials:response-data-0..N (chunked cert/config)
Owner → Device: fdo.credentials:response-end (completion)
Device → Owner: fdo.credentials:response-result (acknowledgment)
```

### Registered Credentials (Device → Owner)
```
Owner → Device: fdo.credentials:pubkey-request (request for key)
Device → Owner: fdo.credentials:pubkey-begin (metadata)
Device → Owner: fdo.credentials:pubkey-data-0..N (chunked public key)
Device → Owner: fdo.credentials:pubkey-end (completion)
Owner → Device: fdo.credentials:pubkey-result (registration result)
```

## Enhanced Handler Configuration

```yaml
# Enhanced credentialing configuration
credentialing:
  # Temporary directory for credential files
  temp_dir: "/tmp/fdo_credentials"
  
  # Certificate authority configuration
  ca:
    base_url: "https://ca.example.com/api/v1"
    timeout: "30s"
    retry_count: 3
    
  # Default key settings
  keys:
    default_algorithm: "ecdsa"
    default_curve: "P-256"
    default_rsa_bits: 2048
    
  # Handler definitions for different credential types
  handlers:
    # === PROVISIONED CREDENTIALS (Owner → Device) ===
    provisioned:
      # Username/Password credentials
      password:
        enabled: true
        type: "password"
        # Multiple commands for complete user setup
        commands:
          - name: "create_user"
            command: "useradd -m {username}"
            variables: ["username"]
            validation:
              username_pattern: "^[a-z][a-z0-9_-]*$"
              username_max_length: 32
              
          - name: "set_password"
            command: "echo '{password}' | passwd --stdin {username}"
            variables: ["username", "password"]
            security:
              no_log: true  # Don't log passwords
              encrypt: true
              
          - name: "create_home_dir"
            command: "mkdir -p /home/{username}/.ssh"
            variables: ["username"]
            
          - name: "set_permissions"
            command: "chown -R {username}:{username} /home/{username}"
            variables: ["username"]
            
        # Post-processing commands
        post_commands:
          - name: "verify_user"
            command: "id {username}"
            variables: ["username"]
            
      # API Key credentials
      api_key:
        enabled: true
        type: "api_key"
        commands:
          - name: "store_api_key"
            command: "echo '{api_key}' > /etc/api/{service_name}.key"
            variables: ["api_key", "service_name"]
            security:
              file_permissions: "600"
              
          - name: "set_permissions"
            command: "chmod 600 /etc/api/{service_name}.key"
            variables: ["service_name"]
            
        post_commands:
          - name: "verify_key"
            command: "test -f /etc/api/{service_name}.key"
            variables: ["service_name"]
            
      # OAuth2 Client Credentials
      oauth2_client_secret:
        enabled: true
        type: "oauth2_client_secret"
        commands:
          - name: "create_config_dir"
            command: "mkdir -p /etc/oauth2"
            
          - name: "store_client_credentials"
            command: |
              cat > /etc/oauth2/{client_id}.json << EOF
              {
                "client_id": "{client_id}",
                "client_secret": "{client_secret}",
                "token_endpoint": "{token_endpoint}",
                "scope": "{scope}"
              }
              EOF
            variables: ["client_id", "client_secret", "token_endpoint", "scope"]
            security:
              file_permissions: "600"
              no_log: true  # Don't log client_secret
              
      # Bearer Token
      bearer_token:
        enabled: true
        type: "bearer_token"
        commands:
          - name: "store_token"
            command: "echo '{token}' > /etc/tokens/{service_name}.token"
            variables: ["token", "service_name"]
            security:
              file_permissions: "600"
              
          - name: "set_expiry"
            command: "echo '{expires_at}' > /etc/tokens/{service_name}.expiry"
            variables: ["expires_at", "service_name"]
            
    # === ENROLLED CREDENTIALS (Device ↔ Owner) ===
    enrolled:
      # X.509 Certificate Enrollment (CSR flow)
      x509_cert:
        enabled: true
        type: "x509_cert"
        # Device-side: Generate CSR
        device_commands:
          - name: "generate_keypair"
            command: |
              openssl genpkey -algorithm {key_algorithm} \
                {key_algorithm_params} \
                -out {key_file}
            variables: ["key_algorithm", "key_algorithm_params", "key_file"]
            output: "key_file"
            cleanup: false  # Keep key for next phase
            
          - name: "generate_csr"
            command: |
              openssl req -new -key {key_file} -out {csr_file} \
                -subj "/C={country}/ST={state}/L={locality}/O={organization}/CN={common_name}"
            input: "key_file"
            output: "csr_file"
            variables: ["country", "state", "locality", "organization", "common_name"]
            
          - name: "prepare_csr_for_transfer"
            command: |
              # Convert CSR to PEM for transfer
              openssl req -in {csr_file} -outform PEM -out {csr_pem_file}
            input: "csr_file"
            output: "csr_pem_file"
            
        # Owner-side: Process CSR and return certificate
        owner_processing:
          - name: "validate_csr"
            command: |
              openssl req -in {csr_file} -noout -verify
            input: "csr_file"
            
          - name: "sign_certificate"
            command: |
              openssl ca -in {csr_file} -out {cert_file} \
                -config {ca_config} -days {validity_days}
            input: "csr_file"
            output: "cert_file"
            variables: ["ca_config", "validity_days"]
            
          - name: "create_ca_bundle"
            command: |
              cat {intermediate_ca} {root_ca} > {ca_bundle_file}
            output: "ca_bundle_file"
            variables: ["intermediate_ca", "root_ca"]
            
        # Device-side: Install certificate
        device_post_commands:
          - name: "install_certificate"
            command: |
              cp {cert_file} /etc/ssl/certs/{cert_name}.crt
              update-ca-certificates
            input: "cert_file"
            variables: ["cert_name"]
            
          - name: "install_ca_bundle"
            command: |
              cp {ca_bundle_file} /etc/ssl/certs/{cert_name}-ca-bundle.crt
              update-ca-certificates
            input: "ca_bundle_file"
            variables: ["cert_name"]
            
          - name: "verify_installation"
            command: |
              openssl verify -CAfile /etc/ssl/certs/{cert_name}-ca-bundle.crt \
                /etc/ssl/certs/{cert_name}.crt
            variables: ["cert_name"]
            
        # Certificate settings
        cert_settings:
          algorithm: "ecdsa"
          curve: "P-256"
          validity_days: 365
          key_usage: ["digitalSignature", "keyEncipherment"]
          extended_key_usage: ["clientAuth", "serverAuth"]
          country: "US"
          state: "California"
          locality: "San Francisco"
          organization: "Example Corp"
          
      # OAuth2 Private Key JWT
      oauth2_private_key_jwt:
        enabled: true
        type: "oauth2_private_key_jwt"
        device_commands:
          - name: "generate_keypair"
            command: |
              openssl genpkey -algorithm {key_algorithm} \
                {key_algorithm_params} \
                -out {key_file}
            output: "key_file"
            
          - name: "extract_public_key_jwk"
            command: |
              openssl pkey -in {key_file} -pubout -out {pubkey_file}
              # Convert to JWK format (requires additional tool)
              pkey-to-jwk {pubkey_file} > {jwk_file}
            input: "key_file"
            output: "jwk_file"
            
        owner_processing:
          - name: "register_public_key"
            command: |
              curl -X POST \
                -H "Content-Type: application/json" \
                -d @'{jwk_file}' \
                {token_endpoint}/register
            input: "jwk_file"
            variables: ["token_endpoint"]
            
        device_post_commands:
          - name: "store_client_config"
            command: |
              cat > /etc/oauth2/{client_id}.json << EOF
              {
                "client_id": "{client_id}",
                "token_endpoint": "{token_endpoint}",
                "scope": "{scope}",
                "private_key_file": "{key_file}"
              }
              EOF
            variables: ["client_id", "token_endpoint", "scope", "key_file"]
            security:
              file_permissions: "600"
              
    # === REGISTERED CREDENTIALS (Device → Owner) ===
    registered:
      # SSH Public Key Registration
      ssh_public_key:
        enabled: true
        type: "ssh_public_key"
        device_commands:
          - name: "generate_ssh_keypair"
            command: |
              ssh-keygen -t {key_type} -b {key_size} \
                -f {key_file} -N "" -C "{comment}"
            output: ["key_file", "key_file.pub"]
            variables: ["key_type", "key_size", "comment"]
            
          - name: "extract_public_key"
            command: |
              cat {key_file}.pub > {pubkey_file}
            output: "pubkey_file"
            
          - name: "prepare_key_for_transfer"
            command: |
              # Ensure OpenSSH format
              ssh-keygen -f {pubkey_file} -y > {pubkey_formatted}
            input: "pubkey_file"
            output: "pubkey_formatted"
            
        owner_processing:
          - name: "register_ssh_key"
            command: |
              # Add to authorized_keys on target servers
              for server in {target_servers}; do
                ssh -i {admin_key} admin@${server} \
                  "echo '{public_key}' >> ~/.ssh/authorized_keys"
              done
            input: "public_key"
            variables: ["target_servers", "admin_key"]
            
        key_settings:
          key_type: "ed25519"
          key_size: 256
          comment: "device-generated key"
          
## Variable Substitution System

### Template Variables

#### Credential Data Variables (from FSIM messages)
- `{username}` - Username from credential data
- `{password}` - Password from credential data
- `{api_key}` - API key from credential data
- `{client_id}` - OAuth2 client ID
- `{client_secret}` - OAuth2 client secret
- `{token}` - Bearer token
- `{token_endpoint}` - OAuth2 token endpoint
- `{scope}` - OAuth2 scope
- `{expires_at}` - Token expiration timestamp
- `{service_name}` - Service name for credential storage

#### Certificate Variables
- `{key_algorithm}` - Key algorithm (ecdsa, rsa)
- `{key_algorithm_params}` - Algorithm-specific params (e.g., "pkeyopt ec_paramgen_curve:P-256")
- `{key_file}` - Path to generated private key
- `{csr_file}` - Path to generated CSR
- `{csr_pem_file}` - Path to CSR in PEM format
- `{cert_file}` - Path to received certificate
- `{ca_bundle_file}` - Path to CA bundle
- `{cert_name}` - Certificate name for installation
- `{validity_days}` - Certificate validity period
- `{country}` - Certificate country field
- `{state}` - Certificate state field
- `{locality}` - Certificate locality field
- `{organization}` - Certificate organization field
- `{common_name}` - Certificate common name

#### SSH Key Variables
- `{key_type}` - SSH key type (rsa, ed25519, ecdsa)
- `{key_size}` - SSH key size in bits
- `{comment}` - SSH key comment
- `{pubkey_file}` - Path to public key file
- `{pubkey_formatted}` - Formatted public key for transfer
- `{target_servers}` - List of servers to register key with
- `{admin_key}` - Admin SSH key for remote registration

#### System Variables
- `{device_id}` - Device identifier
- `{timestamp}` - Current timestamp
- `{temp_dir}` - Temporary directory path
- `{random}` - Random string for uniqueness

## Handler Execution Engine

### Multi-Phase Command Execution

```go
type CredentialHandler struct {
    Type         string           // "password", "x509_cert", etc.
    Commands     []HandlerCommand // List of commands to execute
    Variables    map[string]interface{}
    TempDir      string
    Security     SecurityConfig
}

type HandlerCommand struct {
    Name        string
    Command     string
    Input       string           // Input variable name
    Output      []string        // Output variable names
    Variables   []string        // Required variables
    Security    SecurityConfig
    Cleanup     bool            // Whether to cleanup after execution
}

func (h *CredentialHandler) ExecuteCommands(ctx context.Context, phase string, data map[string]interface{}) error {
    var commands []HandlerCommand
    
    switch phase {
    case "device_commands":
        commands = h.DeviceCommands
    case "owner_processing":
        commands = h.OwnerProcessing
    case "device_post_commands":
        commands = h.DevicePostCommands
    default:
        return fmt.Errorf("unknown phase: %s", phase)
    }
    
    for _, cmd := range commands {
        if err := h.executeCommand(cmd, data); err != nil {
            return fmt.Errorf("command %s failed: %w", cmd.Name, err)
        }
    }
    
    return nil
}

func (h *CredentialHandler) executeCommand(cmd HandlerCommand, data map[string]interface{}) error {
    // Substitute variables in command
    resolvedCmd := h.substituteVariables(cmd.Command, data)
    
    // Execute with security considerations
    output, err := h.executeWithSecurity(resolvedCmd, cmd.Security)
    if err != nil {
        return err
    }
    
    // Store output variables
    for _, outputVar := range cmd.Output {
        h.Variables[outputVar] = output
    }
    
    return nil
}
```

### Security Configuration

```go
type SecurityConfig struct {
    NoLog           bool     // Don't log command output
    Encrypt         bool     // Encrypt sensitive data
    FilePermissions string   // File permissions for created files
    Sandbox         bool     // Run in sandboxed environment
    ValidateInput   bool     // Validate input parameters
    HashOutput      bool     // Hash output for verification
}

func (h *CredentialHandler) executeWithSecurity(cmd string, security SecurityConfig) (string, error) {
    if security.NoLog {
        return h.executeWithoutLogging(cmd)
    }
    
    if security.Encrypt {
        return h.executeWithEncryption(cmd)
    }
    
    if security.Sandbox {
        return h.executeInSandbox(cmd)
    }
    
    return h.executeStandard(cmd)
}
```

## FSIM Integration

### Message Handler Integration

```go
type CredentialingFSIM struct {
    HandlerManager *CredentialHandlerManager
    ActiveHandlers map[string]*CredentialHandler
    CurrentFlow    string // "provisioned", "enrolled", "registered"
}

func (f *CredentialingFSIM) Receive(ctx context.Context, messageName string, messageBody io.Reader, respond func(string) io.Writer, yield func()) error {
    switch messageName {
    // Provisioned credentials (Owner → Device)
    case "fdo.credentials:credential-begin":
        return f.handleCredentialBegin(ctx, messageBody)
    case "fdo.credentials:credential-data-0", "fdo.credentials:credential-data-1", ...:
        return f.handleCredentialData(ctx, messageName, messageBody)
    case "fdo.credentials:credential-end":
        return f.handleCredentialEnd(ctx, messageBody)
        
    // Enrolled credentials (Device ↔ Owner)
    case "fdo.credentials:request-begin":
        return f.handleRequestBegin(ctx, messageBody)
    case "fdo.credentials:request-data-0", "fdo.credentials:request-data-1", ...:
        return f.handleRequestData(ctx, messageName, messageBody)
    case "fdo.credentials:request-end":
        return f.handleRequestEnd(ctx, messageBody)
    case "fdo.credentials:response-begin":
        return f.handleResponseBegin(ctx, messageBody)
    case "fdo.credentials:response-data-0", "fdo.credentials:response-data-1", ...:
        return f.handleResponseData(ctx, messageName, messageBody)
    case "fdo.credentials:response-end":
        return f.handleResponseEnd(ctx, messageBody)
        
    // Registered credentials (Device → Owner)
    case "fdo.credentials:pubkey-request":
        return f.handlePubkeyRequest(ctx, messageBody)
    case "fdo.credentials:pubkey-begin":
        return f.handlePubkeyBegin(ctx, messageBody)
    case "fdo.credentials:pubkey-data-0", "fdo.credentials:pubkey-data-1", ...:
        return f.handlePubkeyData(ctx, messageName, messageBody)
    case "fdo.credentials:pubkey-end":
        return f.handlePubkeyEnd(ctx, messageBody)
    }
}
```

### Chunked Message Handling

```go
type ChunkedMessageHandler struct {
    CurrentMessage   *ChunkedMessage
    AccumulatedData  []byte
    TotalSize        uint64
    ReceivedChunks   int
    ExpectedChunks   int
}

func (h *ChunkedMessageHandler) handleChunkedMessage(messageName string, data []byte) error {
    chunkNum, err := extractChunkNumber(messageName)
    if err != nil {
        return err
    }
    
    if chunkNum == 0 {
        // Begin message - parse metadata
        return h.handleBeginMessage(data)
    } else if h.isEndMessage(messageName) {
        // End message - process complete data
        return h.handleEndMessage(data)
    } else {
        // Data message - accumulate
        return h.handleDataMessage(chunkNum, data)
    }
}
```

## Error Handling and Reporting

### Error Categories

```go
type CredentialError struct {
    Type         string    // "validation", "execution", "network", "security"
    Flow         string    // "provisioned", "enrolled", "registered"
    Phase        string    // "device_commands", "owner_processing", "device_post_commands"
    Command      string    // Command that failed
    Message      string    // Human-readable error
    Code         int       // FDO error code
    Retryable     bool      // Whether this error is retryable
    CredentialID string    // Related credential ID
}

func (e *CredentialError) ToFDOError() (int, string) {
    return e.Code, e.Message
}
```

### Error Codes

```go
const (
    ErrInvalidCredentialType     = 1000
    ErrInvalidCredentialData     = 1001
    ErrCredentialIDExists        = 1002
    ErrHashVerificationFailed    = 1003
    ErrCSRValidationFailed       = 1004
    ErrCertificateSigningFailed  = 1005
    ErrPublicKeyFormatInvalid    = 1006
    ErrUnsupportedCredentialType = 1007
    ErrMetadataValidationFailed  = 1008
    ErrCredentialStorageFailed   = 1009
    ErrCommandExecutionFailed    = 1010
    ErrSecurityValidationFailed  = 1011
    ErrNetworkTimeout            = 1012
    ErrChunkTransferFailed       = 1013
)
```

## Implementation Examples

### Example 1: Username/Password Provisioning

```yaml
handlers:
  provisioned:
    password:
      enabled: true
      type: "password"
      commands:
        - name: "create_user"
          command: "useradd -m {username}"
          variables: ["username"]
          
        - name: "set_password"
          command: "echo '{password}' | passwd --stdin {username}"
          variables: ["username", "password"]
          security:
            no_log: true
            
        - name: "create_ssh_dir"
          command: "mkdir -p /home/{username}/.ssh"
          variables: ["username"]
          
        - name: "set_ownership"
          command: "chown -R {username}:{username} /home/{username}"
          variables: ["username"]
```

**FSIM Flow:**
```
Owner → Device: fdo.credentials:credential-begin = {
    -1: "user-credential-001",
    -2: "password",
    -3: {"username": "admin", "expires_at": "2027-01-01T00:00:00Z"}
}

Owner → Device: fdo.credentials:credential-data-0 = <CBOR bstr: {"username": "admin", "password": "hashed-password"}>

Owner → Device: fdo.credentials:credential-end = {0: 0, 1: h'hash...'}

Device → Owner: fdo.credentials:credential-result = [0, "User account created successfully"]
```

### Example 2: X.509 Certificate Enrollment

```yaml
handlers:
  enrolled:
    x509_cert:
      enabled: true
      type: "x509_cert"
      device_commands:
        - name: "generate_keypair"
          command: "openssl genpkey -algorithm ecdsa -pkeyopt ec_paramgen_curve:P-256 -out {key_file}"
          output: "key_file"
          
        - name: "generate_csr"
          command: |
            openssl req -new -key {key_file} -out {csr_file} \
              -subj "/C={country}/ST={state}/L={locality}/O={organization}/CN={common_name}"
          input: "key_file"
          output: "csr_file"
          variables: ["country", "state", "locality", "organization", "common_name"]
          
      device_post_commands:
        - name: "install_certificate"
          command: "cp {cert_file} /etc/ssl/certs/{cert_name}.crt && update-ca-certificates"
          input: "cert_file"
          variables: ["cert_name"]
          
        - name: "install_ca_bundle"
          command: "cp {ca_bundle_file} /etc/ssl/certs/{cert_name}-ca-bundle.crt"
          input: "ca_bundle_file"
          variables: ["cert_name"]
```

**FSIM Flow:**
```
Device → Owner: fdo.credentials:request-begin = {
    -1: "device-mtls-cert",
    -2: "x509_cert",
    -3: {"subject_dn": "CN=device-001,O=Example Corp"}
}

Device → Owner: fdo.credentials:request-data-0 = <CBOR bstr: CSR in PEM format>

Device → Owner: fdo.credentials:request-end = {0: 0, 1: h'csr-hash...'}

Owner → Device: fdo.credentials:response-begin = {
    -1: "device-mtls-cert",
    -2: "x509_cert",
    -3: {"cert_format": "pem", "ca_bundle_included": true}
}

Owner → Device: fdo.credentials:response-data-0 = <CBOR bstr: client certificate>
Owner → Device: fdo.credentials:response-data-1 = <CBOR bstr: CA bundle>

Owner → Device: fdo.credentials:response-end = {0: 0, 1: h'cert-hash...'}

Device → Owner: fdo.credentials:response-result = [0, "Certificate and CA bundle installed"]
```

### Example 3: SSH Public Key Registration

```yaml
handlers:
  registered:
    ssh_public_key:
      enabled: true
      type: "ssh_public_key"
      device_commands:
        - name: "generate_ssh_keypair"
          command: "ssh-keygen -t ed25519 -f {key_file} -N '' -C '{comment}'"
          output: ["key_file", "key_file.pub"]
          variables: ["comment"]
          
        - name: "extract_public_key"
          command: "cat {key_file}.pub > {pubkey_file}"
          input: "key_file.pub"
          output: "pubkey_file"
```

**FSIM Flow:**
```
Owner → Device: fdo.credentials:pubkey-request = {
    -1: "device-config-access",
    -2: "ssh_public_key",
    -3: {"service_name": "config-server", "key_type": "ed25519"}
}

Device → Owner: fdo.credentials:pubkey-begin = {
    0: 564,
    1: "sha256",
    -1: "device-config-access",
    -2: "ssh_public_key"
}

Device → Owner: fdo.credentials:pubkey-data-0 = <CBOR bstr: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5...">

Device → Owner: fdo.credentials:pubkey-end = {0: 0, 1: h'pubkey-hash...'}

Owner → Device: fdo.credentials:pubkey-result = [0, "Public key registered with config-server"]
```

## Security Considerations

### Data Protection
- **Private keys** never leave the device (except in server-generated key flow)
- **Passwords** are marked with `no_log: true` and encrypted at rest
- **API keys** and **secrets** stored with restricted file permissions
- **Temporary files** are securely cleaned up after use

### Command Validation
- **Input validation** for all user-provided data
- **Command sanitization** to prevent injection attacks
- **Permission checking** before executing privileged commands
- **Sandbox execution** for complex operations

### Audit Trail
- **Comprehensive logging** of all credential operations
- **Hash verification** for all chunked transfers
- **Error reporting** back to FDO server with appropriate codes
- **Success/failure acknowledgment** for all credential flows

This design provides a complete, secure, and flexible framework for handling all FDO credentialing scenarios while maintaining compatibility with the existing generic handler system.
