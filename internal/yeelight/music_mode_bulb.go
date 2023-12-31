package yeelight

import (
	"context"
	"net"
)

type MusicModeBulb struct {
	bulbBase
}

func newMusicModeBulb(b *bulbInfo, conn net.Conn) *MusicModeBulb {
	return &MusicModeBulb{
		bulbBase: bulbBase{
			bulbInfo: b,
			conn:     conn,
			commandCallback: func(ctx context.Context, cmd command) ([]string, error) {
				return nil, nil
			},
		},
	}
}
