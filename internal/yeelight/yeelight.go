package yeelight

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/rotisserie/eris"
)

const (
	// yeelight discover message for SSDP
	discoverMSG = "M-SEARCH * HTTP/1.1\r\n HOST:239.255.255.250:1982\r\n MAN:\"ssdp:discover\"\r\n ST:wifi_bulb\r\n"
	// timeout value for TCP and UDP commands
	timeout = time.Second * 3
	// SSDP discover address
	ssdpAddress = "239.255.255.250:1982"
	// line ending (CRLF)
	lineEnding = "\r\n"
	// default TCP port
	defaultBulbPort = 55443
)

func NewBulbFromAddress(address string) (*Bulb, error) {
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, strconv.Itoa(defaultBulbPort))
	}

	addr, err := netip.ParseAddrPort(address)
	if err != nil {
		return nil, eris.Wrap(err, "failed to parse bulb address")
	}

	return newBulb(addr), nil
}

func Discover(ctx context.Context) ([]*Bulb, error) {
	bulbs := make([]*Bulb, 0)

	udpAddr, err := net.ResolveUDPAddr("udp4", ssdpAddress)
	if err != nil {
		return nil, eris.Wrap(err, "failed to resolve SSDP address")
	}

	conn, err := net.ListenUDP("udp4", udpAddr)
	if err != nil {
		return nil, eris.Wrap(err, "failed to establish connection to SSDP address")
	}

	if _, err = conn.WriteToUDP([]byte(discoverMSG), udpAddr); err != nil {
		return nil, eris.Wrap(err, "failed to write discover message to SSDP address")
	}

	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, eris.Wrap(err, "failed to set read deadline for SSDP connection")
	}

	buf := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, eris.Wrap(err, "failed to read from SSDP connection")
	}

	resp := string(buf[:n])
	for line := range strings.SplitSeq(resp, lineEnding) {
		if address, found := strings.CutPrefix(line, "Location: yeelight://"); found {
			addr, err := netip.ParseAddrPort(address)
			if err != nil {
				return nil, eris.Wrap(err, "failed to parse bulb address")
			}
			bulbs = append(bulbs, newBulb(addr))
			continue
		}

		if id, found := strings.CutPrefix(line, "id: "); found {
			bulbs[len(bulbs)-1].id = id
			continue
		}

		if support, found := strings.CutPrefix(line, "support: "); found {
			bulbs[len(bulbs)-1].support = strings.Split(support, " ")
			continue
		}

		if power, found := strings.CutPrefix(line, "power: "); found {
			bulbs[len(bulbs)-1].power = PowerStatus(power)
			continue
		}

		if brightness, found := strings.CutPrefix(line, "bright: "); found {
			brightnessInt, err := strconv.ParseUint(brightness, 10, 8)
			if err != nil {
				return nil, eris.Wrap(err, "failed to convert brightness to uint8")
			}
			bulbs[len(bulbs)-1].brightness = uint8(brightnessInt)
			continue
		}

		if colorMode, found := strings.CutPrefix(line, "color_mode: "); found {
			colorModeInt, err := strconv.ParseUint(colorMode, 10, 8)
			if err != nil {
				return nil, eris.Wrap(err, "failed to convert color mode to uint8")
			}
			bulbs[len(bulbs)-1].colorMode = ColorMode(colorModeInt)
			continue
		}

		if colorTemperature, found := strings.CutPrefix(line, "ct: "); found {
			colorTemperatureInt, err := strconv.ParseUint(colorTemperature, 10, 16)
			if err != nil {
				return nil, eris.Wrap(err, "failed to convert color temperature to uint16")
			}
			bulbs[len(bulbs)-1].colorTemperature = uint16(colorTemperatureInt)
			continue
		}

		if rgb, found := strings.CutPrefix(line, "rgb: "); found {
			rgbInt, err := strconv.ParseUint(rgb, 10, 32)
			if err != nil {
				return nil, eris.Wrap(err, "failed to convert RGB to uint32")
			}
			bulbs[len(bulbs)-1].rgb = uint(rgbInt)
			continue
		}

		if hue, found := strings.CutPrefix(line, "hue: "); found {
			hueInt, err := strconv.ParseUint(hue, 10, 16)
			if err != nil {
				return nil, eris.Wrap(err, "failed to convert hue to uint16")
			}
			bulbs[len(bulbs)-1].hue = uint16(hueInt)
			continue
		}

		if saturation, found := strings.CutPrefix(line, "sat: "); found {
			saturationInt, err := strconv.ParseUint(saturation, 10, 8)
			if err != nil {
				return nil, eris.Wrap(err, "failed to convert saturation to uint8")
			}
			bulbs[len(bulbs)-1].saturation = uint8(saturationInt)
			continue
		}

		if name, found := strings.CutPrefix(line, "name: "); found {
			bulbs[len(bulbs)-1].name = name
			continue
		}

		if model, found := strings.CutPrefix(line, "model: "); found {
			bulbs[len(bulbs)-1].model = model
			continue
		}

		if firmwareVersion, found := strings.CutPrefix(line, "fw_ver: "); found {
			bulbs[len(bulbs)-1].firmwareVersion = firmwareVersion
			continue
		}
	}

	return bulbs, nil
}
