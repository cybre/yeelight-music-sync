package yeelight

import (
	"net/netip"

	"github.com/cybre/yeelight-music-sync/internal/utils"
	"github.com/rotisserie/eris"
)

var (
	ErrPoweredOff        = eris.New("tried to execute command on a bulb that is powered off")
	ErrBrightnessInvalid = eris.New("brightness must be between 1 and 100")
)

type ColorMode uint8

const (
	ColorModeRGB ColorMode = iota + 1
	ColorModeTemperature
	ColorModeHSV
)

type PowerStatus string

const (
	PowerOn  PowerStatus = "on"
	PowerOff PowerStatus = "off"
)

type Effect string

const (
	Sudden Effect = "sudden"
	Smooth Effect = "smooth"
)

type bulbInfo struct {
	addr             netip.AddrPort
	id               string
	name             string
	model            string
	firmwareVersion  string
	support          []string
	power            PowerStatus
	brightness       uint8
	colorMode        ColorMode
	colorTemperature uint16
	rgb              uint
	hue              uint16
	saturation       uint8
}

func (bi bulbInfo) Addr() netip.AddrPort {
	return bi.addr
}

func (bi bulbInfo) ID() string {
	return bi.id
}

func (bi bulbInfo) Name() string {
	return bi.name
}

func (bi bulbInfo) Model() string {
	return bi.model
}

func (bi bulbInfo) FirmwareVersion() string {
	return bi.firmwareVersion
}

func (bi bulbInfo) Support() []string {
	return bi.support
}

func (bi bulbInfo) Power() PowerStatus {
	return bi.power
}

func (bi bulbInfo) Brightness() uint8 {
	return bi.brightness
}

func (bi bulbInfo) ColorMode() ColorMode {
	return bi.colorMode
}

func (bi bulbInfo) ColorTemperature() uint16 {
	return bi.colorTemperature
}

func (bi bulbInfo) RGB() (uint8, uint8, uint8) {
	return utils.IntToRGB(bi.rgb)
}

func (bi bulbInfo) Hue() uint16 {
	return bi.hue
}

func (bi bulbInfo) Saturation() uint8 {
	return bi.saturation
}
