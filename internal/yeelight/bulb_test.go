package yeelight

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetCommandExecutionCallbackSuccess(t *testing.T) {
	results := make(chan commandResult, 1)
	callback := getCommandExecutionCallback(results, 50*time.Millisecond)

	cmd := command{ID: 1}
	results <- commandResult{ID: 1, Result: []string{"value"}}

	resp, err := callback(context.Background(), cmd)
	assert.NoError(t, err)
	assert.Equal(t, []string{"value"}, resp)
}

func TestGetCommandExecutionCallbackError(t *testing.T) {
	results := make(chan commandResult, 1)
	callback := getCommandExecutionCallback(results, 50*time.Millisecond)

	cmd := command{ID: 2, Method: "test", Params: []any{"a"}}
	results <- commandResult{
		ID:    2,
		Error: &commandError{Code: 500, Message: "boom"},
	}

	_, err := callback(context.Background(), cmd)
	assert.Error(t, err)
}

func TestGetCommandExecutionCallbackTimeout(t *testing.T) {
	results := make(chan commandResult)
	callback := getCommandExecutionCallback(results, 30*time.Millisecond)

	_, err := callback(context.Background(), command{ID: 3})
	assert.Error(t, err)
}
