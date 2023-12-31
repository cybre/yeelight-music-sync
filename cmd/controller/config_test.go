package main

import (
	"testing"
	"time"

	"github.com/cybre/yeelight-music-sync/internal/yeelight"
	"github.com/gordonklaus/portaudio"
	"github.com/stretchr/testify/assert"
)

func TestSelectDevice(t *testing.T) {
	devices := []*portaudio.DeviceInfo{
		{Index: 0, Name: "dev0"},
		{Index: 1, Name: "dev1"},
	}

	t.Run("requested index", func(t *testing.T) {
		dev, err := selectDevice(devices, 0, 1)
		assert.NoError(t, err)
		assert.Equal(t, devices[1], dev)
	})

	t.Run("default index", func(t *testing.T) {
		dev, err := selectDevice(devices, 1, -1)
		assert.NoError(t, err)
		assert.Equal(t, devices[1], dev)
	})

	t.Run("invalid index", func(t *testing.T) {
		_, err := selectDevice(devices, 0, 5)
		assert.Error(t, err)
	})

	t.Run("no devices", func(t *testing.T) {
		_, err := selectDevice(nil, 0, 0)
		assert.Error(t, err)
	})
}

func TestBuildLoopConfig(t *testing.T) {
	bulb := &yeelight.Bulb{}
	device := &portaudio.DeviceInfo{
		Index:                  3,
		Name:                   "test",
		MaxInputChannels:       2,
		DefaultSampleRate:      0,
		DefaultLowInputLatency: 25 * time.Millisecond,
	}

	opts := runtimeOptions{
		deviceIndex: 0,
		sampleRate:  0,
		frameSize:   0,
		channels:    4,
		latency:     10 * time.Millisecond,
		visualize:   true,
	}

	cfg := buildLoopConfig(bulb, device, opts)

	assert.Equal(t, device, cfg.Device)
	assert.Equal(t, 44100.0, cfg.SampleRate)
	assert.Equal(t, 1024, cfg.FrameSize)
	assert.Equal(t, 2, cfg.Channels)
	assert.Equal(t, opts.latency, cfg.Latency)
	assert.True(t, cfg.Visualize)
}

func TestSanitizeChannelCount(t *testing.T) {
	assert.Equal(t, 1, sanitizeChannelCount(-1, 2))
	assert.Equal(t, 2, sanitizeChannelCount(5, 2))
	assert.Equal(t, 2, sanitizeChannelCount(2, 4))
}

func TestEffectiveSampleRate(t *testing.T) {
	assert.Equal(t, 48000.0, effectiveSampleRate(48000, 44100))
	assert.Equal(t, 32000.0, effectiveSampleRate(0, 32000))
	assert.Equal(t, 44100.0, effectiveSampleRate(0, 0))
}

func TestEffectiveFrameSize(t *testing.T) {
	assert.Equal(t, 2048, effectiveFrameSize(2048))
	assert.Equal(t, 1024, effectiveFrameSize(0))
}
