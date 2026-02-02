package cli

import (
	"testing"
)

func TestValidateInput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid input",
			input:   "normal_command --flag value",
			wantErr: false,
		},
		{
			name:    "malicious command injection",
			input:   "ls; rm -rf /",
			wantErr: true,
			errMsg:  "potentially malicious input detected",
		},
		{
			name:    "path traversal attempt",
			input:   "../../../etc/passwd",
			wantErr: true,
			errMsg:  "potentially malicious input detected",
		},
		{
			name:    "sql injection attempt",
			input:   "'; DROP TABLE users; --",
			wantErr: true,
			errMsg:  "potentially malicious input detected",
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: false,
		},
		{
			name:    "input with spaces",
			input:   "command with spaces",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateInput(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateInput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" && err.Error() != tt.errMsg {
				t.Errorf("ValidateInput() error message = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}
