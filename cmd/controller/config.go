package main

import (
	"fmt"

	"github.com/gordonklaus/portaudio"
	"github.com/rotisserie/eris"

	"github.com/cybre/yeelight-music-sync/internal/ui"
	"github.com/cybre/yeelight-music-sync/internal/yeelight"
)

func selectBulbAndDevice(
	bulbs []*yeelight.Bulb,
	devices []*portaudio.DeviceInfo,
	defaultDeviceIndex int,
	opts runtimeOptions,
) (*yeelight.Bulb, *portaudio.DeviceInfo, error) {
	if len(bulbs) == 0 {
		return nil, nil, eris.New("no bulbs available")
	}
	if len(devices) == 0 {
		return nil, nil, eris.New("no input devices available")
	}

	var (
		selectedBulb   *yeelight.Bulb
		selectedDevice *portaudio.DeviceInfo
		deviceIndex    = -1
	)

	if opts.bulbAddr != "" {
		selectedBulb = bulbs[0]
	}
	if opts.deviceIndex >= 0 {
		if opts.deviceIndex >= len(devices) {
			return nil, nil, eris.Errorf("invalid device index %d", opts.deviceIndex)
		}
		selectedDevice = devices[opts.deviceIndex]
		deviceIndex = opts.deviceIndex
	}

	needBulb := selectedBulb == nil
	needDevice := selectedDevice == nil

	if !needBulb && !needDevice {
		return selectedBulb, selectedDevice, nil
	}

	initialBulb := 0
	initialDevice := effectiveInitialDeviceIndex(deviceIndex, defaultDeviceIndex, len(devices))

	bulbOptions := buildBulbOptions(bulbs)
	deviceOptions := buildDeviceOptions(devices)

	result, err := ui.RunSetup(
		bulbOptions,
		deviceOptions,
		ui.SetupConfig{
			RequireBulb:   needBulb,
			RequireDevice: needDevice,
			InitialBulb:   0,
			InitialDevice: initialDevice,
		},
	)
	if err != nil {
		if eris.Is(err, ui.ErrNoInteractiveTTY) {
			if needBulb {
				selectedBulb = bulbs[initialBulb]
			}
			if needDevice {
				selectedDevice = devices[initialDevice]
			}
			return selectedBulb, selectedDevice, nil
		}
		return nil, nil, err
	}

	selectedBulb = bulbs[result.BulbIndex]
	if needDevice {
		selectedDevice = devices[result.DeviceIndex]
	}

	return selectedBulb, selectedDevice, nil
}

func buildBulbOptions(bulbs []*yeelight.Bulb) []ui.Option {
	options := make([]ui.Option, len(bulbs))
	for i, bulb := range bulbs {
		options[i] = ui.Option{
			Label: describeBulb(bulb),
		}
	}
	return options
}

func describeBulb(bulb *yeelight.Bulb) string {
	name := bulb.Name()
	if name == "" {
		name = "Yeelight"
	}
	id := bulb.ID()
	if id == "" {
		id = "n/a"
	}
	model := bulb.Model()
	if model == "" {
		model = "n/a"
	}
	fw := bulb.FirmwareVersion()
	if fw == "" {
		fw = "n/a"
	}

	return fmt.Sprintf("%s [%s] · model:%s · fw:%s · %s",
		name,
		id,
		model,
		fw,
		bulb.Addr(),
	)
}

func buildDeviceOptions(devices []*portaudio.DeviceInfo) []ui.Option {
	options := make([]ui.Option, len(devices))
	for i, dev := range devices {
		options[i] = ui.Option{
			Label: fmt.Sprintf(
				"[%d] %s · %.0fHz · in:%d · latency:%.1fms",
				i,
				dev.Name,
				dev.DefaultSampleRate,
				dev.MaxInputChannels,
				dev.DefaultLowInputLatency.Seconds()*1000,
			),
		}
	}
	return options
}

func effectiveInitialDeviceIndex(requested, fallback, length int) int {
	if length == 0 {
		return 0
	}
	if requested >= 0 && requested < length {
		return requested
	}
	if fallback >= 0 && fallback < length {
		return fallback
	}
	return 0
}

func buildLoopConfig(bulb *yeelight.Bulb, device *portaudio.DeviceInfo, opts runtimeOptions) loopConfig {
	return loopConfig{
		Bulb:       bulb,
		Device:     device,
		SampleRate: effectiveSampleRate(opts.sampleRate, device.DefaultSampleRate),
		FrameSize:  effectiveFrameSize(opts.frameSize),
		Channels:   sanitizeChannelCount(opts.channels, int(device.MaxInputChannels)),
		Latency:    opts.latency,
		Visualize:  opts.visualize,
	}
}

func sanitizeChannelCount(requested, max int) int {
	if requested <= 0 {
		return 1
	}

	if max > 0 && requested > max {
		return max
	}

	return requested
}

func effectiveSampleRate(requested, deviceDefault float64) float64 {
	if requested > 0 {
		return requested
	}

	if deviceDefault > 0 {
		return deviceDefault
	}

	return 44100
}

func effectiveFrameSize(requested int) int {
	if requested > 0 {
		return requested
	}

	return 1024
}
