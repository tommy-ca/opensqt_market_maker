package config

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSecret_String(t *testing.T) {
	s := Secret("password123")
	assert.Equal(t, "[REDACTED]", s.String())

	empty := Secret("")
	assert.Equal(t, "", empty.String())
}

func TestSecret_GoString(t *testing.T) {
	s := Secret("password123")
	assert.Equal(t, "[REDACTED]", fmt.Sprintf("%#v", s))

	empty := Secret("")
	assert.Equal(t, "[REDACTED]", fmt.Sprintf("%#v", empty))
}

func TestSecret_MarshalJSON(t *testing.T) {
	s := Secret("password123")
	data, err := s.MarshalJSON()
	assert.NoError(t, err)
	assert.Equal(t, `"[REDACTED]"`, string(data))
}

func TestSecret_MarshalYAML(t *testing.T) {
	s := Secret("password123")
	val, err := s.MarshalYAML()
	assert.NoError(t, err)
	assert.Equal(t, "[REDACTED]", val)
}
