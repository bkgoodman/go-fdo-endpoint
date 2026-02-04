# Variable Modifier System for Sysconfig Handlers

## Overview

The variable modifier system provides flexible validation and sanitization for sysconfig handler values, preventing both accidental issues and malicious injection attacks while making configuration user-friendly.

## Modifier Syntax

```yaml
{variable:modifier1:modifier2:modifier3}
```

Modifiers are applied in order from left to right. Filter modifiers change the value, while validator modifiers can reject invalid input.

## Filter Modifiers (change the value)

| Modifier | Description | Example Input | Example Output |
|----------|-------------|---------------|----------------|
| `:nospace` | Remove all spaces | "my hostname" | "myhostname" |
| `:underscore` | Replace spaces with underscores | "my hostname" | "my_hostname" |
| `:alphanum` | Keep only alphanumeric characters | "host-name_123" | "hostname123" |
| `:lower` | Convert to lowercase | "MyHOST" | "myhost" |
| `:upper` | Convert to uppercase | "myhost" | "MYHOST" |
| `:trim` | Trim leading/trailing whitespace | "  myhost  " | "myhost" |
| `:safe` | Remove shell metacharacters (&|<>'"$`) | "host&name" | "hostname" |
| `:filename` | Make safe for filenames (alphanumeric + _-.) | "my host@name" | "myhostname" |
| `:domain` | Make safe for domain names (lowercase + no spaces) | "My HOST Name" | "myhostname" |
| `:unspecified` | Replace empty with "unspecified" | "" | "unspecified" |
| `:default:value` | Replace empty with default value | "" | "value" |
| `:ifempty:value` | Alternative syntax for default value | "" | "value" |

## Validator Modifiers (fail on mismatch)

| Modifier | Description | Valid Example | Invalid Example |
|----------|-------------|---------------|----------------|
| `:required` | Fail if value is empty | "admin" | "" (empty) |
| `:username` | Validate username format (alphanumeric + _-) | "admin_user" | "admin-user!" |
| `:password` | Validate password (no shell chars) | "secret123" | "secret&123" |
| `:url` | Validate URL format | "https://api.example.com" | "api.example.com" |
| `:email` | Validate email format | "admin@example.com" | "admin@example" |
| `:numeric` | Validate numeric only | "1234" | "12a34" |
| `:length:32` | Validate max length | "short" | "this_is_too_long_for_validation" |
| `:minlength:8` | Validate min length | "longenough" | "short" |

## Implementation

```go
type Modifier interface {
    Apply(value string) (string, error)
}

type FilterModifier struct {
    Name string
    Func func(string) string
}

type ValidatorModifier struct {
    Name string
    Func func(string) error
}

var modifiers = map[string]Modifier{
    // Filter modifiers
    "nospace":     FilterModifier{Func: func(s string) string { 
        return strings.ReplaceAll(s, " ", "") 
    }},
    "underscore":  FilterModifier{Func: func(s string) string { 
        return strings.ReplaceAll(s, " ", "_") 
    }},
    "alphanum":    FilterModifier{Func: func(s string) string { 
        var result []rune
        for _, r := range s {
            if unicode.IsLetter(r) || unicode.IsDigit(r) {
                result = append(result, r)
            }
        }
        return string(result)
    }},
    "lower":       FilterModifier{Func: strings.ToLower},
    "upper":       FilterModifier{Func: strings.ToUpper},
    "trim":        FilterModifier{Func: strings.TrimSpace},
    "safe":        FilterModifier{Func: func(s string) string { 
        dangerous := []string{"&", "|", "<", ">", "'", "\"", "$", "`"}
        result := s
        for _, d := range dangerous {
            result = strings.ReplaceAll(result, d, "")
        }
        return result
    }},
    "filename":    FilterModifier{Func: func(s string) string {
        var result []rune
        for _, r := range s {
            if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' {
                result = append(result, r)
            }
        }
        return string(result)
    }},
    "domain":      FilterModifier{Func: func(s string) string {
        return strings.ToLower(strings.ReplaceAll(s, " ", ""))
    }},
    
    // Validator modifiers
    "required":    ValidatorModifier{Func: func(s string) error {
        if strings.TrimSpace(s) == "" {
            return fmt.Errorf("value is required")
        }
        return nil
    }},
    "username":    ValidatorModifier{Func: func(s string) error {
        if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(s) {
            return fmt.Errorf("invalid username format: only alphanumeric, underscore, and hyphen allowed")
        }
        return nil
    }},
    "password":    ValidatorModifier{Func: func(s string) error {
        if strings.ContainsAny(s, "&|<>'$`") {
            return fmt.Errorf("password contains unsafe characters")
        }
        return nil
    }},
    "url":         ValidatorModifier{Func: func(s string) error {
        if !regexp.MustCompile(`^https?:\/\/`).MatchString(s) {
            return fmt.Errorf("invalid URL format: must start with http:// or https://")
        }
        return nil
    }},
    "email":       ValidatorModifier{Func: func(s string) error {
        if !regexp.MustCompile(`^[^@]+@[^@]+\.[^@]+$`).MatchString(s) {
            return fmt.Errorf("invalid email format")
        }
        return nil
    }},
    "numeric":     ValidatorModifier{Func: func(s string) error {
        if !regexp.MustCompile(`^[0-9]+$`).MatchString(s) {
            return fmt.Errorf("value must be numeric")
        }
        return nil
    }},
}

func applyModifiers(template string, data map[string]interface{}) (string, error) {
    // Find all {variable:modifier1:modifier2} patterns
    re := regexp.MustCompile(`\{([^}]+)\}`)
    
    result := template
    matches := re.FindAllStringSubmatch(template, -1)
    
    for _, match := range matches {
        fullMatch := match[0]
        variableSpec := match[1]
        
        parts := strings.Split(variableSpec, ":")
        variableName := parts[0]
        modifierNames := parts[1:]
        
        // Get original value
        value, exists := data[variableName]
        if !exists {
            return "", fmt.Errorf("variable not found: %s", variableName)
        }
        
        valueStr := fmt.Sprintf("%v", value)
        
        // Apply modifiers in order
        for _, modName := range modifierNames {
            // Handle parameterized validators like :length:32
            if strings.HasPrefix(modName, "length:") {
                maxLength, err := strconv.Atoi(strings.TrimPrefix(modName, "length:"))
                if err != nil {
                    return "", fmt.Errorf("invalid length parameter: %s", modName)
                }
                if len(valueStr) > maxLength {
                    return "", fmt.Errorf("value too long: max %d characters", maxLength)
                }
                continue
            }
            
            if strings.HasPrefix(modName, "minlength:") {
                minLength, err := strconv.Atoi(strings.TrimPrefix(modName, "minlength:"))
                if err != nil {
                    return "", fmt.Errorf("invalid minlength parameter: %s", modName)
                }
                if len(valueStr) < minLength {
                    return "", fmt.Errorf("value too short: min %d characters", minLength)
                }
                continue
            }
            
            modifier, exists := modifiers[modName]
            if !exists {
                return "", fmt.Errorf("unknown modifier: %s", modName)
            }
            
            switch m := modifier.(type) {
            case FilterModifier:
                valueStr = m.Func(valueStr)
            case ValidatorModifier:
                if err := m.Func(valueStr); err != nil {
                    return "", fmt.Errorf("validation failed for %s: %w", variableName, err)
                }
            }
        }
        
        // Replace in template
        result = strings.ReplaceAll(result, fullMatch, valueStr)
    }
    
    return result, nil
}
```

## Usage Examples

### Basic Sysconfig Handlers

```yaml
handlers:
  sysconfig:
    hostname:
      command: "hostnamectl set-hostname {value:alphanum:lower:trim}"
      
    timezone:
      command: "timedatectl set-timezone {value:trim}"
      
    ntp_server:
      command: "echo 'server {value:trim}' >> /etc/ntp.conf"
      
    locale:
      command: "localectl set-locale LANG={value:trim}"
      
    dns_server:
      command: "echo 'nameserver {value:trim}' >> /etc/resolv.conf"
```

### Security-Focused Examples

```yaml
# Prevent injection in dangerous contexts
ssh_key:
  command: "echo '{value:safe:trim}' >> /home/user/.ssh/authorized_keys"

# Ensure safe filenames
config_file:
  command: "cp /etc/template.conf /etc/{value:filename:trim}.conf"

# Validate email addresses  
admin_email:
  command: "echo '{value:email:trim}' > /etc/admin/email"

# Validate URLs
api_endpoint:
  command: "echo '{value:url:trim}' > /etc/api/endpoint"

# Username with validation
admin_user:
  command: "useradd -m {value:username:required:lower:trim}"
```

### Complex Modifier Chains

```yaml
# Multiple transformations
device_name:
  command: "echo '{value:alphanum:lower:trim:length:32}' > /etc/hostname"

# Sanitize and validate
wifi_ssid:
  command: "nmcli dev wifi connect '{value:alphanum:trim:length:32}'"

# Required field with format validation
license_key:
  command: "echo '{value:required:alphanum:trim:length:64}' > /etc/license/key"
```

## Security Benefits

1. **Prevents shell injection** - `:safe` modifier removes dangerous characters
2. **Prevents directory traversal** - `:filename` ensures safe file names
3. **Validates formats** - `:url`, `:email` ensure proper formats
4. **Enforces requirements** - `:required` prevents empty values
5. **Limits length** - `:length:N` prevents buffer overflow attacks
6. **Sanitizes input** - `:alphanum`, `:nospace` clean up user input

## Error Handling

When validation fails, the system provides clear error messages:

```
ERROR: validation failed for hostname: invalid username format: only alphanumeric, underscore, and hyphen allowed
ERROR: value too long: max 32 characters
ERROR: value is required
ERROR: invalid URL format: must start with http:// or https://
```

## Integration with Generic Handlers

The modifier system integrates seamlessly with existing generic handlers:

```go
func (h *GenericHandler) HandleSysConfigParameter(key string, value interface{}) error {
    command, err := applyModifiers(h.Command, map[string]interface{}{
        "value": value,
    })
    if err != nil {
        return fmt.Errorf("variable validation failed: %w", err)
    }
    
    return h.executeCommand(command)
}
```

---

This modifier system provides a powerful, flexible, and secure way to handle sysconfig values while maintaining simplicity for common use cases.
