package main

import (
	"time"

	"github.com/cybre/yeelight-music-sync/internal/yeelight"
	"github.com/gordonklaus/portaudio"
	"github.com/rotisserie/eris"
)

type runtimeOptions struct {
	bulbID      string
	deviceIndex int
	sampleRate  float64
	frameSize   int
	channels    int
	latency     time.Duration
	visualize   bool
}

func selectBulb(bulbs []*yeelight.Bulb, bulbID string) (*yeelight.Bulb, error) {
	if len(bulbs) == 0 {
		return nil, eris.New("no bulbs available")
	}

	if bulbID == "" {
		return bulbs[0], nil
	}

	for _, bulb := range bulbs {
		if bulb.ID() == bulbID {
			return bulb, nil
		}
	}

	return nil, eris.Errorf("bulb with ID:%s not found", bulbID)
}

func selectDevice(devices []*portaudio.DeviceInfo, defaultIndex, requestedIndex int) (*portaudio.DeviceInfo, error) {
	if len(devices) == 0 {
		return nil, eris.New("no input devices available")
	}

	index := requestedIndex
	if index < 0 {
		index = defaultIndex
	}

	if index < 0 || index >= len(devices) {
		return nil, eris.Errorf("invalid device index %d", index)
	}

	return devices[index], nil
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
