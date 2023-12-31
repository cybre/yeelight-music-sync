package yeelight

import (
	"encoding/json"

	"github.com/rotisserie/eris"
)

type command struct {
	ID     int    `json:"id"`
	Method string `json:"method"`
	Params []any  `json:"params"`
}

func newCommand(id int, method string, params ...any) command {
	return command{
		ID:     id,
		Method: method,
		Params: params,
	}
}

func (c *command) String() (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", eris.Wrap(err, "failed to marshal bulb command")
	}

	return string(b) + lineEnding, nil
}
