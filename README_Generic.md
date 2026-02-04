# Generic FDO Client

A configurable FDO (Fido Device Onboard) client that allows users to define custom handlers for sysconfig parameters and payload files through YAML configuration.

## Features

- **YAML-based Configuration**: Easy-to-read configuration files
- **Template-based Command Execution**: Flexible command templates with variable substitution
- **Sysconfig Parameter Handling**: Map sysconfig keys to custom commands
- **Payload MIME Type Handling**: Handle different payload types with specific commands
- **Error Reporting**: Comprehensive error reporting back to FDO server
- **Graceful Fallback**: Automatic fallback to default handlers if configuration fails
- **Safe Defaults**: Uses echo commands by default for safety

## Quick Start

### 1. Configuration

Create a configuration file (e.g., `config_generic.yaml`):

```yaml
# Generic Handler Configuration
handlers:
  # SysConfig handlers - map parameter names to command templates
  sysconfig:
    hostname:
      command: "echo 'Setting hostname to: {value}'"
      enabled: true
    timezone:
      command: "echo 'Setting timezone to: {value}'"
      enabled: true
    ntp-server:
      command: "echo 'Setting NTP server to: {value}'"
      enabled: true
    locale:
      command: "echo 'Setting locale to: {value}'"
      enabled: true

  # Payload handlers
  payload:
    # Temporary directory for payload files
    temp_dir: "/tmp/fdo_payloads"
    
    # Default action if no specific handler matches
    default_action: "reject"  # Options: "reject", "accept", "require_handler"
    
    # MIME type specific handlers
    mime_types:
      application/octet-stream:
        enabled: true
        command: "echo 'Processing binary payload: {filename} ({size} bytes)'"
        
      application/json:
        enabled: true
        command: "echo 'Processing JSON payload: {filename}'"
        
      text/plain:
        enabled: true
        command: "echo 'Processing text payload: {filename}'"
```

### 2. Run Demo

Test the handler system without connecting to an FDO server:

```bash
./fdo-client -demo
```

### 3. Run with FDO Server

Use the generic handlers with a real FDO server:

```bash
./fdo-client -config config_generic.yaml
```

## Configuration Reference

### SysConfig Handlers

```yaml
handlers:
  sysconfig:
    parameter_name:
      command: "template_command"  # Command template with {value} placeholder
      enabled: true/false         # Enable/disable this handler
```

**Template Variables:**
- `{parameter}` - The parameter name (e.g., "hostname")
- `{value}` - The parameter value (e.g., "my-device")

**Examples:**
```yaml
hostname:
  command: "hostname {value}"
  enabled: true

timezone:
  command: "timedatectl set-timezone {value}"
  enabled: true

dns-server:
  command: "echo 'nameserver {value}' >> /etc/resolv.conf"
  enabled: true
```

### Payload Handlers

```yaml
handlers:
  payload:
    temp_dir: "/tmp/fdo_payloads"           # Directory for temporary payload files
    default_action: "reject"                 # Default action for unknown MIME types
    
    mime_types:
      mime/type:
        enabled: true/false                  # Enable/disable this MIME type handler
        command: "template_command"          # Command template
```

**Template Variables:**
- `{filename}` - Full path to the temporary payload file
- `{mimetype}` - The MIME type (e.g., "application/json")
- `{size}` - Payload size in bytes
- `{name}` - Original payload filename

**Examples:**
```yaml
payload:
  temp_dir: "/tmp/fdo_payloads"
  default_action: "reject"
  
  mime_types:
    application/x-cloud-init:
      enabled: true
      command: "cloud-init init --file {filename}"
      
    application/x-shellscript:
      enabled: true
      command: "chmod +x {filename} && {filename}"
      
    application/json:
      enabled: true
      command: "jq . {filename} > /etc/config.json"
```

### Default Actions

- **`reject`**: Reject unknown payload types (returns error to server)
- **`accept`**: Accept unknown payload types but don't process them
- **`require_handler`**: Reject if no specific handler is configured

## Error Reporting

The handler system provides comprehensive error reporting:

### SysConfig Errors
- **Missing Handler**: "no handler configured for sysconfig parameter: X"
- **Disabled Handler**: "handler disabled for sysconfig parameter: X"
- **Command Execution**: "command execution failed: X"

### Payload Errors
- **Unsupported Type**: "Unsupported payload type: X"
- **No Handler**: "No handler configured for MIME type: X"
- **File Operations**: "Failed to write payload to temp file: X"
- **Handler Execution**: "Handler execution failed: X"

All errors are reported back to the FDO server with appropriate status codes.

## Safety Features

### Safe Defaults
- Commands use `echo` by default for safety
- No actual system commands are executed unless explicitly configured
- Temporary files are automatically cleaned up

### Validation
- Configuration validation on startup
- Absolute path requirement for temp directory
- Template syntax validation
- MIME type validation

### Error Handling
- Graceful fallback to default handlers
- Comprehensive error reporting
- Safe failure modes

## Advanced Usage

### Custom Command Templates

You can use any command template with the available variables:

```yaml
sysconfig:
  custom_param:
    command: "logger 'FDO: Setting {parameter} to {value}'"
    enabled: true

payload:
  mime_types:
    application/zip:
      enabled: true
      command: "unzip -o {filename} -d /opt/deploy/"
```

### Conditional Processing

You can include conditional logic in your commands:

```yaml
sysconfig:
  hostname:
    command: |
      if [ "{value}" != "$(hostname)" ]; then
        echo "Changing hostname from $(hostname) to {value}"
        hostname {value}
      else
        echo "Hostname already set to {value}"
      fi
    enabled: true
```

### Multiple MIME Types

Handle multiple MIME types with the same command:

```yaml
payload:
  mime_types:
    application/json:
      enabled: true
      command: "jq . {filename} > /etc/config.json"
    text/json:
      enabled: true
      command: "jq . {filename} > /etc/config.json"
```

## Building

```bash
go build -o fdo-client .
```

## Testing

Run the built-in demo to test the handler system:

```bash
./fdo-client -demo
```

This will:
1. Load and validate the configuration
2. Test all configured sysconfig handlers
3. Test all configured payload handlers
4. Show a summary of the configuration

## Integration with Existing FDO Setup

The generic handler system integrates seamlessly with existing FDO infrastructure:

1. **Backward Compatible**: Falls back to default handlers if generic config fails
2. **Standard FDO Protocol**: Uses standard FDO sysconfig and payload modules
3. **Error Reporting**: Reports handler errors through standard FDO error mechanisms
4. **Version Support**: Works with both FDO 1.0.1 and 2.0 protocols

## Production Deployment

For production deployment:

1. **Replace Echo Commands**: Replace `echo` commands with actual system commands
2. **Secure Temp Directory**: Use a secure temporary directory with proper permissions
3. **Logging**: Add proper logging for audit purposes
4. **Error Handling**: Implement appropriate error handling for your specific environment
5. **Testing**: Thoroughly test all handler commands in a non-production environment

## Example Production Configuration

```yaml
handlers:
  sysconfig:
    hostname:
      command: "hostnamectl set-hostname {value}"
      enabled: true
    timezone:
      command: "timedatectl set-timezone {value}"
      enabled: true
    dns-server:
      command: |
        echo "nameserver {value}" > /etc/resolv.conf.head
        systemctl restart systemd-resolved
      enabled: true

  payload:
    temp_dir: "/var/lib/fdo/payloads"
    default_action: "reject"
    
    mime_types:
      application/x-cloud-init:
        enabled: true
        command: |
          cp {filename} /var/lib/cloud/instance/user-data
          cloud-init init
          
      application/x-shellscript:
        enabled: true
        command: |
          chmod +x {filename}
          {filename}
```

## Troubleshooting

### Common Issues

1. **Configuration Not Found**: Ensure the config file exists and is readable
2. **Template Syntax Errors**: Check template variable syntax ({variable})
3. **Permission Errors**: Ensure the temp directory is writable
4. **Command Failures**: Test commands manually before adding to configuration

### Debug Mode

Enable debug mode for detailed logging:

```yaml
debug: true
```

### Validation

Use the built-in validation to check your configuration:

```bash
./fdo-client -demo  # Will validate and show configuration
```

## Contributing

To add new handler types or enhance the system:

1. Add new configuration structures in `generic_handlers.go`
2. Implement handler logic in the appropriate functions
3. Add validation rules
4. Update tests and documentation

## License

SPDX-FileCopyrightText: (C) 2024 Intel Corporation
SPDX-License-Identifier: Apache 2.0
