package yeelight

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/crazy3lf/colorconv"
	"github.com/cybre/yeelight-music-sync/internal/utils"
	"github.com/rotisserie/eris"
)

type bulbBase struct {
	*bulbInfo

	conn          net.Conn
	lastCommandID int

	commandCallback func(context.Context, command) ([]string, error)
}

func (bb *bulbBase) Disconnect() error {
	return bb.conn.Close()
}

func (bb *bulbBase) TurnOn(ctx context.Context, effect Effect, duration int) error {
	if _, err := bb.executeCommand(ctx, "set_power", "on", effect, duration); err != nil {
		return err
	}

	bb.power = PowerOn

	return nil
}

func (bb *bulbBase) TurnOff(ctx context.Context, effect Effect, duration int) error {
	if _, err := bb.executeCommandBase(ctx, "set_power", "off", effect, duration); err != nil {
		return err
	}

	bb.power = PowerOff

	return nil
}

func (bb *bulbBase) Toggle(ctx context.Context, effect Effect, duration int) error {
	power := bb.power

	if _, err := bb.executeCommand(ctx, "toggle", effect, duration); err != nil {
		return err
	}

	if power == PowerOn {
		bb.power = PowerOff
	} else {
		bb.power = PowerOn
	}

	return nil
}

func (bb *bulbBase) SetBrightness(ctx context.Context, brightness uint8, effect Effect, duration int) error {
	if brightness < 1 || brightness > 100 {
		return eris.Wrap(ErrBrightnessInvalid, "failed to set brightness")
	}

	if _, err := bb.executeCommand(ctx, "set_bright", brightness, effect, duration); err != nil {
		return err
	}

	bb.brightness = brightness

	return nil
}

func (bb *bulbBase) SetRGB(ctx context.Context, r, g, b uint8, effect Effect, duration int) error {
	rgb := utils.RGBToInt(r, g, b)

	if _, err := bb.executeCommand(ctx, "set_rgb", rgb, effect, duration); err != nil {
		return err
	}

	bb.rgb = rgb

	return nil
}

func (bb *bulbBase) SetHSV(ctx context.Context, hue uint16, saturation uint8, value uint8, effect Effect, duration int) error {
	red, green, blue, err := colorconv.HSVToRGB(float64(hue), float64(saturation)/100.0, 1)
	if err != nil {
		return eris.Wrap(err, "failed to convert HSV to RGB")
	}

	rgb := utils.RGBToInt(red, green, blue)

	// Since set_hsv doesn't actually let you set the brightness (value), we have to use start_cf
	if _, err = bb.executeCommand(ctx, "start_cf", 1, 1, fmt.Sprintf("%d, 1, %d, %d", duration, rgb, value)); err != nil {
		return err
	}

	bb.hue = hue
	bb.saturation = saturation
	bb.brightness = value
	bb.rgb = rgb

	return nil
}

func (bb *bulbBase) StartColorFlow(ctx context.Context, count int, action int, expression string) error {
	_, err := bb.executeCommand(ctx, "start_cf", count, action, expression)
	return err
}

func (bb *bulbBase) StopColorFlow(ctx context.Context) error {
	_, err := bb.executeCommand(ctx, "stop_cf")
	return err
}

func (bb *bulbBase) executeCommand(ctx context.Context, method string, params ...any) ([]string, error) {
	command, err := bb.executeCommandBase(ctx, method, params...)
	if err != nil {
		return nil, err
	}

	return bb.commandCallback(ctx, command)
}

func (bb *bulbBase) executeCommandBase(ctx context.Context, method string, params ...any) (command, error) {
	select {
	case <-ctx.Done():
		return command{}, eris.Wrap(ctx.Err(), "failed to execute command")
	default:
	}

	cmd := newCommand(bb.getCommandID(), method, params...)
	comandText, err := cmd.String()
	if err != nil {
		return command{}, err
	}

	slog.Debug("executing command",
		slog.String("addr", bb.Addr().String()),
		slog.Int("id", cmd.ID),
		slog.String("method", method),
		slog.Any("params", cmd.Params),
		slog.String("command", comandText),
	)

	if _, err = bb.conn.Write([]byte(comandText)); err != nil {
		return command{}, eris.Wrap(err, "failed to write command to connection")
	}

	return cmd, nil
}

func (bb *bulbBase) getCommandID() int {
	bb.lastCommandID++

	return bb.lastCommandID
}
