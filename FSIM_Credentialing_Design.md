# FSIM Handler Design - Simplified

## Overview

This design provides a simple, practical handler system for multiple FDO FSIM modules, supporting the protocol flows defined in the specification:

1. **SysConfig Handlers** - Device configuration parameter setting
2. **Payload Handlers** - File-based payload processing by MIME type
3. **Credentialing Handlers** - `fdo.credentials` module for secrets and certificates
4. **Wi-Fi Handlers** - Network configuration through Wi-Fi credentials

## Design Philosophy

- **Simple configuration** - Users only define shell commands and file paths
- **Protocol-driven** - Variables come from FSIM protocol, no need to define them
- **Built-in security** - Automatic validation and sanitization
- **Flexible flows** - Support simple provisioning and complex multi-phase flows

## Configuration Structure

```yaml
credentialing:
  temp_dir: "/tmp/fdo_credentials"
  
  handlers:
    # Simple provisioned credentials (server sends data → we process)
    password:
      commands:
        - "python3 /usr/local/bin/process_password.py {temp_dir}/password.json"
        
    api_key:
      commands:
        - "python3 /usr/local/bin/install_api_key.py {temp_dir}/api_key.json"
        
    oauth2_client_secret:
      commands:
        - "python3 /usr/local/bin/setup_oauth2.py {temp_dir}/oauth2.json"
        
    bearer_token:
      commands:
        - "python3 /usr/local/bin/install_bearer_token.py {temp_dir}/bearer_token.json"
        
    # Certificate enrollment (device generates CSR → server signs → device installs)
    x509_cert:
      generate_phase:
        commands:
          - "openssl genpkey -algorithm ecdsa -pkeyopt ec_paramgen_curve:P-256 -out {temp_dir}/{device_id}_key.pem"
          - "openssl req -new -key {temp_dir}/{device_id}_key.pem -out {temp_dir}/{device_id}.csr -subj '/CN={device_id}'"
          
        send_payload: "{temp_dir}/{device_id}.csr"
        
      install_phase:
        receive_payload:
          cert: "{temp_dir}/{device_id}_cert.pem"
          ca_bundle: "{temp_dir}/{device_id}_ca_bundle.pem"
          
        commands:
          - "cp {temp_dir}/{device_id}_cert.pem /etc/ssl/certs/device.crt"
          - "cp {temp_dir}/{device_id}_ca_bundle.pem /etc/ssl/certs/device-ca-bundle.crt"
          - "update-ca-certificates"
          
    # SSH key registration (device generates key → server registers)
    ssh_public_key:
      generate_phase:
        commands:
          - "ssh-keygen -t ed25519 -f {temp_dir}/{device_id}_ssh_key -N '' -C '{device_id}-key'"
          - "cat {temp_dir}/{device_id}_ssh_key.pub > {temp_dir}/{device_id}_public_key"
          
        send_payload: "{temp_dir}/{device_id}_public_key"
```

## Protocol Variables

Variables are provided directly by the FSIM protocol - no configuration needed.

### Available Variables

**Protocol-level variables provided for shell command substitution:**

- `{temp_dir}` - Temporary directory for credential files (from handler config)
- `{device_id}` - Device identifier (from FDO session)
- `{credential_id}` - Credential identifier (from protocol metadata)
- `{credential_type}` - Credential type (from protocol metadata)
- `{endpoint_url}` - Service endpoint URL (from protocol metadata)
- `{credential_scope}` - Credential scope (from protocol metadata)

**Note:** Credential-specific data (username, password, api_key, client_id, etc.) is provided in JSON files that users process in their shell commands. The protocol variables above are for file paths and metadata, not the actual credential data.

## JSON Data Processing

Users receive credential data as JSON files in their temporary directory. Each credential type has its own JSON structure:

### Password Credentials
```json
{
  "username": "admin",
  "password": "secret123",
  "hash_algorithm": "bcrypt"
}
```

### API Key Credentials  
```json
{
  "api_key": "sk_live_abc123...",
  "service": "api.example.com"
}
```

### OAuth2 Client Credentials
```json
{
  "client_id": "device-001",
  "client_secret": "secret-abc...",
  "token_endpoint": "https://auth.example.com/token",
  "scope": "device:read device:write"
}
```

### Bearer Token Credentials
```json
{
  "token": "eyJhbGciOiJSUzI1NiIs...",
  "token_type": "Bearer",
  "expires_at": "2026-12-31T23:59:59Z"
}
```

Users write shell commands or scripts to parse these JSON files and apply the credentials appropriately.

## Flow Types

### Simple Handlers
For provisioned credentials - just execute commands when data arrives:
```yaml
password:
  commands:
    - "python3 /usr/local/bin/process_password.py {temp_dir}/password.json"
    
api_key:
  commands:
    - "python3 /usr/local/bin/install_api_key.py {temp_dir}/api_key.json"
    
oauth2_client_secret:
  commands:
    - "python3 /usr/local/bin/setup_oauth2.py {temp_dir}/oauth2.json"
```

### Multi-Phase Handlers  
For enrolled and registered credentials:
```yaml
x509_cert:
  generate_phase:
    commands: [...]        # Generate CSR/key
    send_payload: "..."    # Send to server
  install_phase:
    receive_payload: {...} # Receive response
    commands: [...]        # Install result
```

## Security

### Automatic Validation
Built-in validation for all protocol variables:
- **No shell injection** - Filter dangerous characters
- **Format validation** - URLs, emails, timestamps
- **Length limits** - Prevent buffer overflows
- **Required fields** - Ensure needed data is present

### High-Security Variables
Special handling for sensitive data:
- `{password}`, `{client_secret}` - No logging, strict filtering
- `{token}` - JWT format validation
- `{api_key}` - API key format validation

## Variable Modifier System (for Sysconfig)

For sysconfig handlers, use modifiers to validate and sanitize values:

### Modifier Syntax
```yaml
{variable:modifier1:modifier2}
```

### Filter Modifiers (change the value)
- `:nospace` - Remove all spaces
- `:underscore` - Replace spaces with underscores  
- `:alphanum` - Keep only alphanumeric characters
- `:lower` - Convert to lowercase
- `:upper` - Convert to uppercase
- `:trim` - Trim leading/trailing whitespace
- `:safe` - Remove shell metacharacters (&|<>'"$`)
- `:filename` - Make safe for filenames (alphanumeric + _-.)
- `:domain` - Make safe for domain names
- `:unspecified` - Replace empty with "unspecified"
- `:default:value` - Replace empty with default value
- `:ifempty:value` - Alternative syntax for default value

### Validator Modifiers (fail on mismatch)
- `:required` - Fail if value is empty
- `:username` - Validate username format (alphanumeric + _-)
- `:password` - Validate password (no shell chars)
- `:url` - Validate URL format
- `:email` - Validate email format
- `:numeric` - Validate numeric only
- `:length:32` - Validate max length
- `:minlength:8` - Validate min length

### Sysconfig Examples
```yaml
handlers:
  sysconfig:
    hostname:
      command: "hostnamectl set-hostname {value:alphanum:lower:trim}"
      
    admin_user:
      command: "useradd -m {value:username:lower:trim}"
      
    api_endpoint:
      command: "echo '{value:url:trim}' > /etc/api/endpoint"
      
    config_file:
      command: "cp /etc/template.conf /etc/{value:filename:trim}.conf"
      
    # Examples with new empty-value modifiers
    device_owner:
      command: "echo '{username:unspecified}' > /etc/device/owner"
      
    required_field:
      command: "echo '{value:required:trim}' > /etc/config/setting"
      
    default_value:
      command: "echo '{value:default:admin:trim}' > /etc/config/user"
```

## Implementation

### Handler Structure
```go
type CredentialHandler struct {
    Commands       []string         `json:",omitempty"`
    GeneratePhase  *PhaseConfig     `json:",omitempty"`
    InstallPhase   *PhaseConfig     `json:",omitempty"`
}

type PhaseConfig struct {
    Commands      []string          `json:",omitempty"`
    SendPayload   string            `json:",omitempty"`
    ReceivePayload map[string]string `json:",omitempty"`
}
```

### Execution Logic
```go
func (h *CredentialHandler) Handle(data map[string]interface{}) error {
    if h.GeneratePhase != nil {
        return h.handleGeneratePhase(data)
    } else if h.InstallPhase != nil {
        return h.handleInstallPhase(data)
    } else {
        return h.executeCommands(h.Commands, data)
    }
}
```

## Integration with Generic Handlers

The credentialing handlers integrate seamlessly with the existing generic handler framework:

```yaml
# In config_generic.yaml
handlers:
  sysconfig:
    hostname:
      commands:
        - "hostnamectl set-hostname {value:alphanum:lower:trim}"
      
  payload:
    application/json:
      filename: "/tmp/payload.json"
      commands:
        - "python3 /usr/local/bin/process_json.py /tmp/payload.json"
      
  credentialing:
    temp_dir: "/tmp/fdo_credentials"
    password:
      commands:
        - "python3 /usr/local/bin/process_password.py {temp_dir}/password.json"
```

## Variable Modifier System

The variable modifier system provides flexible validation and sanitization for ALL FSIM handler types (sysconfig, payload, credentialing, Wi-Fi). This prevents both accidental issues and malicious injection attacks while making configuration user-friendly.

### Modifier Syntax
```yaml
{variable:modifier1:modifier2:modifier3}
```

Modifiers are applied in order from left to right. Filter modifiers change the value, while validator modifiers can reject invalid input.

### Filter Modifiers (change the value)
- `:nospace` - Remove all spaces
- `:underscore` - Replace spaces with underscores  
- `:alphanum` - Keep only alphanumeric characters
- `:lower` - Convert to lowercase
- `:upper` - Convert to uppercase
- `:trim` - Trim leading/trailing whitespace
- `:safe` - Remove shell metacharacters (&|<>'"$`)
- `:filename` - Make safe for filenames (alphanumeric + _-.)
- `:domain` - Make safe for domain names
- `:unspecified` - Replace empty with "unspecified"
- `:default:value` - Replace empty with default value
- `:ifempty:value` - Alternative syntax for default value

### Validator Modifiers (fail on mismatch)
- `:required` - Fail if value is empty
- `:username` - Validate username format (alphanumeric + _-)
- `:password` - Validate password (no shell chars)
- `:url` - Validate URL format
- `:email` - Validate email format
- `:numeric` - Validate numeric only
- `:length:32` - Validate max length
- `:minlength:8` - Validate min length

### Examples Across Handler Types

#### SysConfig Examples
```yaml
handlers:
  sysconfig:
    hostname:
      commands:
        - "hostnamectl set-hostname {value:alphanum:lower:trim}"
        
    admin_user:
      commands:
        - "useradd -m {value:username:lower:trim}"
        
    api_endpoint:
      commands:
        - "echo '{value:url:trim}' > /etc/api/endpoint"
        
    config_file:
      commands:
        - "cp /etc/template.conf /etc/{value:filename:trim}.conf"
        
    # Examples with new empty-value modifiers
    device_owner:
      commands:
        - "echo '{username:unspecified}' > /etc/device/owner"
        
    required_field:
      commands:
        - "echo '{value:required:trim}' > /etc/config/setting"
        
    default_value:
      commands:
        - "echo '{value:default:admin:trim}' > /etc/config/user"
```

#### Credentialing Examples
```yaml
handlers:
  credentialing:
    password:
      commands:
        - "python3 /usr/local/bin/process_password.py {temp_dir}/password.json"
        
    ssh_public_key:
      commands:
        - "ssh-keygen -t ed25519 -f {temp_dir}/{device_id}_ssh_key -N '' -C '{device_id}-key'"
        - "cat {temp_dir}/{device_id}_ssh_key.pub > {temp_dir}/{device_id}_public_key"
```

#### Wi-Fi Examples
```yaml
handlers:
  wifi:
    ssid:
      commands:
        - "nmcli dev wifi connect 'network-name' password 'network-password'"
        
    enterprise:
      commands:
        - "nmcli connection add type wifi con-name 'enterprise-net' ifname wlan0 ssid 'enterprise-net' -- wifi-sec.key-mgmt wpa-enterprise wifi-sec.identity 'user' wifi-sec.password 'pass'"
```

#### Payload Examples
```yaml
handlers:
  payload:
    application/json:
      filename: "/tmp/{filename:filename:trim}.json"
      commands:
        - "python3 /usr/local/bin/process_json.py /tmp/{filename:filename:trim}.json"
```

### Security Benefits
- **Prevents shell injection** - `:safe` modifier removes dangerous characters
- **Fixes accidents** - `:alphanum`, `:nospace` clean user input
- **Enforces rules** - `:required`, `:length:N` validate requirements
- **User-friendly** - Clear error messages and intuitive syntax

**Note:** The variable modifier system is implemented in the generic handler framework and applies to all FSIM handler types. See `Variable_Modifier_System.md` for complete implementation details.

## Payload Handlers

Payload handlers provide simple file-based processing for different MIME types received through the FDO payload FSIM module:

### Configuration Structure
```yaml
handlers:
  payload:
    application/json:
      filename: "/tmp/payload_{filename}.json"
      commands:
        - "python3 /usr/local/bin/process_json.py /tmp/payload_{filename}.json"
        
    text/plain:
      filename: "/tmp/config_{filename}.txt"
      commands:
        - "cat /tmp/config_{filename}.txt >> /etc/config.txt"
        
    application/octet-stream:
      filename: "/tmp/binary_{filename}"
      commands:
        - "/usr/local/bin/install_binary.sh /tmp/binary_{filename}"
        
    application/cloud-init:
      filename: "/tmp/cloudinit.dat"
      commands:
        - "/bin/config-cloudinit /tmp/cloudinit.dat"
```

### Available Variables
- `{filename}` - The original payload filename (for use in filename template)
- `{mime_type}` - The MIME type of the payload
- `{size}` - Payload size in bytes
- `{device_id}` - Device identifier

**Note:** The `filename` field supports variable substitution using `{filename}` and other available variables. The resolved filename path is then available for use in commands.

### Examples

#### JSON Configuration Processing
```yaml
application/json:
  filename: "/tmp/device_config_{filename}.json"
  commands:
    - "python3 /usr/local/bin/apply_config.py /tmp/device_config_{filename}.json"
    - "systemctl restart my-service"
```

#### Firmware Update
```yaml
application/octet-stream:
  filename: "/tmp/firmware_{filename}"
  commands:
    - "echo 'Installing firmware /tmp/firmware_{filename} ({size} bytes)'"
    - "/usr/local/bin/install_firmware.sh /tmp/firmware_{filename}"
```

#### Certificate Bundle
```yaml
application/x-pem-file:
  filename: "/tmp/certs_{filename}.pem"
  commands:
    - "cp /tmp/certs_{filename}.pem /etc/ssl/certs/device_bundle.pem"
    - "update-ca-certificates"
```

#### Cloud-init Configuration
```yaml
application/cloud-init:
  filename: "/tmp/cloudinit.dat"
  commands:
    - "/bin/config-cloudinit /tmp/cloudinit.dat"
```

### Security Considerations
- **File validation** - Payloads are validated against MIME type before processing
- **Path safety** - `{filename}` is sanitized to prevent directory traversal
- **Size limits** - Maximum payload size enforced before saving
- **Command isolation** - Commands run with limited privileges

## Wi-Fi Handlers

Wi-Fi handlers provide network configuration through the FDO Wi-Fi FSIM module, similar to credentialing but focused on network settings:

### Configuration Structure
```yaml
handlers:
  wifi:
    ssid:
      commands:
        - "nmcli dev wifi connect '{ssid}' password '{password}'"
        
    enterprise:
      commands:
        - "nmcli connection add type wifi con-name '{ssid}' ifname wlan0 ssid '{ssid}' -- wifi-sec.key-mgmt wpa-enterprise wifi-sec.identity '{username}' wifi-sec.password '{password}'"
        
    hidden:
      commands:
        - "nmcli dev wifi connect '{ssid}' password '{password}' hidden yes"
```

### Available Variables
- `{ssid}` - Network name (from Wi-Fi credential data)
- `{password}` - Network password (from Wi-Fi credential data)
- `{username}` - Enterprise username (optional, from Wi-Fi credential data)
- `{security_type}` - Security type: "wpa-psk", "wpa-enterprise", "open"
- `{device_id}` - Device identifier

### Examples

#### Simple WPA2 Network
```yaml
ssid:
  commands:
    - "nmcli dev wifi connect '{ssid}' password '{password}'"
    - "echo 'Connected to {ssid}' > /var/log/wifi.log"
```

#### Enterprise Network (802.1X)
```yaml
enterprise:
  commands:
    - "nmcli connection add type wifi con-name '{ssid}' ifname wlan0 ssid '{ssid}' -- wifi-sec.key-mgmt wpa-enterprise wifi-sec.identity '{username}' wifi-sec.password '{password}'"
    - "nmcli connection up '{ssid}'"
```

#### Hidden Network
```yaml
hidden:
  commands:
    - "nmcli dev wifi connect '{ssid}' password '{password}' hidden yes"
    - "systemctl restart networking.service"
```

#### Open Network (No Password)
```yaml
open:
  commands:
    - "nmcli dev wifi connect '{ssid}'"
    - "echo 'Connected to open network {ssid}'"
```

### Security Considerations
- **Password protection** - Wi-Fi passwords are not logged
- **Command validation** - SSID and password values are sanitized
- **Network isolation** - Wi-Fi commands run with network privileges only
- **Fallback handling** - Failed connections are reported to FDO server

## Error Handling

All handlers report errors back to the FDO server:
- **Command execution failures** - Shell command errors
- **Validation failures** - Invalid variable formats
- **File system errors** - Permission issues, disk space
- **Protocol errors** - Missing required fields, malformed data

## Examples

### Password Provisioning
```
Server sends: {"username": "admin", "password": "secret123"}
Device runs: useradd -m admin
Device runs: echo 'secret123' | passwd --stdin admin
Result: Success/failure back to server
```

### Certificate Enrollment
```
Device generates: CSR file
Device sends: CSR to server
Server returns: Certificate + CA bundle
Device installs: cert and CA bundle
Result: Success/failure back to server
```

### SSH Key Registration
```
Device generates: SSH key pair
Device sends: Public key to server
Server registers: Key with target services
Result: Success/failure back to server
```

---

This design provides a complete, secure, and user-friendly framework for handling all FDO credentialing scenarios while maintaining simplicity and flexibility.

The architecture leverages the existing generic handler framework while adding the necessary complexity for credential management, ensuring a consistent and secure approach to all FDO credentialing needs.
