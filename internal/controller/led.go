package controller

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/cybre/yeelight-music-sync/internal/dsp"
	"github.com/cybre/yeelight-music-sync/internal/patterns"
	"github.com/cybre/yeelight-music-sync/internal/ui"
	"github.com/cybre/yeelight-music-sync/internal/utils"
	"github.com/cybre/yeelight-music-sync/internal/yeelight"
)

// LEDController drives the Yeelight music mode using analyzed audio features.
type LEDController struct {
	bulb   *yeelight.MusicModeBulb
	logger *slog.Logger
	viz    *ui.Visualizer

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

// NewLEDController constructs a controller with smoothing defaults.
func NewLEDController(bulb *yeelight.MusicModeBulb, logger *slog.Logger, viz *ui.Visualizer) *LEDController {
	var bandSmoothers [3]*dsp.Smoother
	for i := range bandSmoothers {
		bandSmoothers[i] = dsp.NewSmoother(0.14)
	}

	return &LEDController{
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

// Run reacts to analyzer output and pushes updates to the bulb and visualizer.
func (c *LEDController) Run(ctx context.Context, in <-chan dsp.Features, analyzer *patterns.Analyzer) error {
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

func (c *LEDController) apply(ctx context.Context, features dsp.Features, state patterns.Output) error {
	if state.Beat {
		c.beatPulse = utils.Clamp(state.BeatStrength*1.2, 0.0, 1.0)
	} else {
		c.beatPulse *= 0.88
	}
	c.sparkleLevel = c.sparkleSmoother.Step(features.BandEnergyNormalized[2])

	for i := range c.smoothedBands {
		c.smoothedBands[i] = c.bandSmoothers[i].Step(features.BandEnergyNormalized[i])
	}
	c.centroidValue = c.centroidSmoother.Step(features.SpectralCentroidNorm)
	c.rolloffValue = c.rolloffSmoother.Step(features.SpectralRolloffNorm)

	lowMidBalance := utils.SpectralBalance(c.smoothedBands[0], c.smoothedBands[1])
	midHiBalance := utils.SpectralBalance(c.smoothedBands[1], c.smoothedBands[2])

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
		c.viz.Update(ui.VisualizerFrame{
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
	satInt := utils.Clamp(int(math.Round(c.saturation)), 0, 100)
	brightInt := utils.Clamp(int(math.Round(c.brightness)), 1, 100)

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
	return utils.Clamp(base+pulseShift, 0.0, 359.0)
}

func energyPulseSaturation(mid, treble float64, state patterns.Output, beatPulse, sparkle float64) float64 {
	return utils.Clamp(38+42*mid+25*treble+20*beatPulse+16*state.BeatDensity+18*sparkle, 25.0, 100.0)
}

func energyPulseBrightness(intensity, beatPulse, sparkle float64) float64 {
	base := 28 + 62*intensity
	pulse := 32 * beatPulse
	sparkleBoost := 26 * sparkle
	return utils.Clamp(base+pulse+sparkleBoost, 8.0, 100.0)
}

func spectrumFlowHue(centroid, rolloff, midHiBalance float64) float64 {
	return utils.Clamp(210*centroid+40*(rolloff-0.5)+90*(midHiBalance-0.5)+40, 0.0, 359.0)
}

func spectrumFlowSaturation(mid, treble, intensity float64) float64 {
	return utils.Clamp(42+50*mid+18*treble+12*intensity, 28.0, 98.0)
}

func spectrumFlowBrightness(high, intensity, beatPulse, sparkle float64) float64 {
	return utils.Clamp(34+56*intensity+22*high+12*beatPulse+20*sparkle, 10.0, 100.0)
}

func smoothHue(current, target, alpha float64) float64 {
	delta := math.Mod(target-current+540, 360) - 180
	return math.Mod(current+alpha*delta+360, 360)
}
