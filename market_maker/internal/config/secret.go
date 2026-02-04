package config

// Secret is a string type that redacts itself when printed
type Secret string

func (s Secret) String() string {
	if s == "" {
		return ""
	}
	return "[REDACTED]"
}

// MarshalJSON ensures secrets are redacted when marshaled to JSON
func (s Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"[REDACTED]"`), nil
}

// GormValue ensures secrets are redacted when logging SQL queries (if Gorm is used)
func (s Secret) GormValue(ctx interface{}, db interface{}) interface{} {
	return "[REDACTED]"
}
