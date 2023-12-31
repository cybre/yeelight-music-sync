package yeelight

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/rotisserie/eris"
)

const (
	propertyPollInterval   = 2 * time.Second
	commandResponseTimeout = 3 * time.Second
)

type commandError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *commandError) Error() string {
	return fmt.Sprintf("%s (%d)", e.Message, e.Code)
}

type commandResult struct {
	ID     int           `json:"id"`
	Result []string      `json:"result"`
	Error  *commandError `json:"error"`
}

type notification struct {
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

type Bulb struct {
	bulbBase
	results            chan commandResult
	musicContextCancel context.CancelFunc
}

func newBulb(addr netip.AddrPort) *Bulb {
	results := make(chan commandResult)
	return &Bulb{
		bulbBase: bulbBase{
			bulbInfo: &bulbInfo{
				addr: addr,
			},
			commandCallback: getCommandExecutionCallback(results, commandResponseTimeout),
		},
		results: results,
	}
}

func (bb *Bulb) Connect(ctx context.Context) error {
	conn, err := net.Dial("tcp", bb.Addr().String())
	if err != nil {
		return eris.Wrap(err, "failed to connect connect to bulb")
	}

	bb.conn = conn

	bb.listen(ctx)

	return nil
}

func (bb *Bulb) EnableMusicMode(ctx context.Context, port uint16, callback func(context.Context, *MusicModeBulb) error) error {
	localAddr := bb.conn.LocalAddr()
	if localAddr == nil {
		return eris.New("bulb is not connected")
	}

	splitAddr := strings.Split(localAddr.String(), ":")
	if len(splitAddr) != 2 {
		return eris.New("invalid local address")
	}

	ip := splitAddr[0]

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		return eris.Wrap(err, "failed to start music mode listener")
	}
	defer ln.Close()

	if _, err = bb.executeCommand(ctx, "set_music", 1, ip, port); err != nil {
		return err
	}

	conn, err := ln.Accept()
	if err != nil {
		return eris.Wrap(err, "failed to accept connection from bulb")
	}

	musicContext, musicContextCancel := context.WithCancel(ctx)
	bb.musicContextCancel = musicContextCancel

	bulb := newMusicModeBulb(bb.bulbInfo, conn)
	defer func() {
		if bb.musicContextCancel != nil {
			bb.musicContextCancel()
			bb.musicContextCancel = nil
		}
		if err := bb.DisableMusicMode(context.Background()); err != nil {
			slog.Error("failed to disable music mode", slog.Any("error", err))
		} else {
			slog.Info("music mode disabled")
		}
	}()

	return callback(musicContext, bulb)
}

func (bb *Bulb) DisableMusicMode(ctx context.Context) error {
	if bb.musicContextCancel != nil {
		bb.musicContextCancel()
		bb.musicContextCancel = nil
	}

	_, err := bb.executeCommand(ctx, "set_music", 0)

	return err
}

func (bb *Bulb) listen(ctx context.Context) {
	addr := bb.Addr().String()

	go bb.pollProperties(ctx, addr)
	go bb.readMessages(ctx, addr)
}

func (bb *Bulb) pollProperties(ctx context.Context, addr string) {
	ticker := time.NewTicker(propertyPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			bb.refreshProperties(ctx, addr)
		}
	}
}

func (bb *Bulb) refreshProperties(ctx context.Context, addr string) {
	props, err := bb.executeCommand(ctx, "get_prop", "power", "bright", "color_mode", "ct", "rgb", "hue", "sat", "name")
	if err != nil {
		if !eris.Is(err, context.Canceled) {
			slog.Error("failed to get bulb props",
				slog.String("addr", addr),
				slog.Any("error", err),
			)
		}
		return
	}

	bb.updatePropertiesFromSlice(props, addr)
}

func (bb *Bulb) readMessages(ctx context.Context, addr string) {
	buf := make([]byte, 1024)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := bb.conn.Read(buf)
		if err != nil {
			if eris.Is(err, net.ErrClosed) {
				return
			}

			slog.Error("failed to read data from bulb connection",
				slog.String("addr", addr),
				slog.Any("error", err),
			)
			return
		}

		bb.handleIncomingPayload(string(buf[:n]), addr)
	}
}

func (bb *Bulb) handleIncomingPayload(payload, addr string) {
	slog.Debug("received response from bulb",
		slog.String("addr", addr),
		slog.String("response", payload),
	)

	for raw := range strings.SplitSeq(payload, lineEnding) {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		bb.handleMessageLine(line, addr)
	}
}

func (bb *Bulb) handleMessageLine(line, addr string) {
	switch {
	case strings.HasPrefix(line, "{\"id\":"):
		bb.decodeCommandResult(line, addr)
	case strings.HasPrefix(line, "{\"method\":"):
		bb.decodeNotification(line, addr)
	}
}

func (bb *Bulb) decodeCommandResult(line, addr string) {
	var result commandResult
	if err := json.Unmarshal([]byte(line), &result); err != nil {
		slog.Error("failed to unmarshal command result",
			slog.String("addr", addr),
			slog.String("json", line),
			slog.Any("error", err),
		)
		return
	}

	bb.results <- result
}

func (bb *Bulb) decodeNotification(line, addr string) {
	var note notification
	if err := json.Unmarshal([]byte(line), &note); err != nil {
		slog.Error("failed to unmarshal notification",
			slog.String("addr", addr),
			slog.String("json", line),
			slog.Any("error", err),
		)
		return
	}

	bb.handleNotification(note, addr)
}

func (bb *Bulb) handleNotification(note notification, addr string) {
	switch note.Method {
	case "props":
		bb.applyPropertyNotification(note.Params, addr)
	}
}

func (bb *Bulb) applyPropertyNotification(params map[string]any, addr string) {
	for key, value := range params {
		switch key {
		case "power":
			if s, ok := value.(string); ok {
				bb.power = PowerStatus(s)
			} else {
				bb.logUnexpectedType("power", value, addr)
			}
		case "bright":
			if v, ok := asFloat64(value); ok {
				bb.brightness = uint8(v)
			} else {
				bb.logUnexpectedType("bright", value, addr)
			}
		case "color_mode":
			if v, ok := asFloat64(value); ok {
				bb.colorMode = ColorMode(int(v))
			} else {
				bb.logUnexpectedType("color_mode", value, addr)
			}
		case "ct":
			if v, ok := asFloat64(value); ok {
				bb.colorTemperature = uint16(v)
			} else {
				bb.logUnexpectedType("ct", value, addr)
			}
		case "rgb":
			if v, ok := asFloat64(value); ok {
				bb.rgb = uint(v)
			} else {
				bb.logUnexpectedType("rgb", value, addr)
			}
		case "hue":
			if v, ok := asFloat64(value); ok {
				bb.hue = uint16(v)
			} else {
				bb.logUnexpectedType("hue", value, addr)
			}
		case "sat":
			if v, ok := asFloat64(value); ok {
				bb.saturation = uint8(v)
			} else {
				bb.logUnexpectedType("sat", value, addr)
			}
		case "name":
			if s, ok := value.(string); ok {
				bb.name = s
			} else {
				bb.logUnexpectedType("name", value, addr)
			}
		}
	}
}

func (bb *Bulb) updatePropertiesFromSlice(props []string, addr string) {
	for i, prop := range props {
		switch i {
		case 0:
			bb.power = PowerStatus(prop)
		case 1:
			if v, ok := parseUint(prop, 10, 8, "brightness", addr); ok {
				bb.brightness = uint8(v)
			}
		case 2:
			if v, ok := parseUint(prop, 10, 8, "color mode", addr); ok {
				bb.colorMode = ColorMode(v)
			}
		case 3:
			if v, ok := parseUint(prop, 10, 16, "color temperature", addr); ok {
				bb.colorTemperature = uint16(v)
			}
		case 4:
			if v, ok := parseUint(prop, 10, 32, "RGB", addr); ok {
				bb.rgb = uint(v)
			}
		case 5:
			if v, ok := parseUint(prop, 10, 16, "hue", addr); ok {
				bb.hue = uint16(v)
			}
		case 6:
			if v, ok := parseUint(prop, 10, 8, "saturation", addr); ok {
				bb.saturation = uint8(v)
			}
		case 7:
			bb.name = prop
		}
	}
}

func (bb *Bulb) logUnexpectedType(field string, value any, addr string) {
	slog.Warn("unexpected notification field type",
		slog.String("addr", addr),
		slog.String("field", field),
		slog.String("type", fmt.Sprintf("%T", value)),
	)
}

func parseUint(value string, base, bitSize int, field, addr string) (uint64, bool) {
	v, err := strconv.ParseUint(value, base, bitSize)
	if err != nil {
		slog.Warn("failed to convert "+field+" to int",
			slog.String("addr", addr),
			slog.Any("error", err),
		)
		return 0, false
	}

	return v, true
}

func asFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	default:
		return 0, false
	}
}

func getCommandExecutionCallback(results <-chan commandResult, wait time.Duration) func(context.Context, command) ([]string, error) {
	return func(ctx context.Context, cmd command) ([]string, error) {
		select {
		case result := <-results:
			if result.ID == cmd.ID {
				if result.Error != nil {
					return nil, eris.Wrapf(result.Error, "failed to execute command %s (%v)", cmd.Method, cmd.Params)
				}

				if len(result.Result) == 1 && result.Result[0] == "ok" {
					return nil, nil
				}

				return result.Result, nil
			}
		case <-time.After(wait):
			return nil, eris.New("command timed out")
		case <-ctx.Done():
			return nil, eris.Wrapf(ctx.Err(), "failed to execute command %s (%v)", cmd.Method, cmd.Params)
		}

		return nil, nil
	}
}
