package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/rotisserie/eris"
	"golang.org/x/sync/errgroup"

	"github.com/cybre/yeelight-music-sync/internal/dsp"
	"github.com/cybre/yeelight-music-sync/internal/patterns"
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
	var (
		bulbID      string
		deviceIndex int
		listDevices bool
		listBulbs   bool
		sampleRate  float64
		frameSize   int
		channels    int
		latencyMs   int
		debug       bool
		visualize   bool
	)

	flag.StringVar(&bulbID, "bulb", "", "yeelight bulb id (use --list-bulbs to inspect)")
	flag.IntVar(&deviceIndex, "device", -1, "audio input device index (use --list-devices to inspect)")
	flag.BoolVar(&listBulbs, "list-bulbs", false, "list discovered Yeelight bulbs and exit")
	flag.BoolVar(&listDevices, "list-devices", false, "list available PortAudio devices and exit")
	flag.Float64Var(&sampleRate, "sample-rate", 0, "capture sample rate (0 = device default)")
	flag.IntVar(&frameSize, "frame-size", 1024, "analysis frame size in samples")
	flag.IntVar(&channels, "channels", 2, "number of input channels to capture (<= device max)")
	flag.IntVar(&latencyMs, "latency-ms", 0, "override input latency in milliseconds (0 = device default)")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.BoolVar(&visualize, "visualize", false, "render realtime ASCII visualization (logs go to stderr)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	bulbs, err := yeelight.Discover(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to discover yeelight bulbs: %v\n", err)
		os.Exit(1)
	}

	if listBulbs {
		for _, bulb := range bulbs {
			fmt.Printf(
				"id:%18s  name:%-15s  address:%-20s  model:%-10s  firmware_version: %s\n",
				bulb.ID(),
				bulb.Name(),
				bulb.Addr(),
				bulb.Model(),
				bulb.FirmwareVersion(),
			)
		}
		return
	}

	if err := portaudio.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "portaudio failed to initialize: %v\n", err)
		os.Exit(1)
	}
	defer portaudio.Terminate()

	devices, err := portaudio.Devices()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to enumerate devices: %v\n", err)
		os.Exit(1)
	}

	defaultDevice, err := portaudio.DefaultInputDevice()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get default input device: %v\n", err)
		os.Exit(1)
	}

	if listDevices {
		for idx, dev := range devices {
			fmt.Printf(
				"%3d: %-40s  default:%-5t  sample_rate:%.0f  max_in:%d  latency_low:%.1fms  latency_high:%.1fms\n",
				idx,
				dev.Name, idx == defaultDevice.Index,
				dev.DefaultSampleRate,
				dev.MaxInputChannels,
				dev.DefaultLowInputLatency.Seconds()*1000,
				dev.DefaultHighInputLatency.Seconds()*1000,
			)
		}
		return
	}

	opts := runtimeOptions{
		bulbID:      bulbID,
		deviceIndex: deviceIndex,
		sampleRate:  sampleRate,
		frameSize:   frameSize,
		channels:    channels,
		latency:     time.Duration(latencyMs) * time.Millisecond,
		visualize:   visualize,
	}

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
		fmt.Fprintln(os.Stderr, "Visualizer active; logs limited to warnings on stderr. Use --debug to see debug output.")
	}

	logger := slog.New(slog.NewTextHandler(logOutput, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	bulb, err := selectBulb(bulbs, opts.bulbID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to select bulb: %v\n", err)
		os.Exit(1)
	}

	device, err := selectDevice(devices, defaultDevice.Index, opts.deviceIndex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to select device: %v\n", err)
		os.Exit(1)
	}
	if device.MaxInputChannels < 1 {
		fmt.Fprintf(os.Stderr, "device %s has no input channels; select a loopback/monitor device\n", device.Name)
		os.Exit(1)
	}

	cfg := buildLoopConfig(bulb, device, opts)

	if opts.channels > 0 && opts.channels > int(device.MaxInputChannels) {
		logger.Warn("requested channels exceed device capabilities",
			slog.Int("requested", opts.channels),
			slog.Int("max", int(device.MaxInputChannels)),
			slog.Int("using", cfg.Channels),
		)
	}

	if err := run(ctx, logger, cfg); err != nil && !eris.Is(err, context.Canceled) {
		logger.Error("audio reactive loop failed", slog.Any("error", err))
		os.Exit(1)
	}
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
	defer func() {
		if err := cfg.Bulb.Disconnect(); err != nil {
			logger.Warn("faield to disconnect from bulb", slog.Any("error", err))
		}
	}()

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

func runReactiveLoop(ctx context.Context, logger *slog.Logger, bulb *yeelight.MusicModeBulb, cfg loopConfig) error {
	frameCh := make(chan []float32, 32)
	featuresCh := make(chan dsp.Features, 32)
	analyzer := dsp.NewAnalyzer(cfg.SampleRate, cfg.FrameSize, dsp.DefaultBands())
	patternAnalyzer := patterns.NewAnalyzer(patterns.Options{})
	viz := newVisualizer(cfg.Visualize)
	controller := newLEDController(bulb, logger, viz)

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		defer close(frameCh)
		return captureAudio(ctx, logger, frameCh, cfg)
	})

	g.Go(func() error {
		defer close(featuresCh)
		var mono []float64
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case frame, ok := <-frameCh:
				if !ok {
					return nil
				}
				mono = dsp.ToMono(frame, cfg.Channels, mono)
				features := analyzer.Process(mono, time.Now())
				select {
				case featuresCh <- features:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	})

	g.Go(func() error {
		return controller.Run(ctx, featuresCh, patternAnalyzer)
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

type ledController struct {
	bulb   *yeelight.MusicModeBulb
	logger *slog.Logger
	viz    *visualizer

	hue          float64
	saturation   float64
	brightness   float64
	beatPulse    float64
	sparkleLevel float64

	initialized       bool
	lastCommand       time.Time
	minCommandSpacing time.Duration
	lastHue           int
	lastSat           int
	lastBrightness    int

	satSmoother      *dsp.Smoother
	brightSmoother   *dsp.Smoother
	sparkleSmoother  *dsp.Smoother
	bandSmoothers    [3]*dsp.Smoother
	smoothedBands    [3]float64
	centroidSmoother *dsp.Smoother
	rolloffSmoother  *dsp.Smoother
	centroidValue    float64
	rolloffValue     float64
}

func newLEDController(bulb *yeelight.MusicModeBulb, logger *slog.Logger, viz *visualizer) *ledController {
	var bandSmoothers [3]*dsp.Smoother
	for i := range bandSmoothers {
		bandSmoothers[i] = dsp.NewSmoother(0.14)
	}
	return &ledController{
		bulb:              bulb,
		logger:            logger,
		viz:               viz,
		minCommandSpacing: 25 * time.Millisecond,
		satSmoother:       dsp.NewSmoother(0.16),
		brightSmoother:    dsp.NewSmoother(0.22),
		sparkleSmoother:   dsp.NewSmoother(0.14),
		bandSmoothers:     bandSmoothers,
		centroidSmoother:  dsp.NewSmoother(0.12),
		rolloffSmoother:   dsp.NewSmoother(0.1),
	}
}

func (c *ledController) Run(ctx context.Context, in <-chan dsp.Features, analyzer *patterns.Analyzer) error {
	debugTicker := time.NewTicker(2 * time.Second)
	defer debugTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case features, ok := <-in:
			if !ok {
				return nil
			}
			output := analyzer.Process(features.Timestamp, features)
			if err := c.apply(ctx, features, output); err != nil {
				return err
			}
		case <-debugTicker.C:
			c.logger.Debug("audio reactive state",
				slog.Float64("hue", c.hue),
				slog.Float64("sat", c.saturation),
				slog.Float64("brightness", c.brightness),
				slog.Float64("sparkle", c.sparkleLevel))
		}
	}
}

func (c *ledController) apply(ctx context.Context, features dsp.Features, state patterns.Output) error {
	if state.Beat {
		c.beatPulse = clampFloat(state.BeatStrength*1.2, 0, 1)
	} else {
		c.beatPulse *= 0.88
	}
	c.sparkleLevel = c.sparkleSmoother.Step(features.BandEnergyNormalized[2])

	for i := range c.smoothedBands {
		c.smoothedBands[i] = c.bandSmoothers[i].Step(features.BandEnergyNormalized[i])
	}
	c.centroidValue = c.centroidSmoother.Step(features.SpectralCentroidNorm)
	c.rolloffValue = c.rolloffSmoother.Step(features.SpectralRolloffNorm)

	lowMidBalance := spectralBalance(c.smoothedBands[0], c.smoothedBands[1])
	midHiBalance := spectralBalance(c.smoothedBands[1], c.smoothedBands[2])

	var targetHue, targetSat, targetBright float64
	switch state.Mode {
	case patterns.ModeSpectrumFlow:
		targetHue = spectrumFlowHue(c.centroidValue, c.rolloffValue, midHiBalance)
		targetSat = spectrumFlowSaturation(c.smoothedBands[1], c.smoothedBands[2], state.Intensity)
		targetBright = spectrumFlowBrightness(c.smoothedBands[2], state.Intensity, c.beatPulse, c.sparkleLevel)
	default:
		targetHue = energyPulseHue(c.smoothedBands, c.centroidValue, lowMidBalance, c.beatPulse)
		targetSat = energyPulseSaturation(c.smoothedBands[1], c.smoothedBands[2], state, c.beatPulse, c.sparkleLevel)
		targetBright = energyPulseBrightness(state.Intensity, c.beatPulse, c.sparkleLevel)
	}

	if !c.initialized {
		c.hue = targetHue
		c.saturation = targetSat
		c.brightness = targetBright
		c.initialized = true
	} else {
		c.hue = smoothHue(c.hue, targetHue, 0.22)
		c.saturation = c.satSmoother.Step(targetSat)
		c.brightness = c.brightSmoother.Step(targetBright)
	}

	if c.viz != nil {
		c.viz.Update(vizFrame{
			Hue:          c.hue,
			Saturation:   c.saturation,
			Brightness:   c.brightness,
			Intensity:    state.Intensity,
			Energy:       state.EnergyNorm,
			Beat:         state.Beat,
			BeatStrength: state.BeatStrength,
			BeatPulse:    c.beatPulse,
			Bass:         c.smoothedBands[0],
			Mid:          c.smoothedBands[1],
			Treble:       c.smoothedBands[2],
			Sparkle:      c.sparkleLevel,
			Centroid:     c.centroidValue,
			Rolloff:      c.rolloffValue,
			Mode:         state.Mode.String(),
		})
	}

	hueInt := int(math.Round(c.hue)) % 360
	if hueInt < 0 {
		hueInt += 360
	}
	satInt := clampInt(int(math.Round(c.saturation)), 0, 100)
	brightInt := clampInt(int(math.Round(c.brightness)), 1, 100)

	if time.Since(c.lastCommand) < c.minCommandSpacing {
		return nil
	}
	if hueInt == c.lastHue && satInt == c.lastSat && brightInt == c.lastBrightness {
		return nil
	}

	if err := c.bulb.SetHSV(ctx, uint16(hueInt), uint8(satInt), uint8(brightInt), yeelight.Sudden, 0); err != nil {
		return err
	}

	c.lastHue = hueInt
	c.lastSat = satInt
	c.lastBrightness = brightInt
	c.lastCommand = time.Now()
	return nil
}

func energyPulseHue(bands [3]float64, centroid float64, lowMidBalance float64, beatPulse float64) float64 {
	bass := bands[0]
	treble := bands[2]

	base := 40.0 + 180.0*centroid - 100.0*bass + 60.0*treble
	pulseShift := 20.0 * beatPulse * (0.5 - lowMidBalance)
	return clampFloat(base+pulseShift, 0, 359)
}

func energyPulseSaturation(mid, treble float64, state patterns.Output, beatPulse, sparkle float64) float64 {
	return clampFloat(38+42*mid+25*treble+20*beatPulse+16*state.BeatDensity+18*sparkle, 25, 100)
}

func energyPulseBrightness(intensity, beatPulse, sparkle float64) float64 {
	base := 28 + 62*intensity
	pulse := 32 * beatPulse
	sparkleBoost := 26 * sparkle
	return clampFloat(base+pulse+sparkleBoost, 8, 100)
}

func spectrumFlowHue(centroid, rolloff, midHiBalance float64) float64 {
	return clampFloat(210*centroid+40*(rolloff-0.5)+90*(midHiBalance-0.5)+40, 0, 359)
}

func spectrumFlowSaturation(mid, treble, intensity float64) float64 {
	return clampFloat(42+50*mid+18*treble+12*intensity, 28, 98)
}

func spectrumFlowBrightness(high, intensity, beatPulse, sparkle float64) float64 {
	return clampFloat(34+56*intensity+22*high+12*beatPulse+20*sparkle, 10, 100)
}

func smoothHue(current, target, alpha float64) float64 {
	delta := math.Mod(target-current+540, 360) - 180
	return math.Mod(current+alpha*delta+360, 360)
}

func clampFloat(v, minVal, maxVal float64) float64 {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}

func clampInt(v, minVal, maxVal int) int {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}

func spectralBalance(a, b float64) float64 {
	total := a + b
	if total <= 1e-9 {
		return 0.5
	}
	return clampFloat(a/total, 0, 1)
}

func randomMusicModePort() uint16 {
	const base = 55000
	const span = 5000
	return uint16(base + rng.Intn(span))
}
