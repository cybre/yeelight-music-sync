package yeelight

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommandString(t *testing.T) {
	cmd := command{ID: 42, Method: "toggle", Params: []any{"on"}}
	str, err := cmd.String()
	assert.NoError(t, err)
	assert.True(t, strings.HasSuffix(str, lineEnding))
	assert.Contains(t, str, "\"id\":42")
}

func TestCommandStringError(t *testing.T) {
	cmd := command{ID: 1, Method: "test", Params: []any{make(chan int)}}
	_, err := cmd.String()
	assert.Error(t, err)
}
