package config

// Secret is a string type that redacts itself when printed
type Secret string

func (s Secret) String() string {
	if s == "" {
		return ""
	}
	return "[REDACTED]"
}

// MarshalYAML ensures secrets are redacted when marshaled to YAML
func (s Secret) MarshalYAML() (interface{}, error) {
	if s == "" {
		return "", nil
	}
	return "[REDACTED]", nil
}

// MarshalJSON ensures secrets are redacted when marshaled to JSON
func (s Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"[REDACTED]"`), nil
}

// GormValue ensures secrets are redacted when logging SQL queries (if Gorm is used)
func (s Secret) GormValue(ctx interface{}, db interface{}) interface{} {
	return "[REDACTED]"
}

// GoString ensures secrets are redacted when using %#v format
func (s Secret) GoString() string {
	if s == "" {
		return `""`
	}
	return `"[REDACTED]"`
}
