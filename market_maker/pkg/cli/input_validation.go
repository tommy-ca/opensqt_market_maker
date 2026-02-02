package cli

import (
	"errors"
	"regexp"
	"strings"
)

// ValidateInput checks for potentially malicious input patterns
// following OpenCode safety guidelines
func ValidateInput(input string) error {
	// Check for command injection patterns
	if strings.Contains(input, ";") || strings.Contains(input, "&&") || strings.Contains(input, "||") {
		return errors.New("potentially malicious input detected")
	}

	// Check for path traversal
	if strings.Contains(input, "../") || strings.Contains(input, "..\\") {
		return errors.New("potentially malicious input detected")
	}

	// Check for SQL injection patterns (more specific)
	sqlPattern := regexp.MustCompile(`['"]\s*;\s*|\b(DROP|DELETE|UPDATE|INSERT)\b`)
	if sqlPattern.MatchString(strings.ToUpper(input)) {
		return errors.New("potentially malicious input detected")
	}

	// Additional checks can be added here

	return nil
}
