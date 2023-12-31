package main

import (
	"flag"
	"time"
)

type runtimeOptions struct {
	bulbAddr    string
	deviceIndex int
	sampleRate  float64
	frameSize   int
	channels    int
	latency     time.Duration
	visualize   bool
	debug       bool
}

func parseCLIFlags() runtimeOptions {
	var (
		cfg       runtimeOptions
		latencyMs int
	)

	flag.StringVar(&cfg.bulbAddr, "bulb", "", "yeelight bulb address (ip[:port], default port 55443)")
	flag.IntVar(&cfg.deviceIndex, "device", -1, "audio input device index (leave blank to choose interactively)")
	flag.Float64Var(&cfg.sampleRate, "sample-rate", 0, "capture sample rate (0 = device default)")
	flag.IntVar(&cfg.frameSize, "frame-size", 1024, "analysis frame size in samples")
	flag.IntVar(&cfg.channels, "channels", 2, "number of input channels to capture (<= device max)")
	flag.IntVar(&latencyMs, "latency-ms", 0, "override input latency in milliseconds (0 = device default)")
	flag.BoolVar(&cfg.debug, "debug", false, "enable debug logging")
	flag.BoolVar(&cfg.visualize, "visualize", false, "render realtime ASCII visualization (logs go to stderr)")
	flag.Parse()

	cfg.latency = time.Duration(latencyMs) * time.Millisecond

	return cfg
}
