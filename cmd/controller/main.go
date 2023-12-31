package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/rotisserie/eris"
	"golang.org/x/sync/errgroup"

	"github.com/cybre/yeelight-music-sync/internal/controller"
	"github.com/cybre/yeelight-music-sync/internal/dsp"
	"github.com/cybre/yeelight-music-sync/internal/patterns"
	"github.com/cybre/yeelight-music-sync/internal/ui"
	"github.com/cybre/yeelight-music-sync/internal/yeelight"
)

type loopConfig struct {
	Bulb       *yeelight.Bulb
	Device     *portaudio.DeviceInfo
	SampleRate float64
	FrameSize  int
	Channels   int
	Latency    time.Duration
	Visualize  bool
}

var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

func main() {
	cfg := parseCLIFlags()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := runController(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func runController(ctx context.Context, cfg runtimeOptions) error {
	bulbs, err := resolveBulbs(ctx, cfg)
	if err != nil {
		return err
	}

	if err := portaudio.Initialize(); err != nil {
		return eris.Wrap(err, "initialize PortAudio")
	}
	defer portaudio.Terminate()

	devices, err := portaudio.Devices()
	if err != nil {
		return eris.Wrap(err, "enumerate audio devices")
	}

	defaultDevice, err := portaudio.DefaultInputDevice()
	if err != nil {
		return eris.Wrap(err, "resolve default audio input device")
	}

	logger := setupLogger(cfg.debug, cfg.visualize)

	bulb, device, err := selectBulbAndDevice(bulbs, devices, defaultDevice.Index, cfg)
	if err != nil {
		return eris.Wrap(err, "select bulb/device")
	}
	if device.MaxInputChannels < 1 {
		return eris.Errorf("device %s has no input channels; select a loopback/monitor device", device.Name)
	}

	loopCfg := buildLoopConfig(bulb, device, cfg)

	if cfg.channels > 0 && cfg.channels > int(device.MaxInputChannels) {
		logger.Warn("requested channels exceed device capabilities",
			slog.Int("requested", cfg.channels),
			slog.Int("max", int(device.MaxInputChannels)),
			slog.Int("using", loopCfg.Channels),
		)
	}

	if err := run(ctx, logger, loopCfg); err != nil && !eris.Is(err, context.Canceled) {
		logger.Error("audio reactive loop failed", slog.Any("error", err))
		return err
	}

	return nil
}

func setupLogger(debug, visualize bool) *slog.Logger {
	logOutput := os.Stdout
	logLevel := slog.LevelInfo
	if debug {
		logLevel = slog.LevelDebug
	}
	if visualize && !debug {
		logLevel = slog.LevelWarn
	}
	if visualize {
		logOutput = os.Stderr
	}

	logger := slog.New(slog.NewTextHandler(logOutput, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	return logger
}

func run(ctx context.Context, logger *slog.Logger, cfg loopConfig) error {
	logger.Info(
		"using yeelight bulb",
		slog.String("id", cfg.Bulb.ID()),
		slog.String("name", cfg.Bulb.Name()),
		slog.String("model", cfg.Bulb.Model()),
		slog.String("firmware_version", cfg.Bulb.FirmwareVersion()),
	)

	if err := cfg.Bulb.Connect(ctx); err != nil {
		return err
	}
	defer func(ctx context.Context) {
		if err := cfg.Bulb.TurnOff(ctx, yeelight.Smooth, 100); err != nil {
			logger.Warn("failed to turn off bulb", slog.Any("error", err))
		} else {
			logger.Info("bulb turned off")
		}
		time.Sleep(500 * time.Millisecond)
		if err := cfg.Bulb.Disconnect(); err != nil {
			logger.Warn("faield to disconnect from bulb", slog.Any("error", err))
		} else {
			logger.Info("bulb disconnected")
		}
	}(context.WithoutCancel(ctx))

	if cfg.Bulb.Power() != yeelight.PowerOn {
		if err := cfg.Bulb.TurnOn(ctx, yeelight.Smooth, 250); err != nil {
			logger.Warn("failed to turn on bulb", slog.Any("error", err))
		} else {
			logger.Info("bulb turned on")
		}
	}

	musicPort := randomMusicModePort()
	logger.Info("starting music mode", slog.Int("port", int(musicPort)))
	if err := cfg.Bulb.EnableMusicMode(ctx, musicPort, func(loopCtx context.Context, musicBulb *yeelight.MusicModeBulb) error {
		return runReactiveLoop(loopCtx, logger, musicBulb, cfg)
	}); err != nil {
		if eris.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	return nil
}

func resolveBulbs(ctx context.Context, cfg runtimeOptions) ([]*yeelight.Bulb, error) {
	if cfg.bulbAddr != "" {
		bulb, err := yeelight.NewBulbFromAddress(cfg.bulbAddr)
		if err != nil {
			return nil, eris.Wrap(err, "parse bulb address")
		}
		return []*yeelight.Bulb{bulb}, nil
	}

	bulbs, err := yeelight.Discover(ctx)
	if err != nil {
		return nil, err
	}
	if len(bulbs) == 0 {
		return nil, eris.New("no bulbs available")
	}
	return bulbs, nil
}

func runReactiveLoop(ctx context.Context, logger *slog.Logger, bulb *yeelight.MusicModeBulb, cfg loopConfig) error {
	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	frameCh := make(chan []float32, 32)
	featuresCh := make(chan dsp.Features, 32)
	analyzer := dsp.NewAnalyzer(cfg.SampleRate, cfg.FrameSize, dsp.DefaultBands())
	patternAnalyzer := patterns.NewAnalyzer(patterns.Options{})

	var viz *ui.Visualizer
	if cfg.Visualize {
		viz = ui.NewVisualizer(cancel)
		defer viz.Close()
	}

	ledCtrl := controller.NewLEDController(bulb, logger, viz)

	g, gctx := errgroup.WithContext(loopCtx)

	g.Go(func() error {
		defer close(frameCh)
		return captureAudio(gctx, logger, frameCh, cfg)
	})

	g.Go(func() error {
		defer close(featuresCh)
		var mono []float64
		for {
			select {
			case <-gctx.Done():
				return gctx.Err()
			case frame, ok := <-frameCh:
				if !ok {
					return nil
				}
				mono = dsp.ToMono(frame, cfg.Channels, mono)
				features := analyzer.Process(mono, time.Now())
				select {
				case featuresCh <- features:
				case <-gctx.Done():
					return gctx.Err()
				}
			}
		}
	})

	g.Go(func() error {
		return ledCtrl.Run(gctx, featuresCh, patternAnalyzer)
	})

	if err := g.Wait(); err != nil {
		if eris.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	return nil
}

func captureAudio(ctx context.Context, logger *slog.Logger, out chan []float32, cfg loopConfig) error {
	if cfg.Device == nil {
		return eris.New("audio device is not specified")
	}

	logger.Info("using audio input device",
		slog.String("name", cfg.Device.Name),
		slog.Float64("sample_rate", cfg.SampleRate),
		slog.Int("channels", cfg.Channels),
		slog.Int("frame_size", cfg.FrameSize))

	params := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   cfg.Device,
			Channels: cfg.Channels,
			Latency:  cfg.Device.DefaultLowInputLatency,
		},
		SampleRate:      cfg.SampleRate,
		FramesPerBuffer: cfg.FrameSize,
	}
	if cfg.Latency > 0 {
		params.Input.Latency = cfg.Latency
	}

	stream, err := portaudio.OpenStream(params, func(in []float32) {
		frame := make([]float32, len(in))
		copy(frame, in)

		select {
		case out <- frame:
		default:
			select {
			case <-out:
			default:
			}
			select {
			case out <- frame:
			default:
			}
		}
	})
	if err != nil {
		return eris.Wrap(err, "open audio stream")
	}
	defer stream.Close()

	if err := stream.Start(); err != nil {
		return eris.Wrap(err, "start audio stream")
	}
	defer stream.Stop()

	<-ctx.Done()
	return ctx.Err()
}

func randomMusicModePort() uint16 {
	const base = 55000
	const span = 5000
	return uint16(base + rng.Intn(span))
}
