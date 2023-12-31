package patterns

import (
	"time"

	"github.com/cybre/yeelight-music-sync/internal/dsp"
)

// Mode describes the high-level lighting strategy selected by the pattern analyzer.
type Mode int

const (
	// ModeEnergyPulse emphasizes transient energy and beat-driven brightness pulses.
	ModeEnergyPulse Mode = iota
	// ModeSpectrumFlow focuses on spectral balance for flowing color changes.
	ModeSpectrumFlow
)

// String returns a human-friendly name for the mode.
func (m Mode) String() string {
	switch m {
	case ModeEnergyPulse:
		return "energy-pulse"
	case ModeSpectrumFlow:
		return "spectrum-flow"
	default:
		return "unknown"
	}
}

// Options tunes the behaviour of the Analyzer.
type Options struct {
	EnergyWindow    int
	BeatThreshold   float64
	MinBeatInterval time.Duration
	MaxBeatInterval time.Duration
	IntensityAlpha  float64
	ModeHold        time.Duration
	BeatWindow      time.Duration
}

// Output summarises the rhythmic state for downstream visual mapping.
type Output struct {
	Beat         bool
	BeatStrength float64
	Energy       float64
	EnergyNorm   float64
	Intensity    float64
	BeatDensity  float64
	Mode         Mode
}

// Analyzer performs beat detection, energy tracking, and mood estimation based on
// instantaneous DSP features.
type Analyzer struct {
	opts Options

	energyHistory []float64
	energySum     float64
	energyCount   int
	energyIndex   int

	lastBeat       time.Time
	beatTimes      []time.Time
	currentMode    Mode
	lastModeSwitch time.Time
	intensity      float64
	noiseFloor     float64
	peakEnergy     float64
}

// NewAnalyzer returns a ready-to-use Analyzer with sane defaults for music-reactive
// lighting in the ~44.1kHz / 1024 frame regime.
func NewAnalyzer(opts Options) *Analyzer {
	if opts.EnergyWindow <= 0 {
		opts.EnergyWindow = 48 // ≈1.1s at 44.1kHz/1024 hop
	}
	if opts.BeatThreshold <= 0 {
		opts.BeatThreshold = 1.35
	}
	if opts.MinBeatInterval <= 0 {
		opts.MinBeatInterval = 160 * time.Millisecond
	}
	if opts.MaxBeatInterval <= 0 {
		opts.MaxBeatInterval = 1200 * time.Millisecond
	}
	if opts.IntensityAlpha <= 0 {
		opts.IntensityAlpha = 0.18
	}
	if opts.ModeHold <= 0 {
		opts.ModeHold = 2500 * time.Millisecond
	}
	if opts.BeatWindow <= 0 {
		opts.BeatWindow = 2 * time.Second
	}

	return &Analyzer{
		opts:          opts,
		energyHistory: make([]float64, opts.EnergyWindow),
		currentMode:   ModeEnergyPulse,
		noiseFloor:    1e-3,
		peakEnergy:    1e-2,
	}
}

// Process ingests the latest spectral features and returns the corresponding Output.
func (a *Analyzer) Process(ts time.Time, features dsp.Features) Output {
	if a.lastModeSwitch.IsZero() {
		a.lastModeSwitch = ts
	}

	energy := features.RMS
	if energy <= 0 {
		energy = 1e-9
	}

	// Track a noise floor (slow EMA) and a decaying peak envelope for normalization.
	a.noiseFloor = ema(a.noiseFloor, energy, 0.01)
	if energy > a.peakEnergy {
		a.peakEnergy = ema(a.peakEnergy, energy, 0.34)
	} else {
		a.peakEnergy = ema(a.peakEnergy, energy, 0.02)
	}
	minPeak := a.noiseFloor * 1.5
	if a.peakEnergy < minPeak {
		a.peakEnergy = minPeak
	}

	a.energySum -= a.energyHistory[a.energyIndex]
	a.energyHistory[a.energyIndex] = energy
	a.energySum += energy
	a.energyIndex = (a.energyIndex + 1) % len(a.energyHistory)
	if a.energyCount < len(a.energyHistory) {
		a.energyCount++
	}
	avgEnergy := a.energySum / float64(max(a.energyCount, 1))

	energyNorm := clamp((energy-a.noiseFloor)/(a.peakEnergy-a.noiseFloor+1e-9), 0, 1)

	beat, beatStrength := a.detectBeat(ts, energy, avgEnergy)
	if beat {
		a.lastBeat = ts
		a.beatTimes = append(a.beatTimes, ts)
	}
	a.pruneBeats(ts)

	beatDensity := clamp(float64(len(a.beatTimes))/a.opts.BeatWindow.Seconds()/4.0, 0, 1)

	intensityInstant := clamp(0.65*energyNorm+0.25*beatDensity+0.1*features.SpectralCentroidNorm, 0, 1)
	a.intensity = ema(a.intensity, intensityInstant, a.opts.IntensityAlpha)

	a.updateMode(ts, energyNorm, beatDensity, features.SpectralCentroidNorm)

	return Output{
		Beat:         beat,
		BeatStrength: beatStrength,
		Energy:       energy,
		EnergyNorm:   energyNorm,
		Intensity:    a.intensity,
		BeatDensity:  beatDensity,
		Mode:         a.currentMode,
	}
}

func (a *Analyzer) detectBeat(ts time.Time, energy, avgEnergy float64) (bool, float64) {
	if avgEnergy <= 1e-9 {
		return false, 0
	}
	if !a.lastBeat.IsZero() && ts.Sub(a.lastBeat) < a.opts.MinBeatInterval {
		return false, 0
	}

	threshold := a.opts.BeatThreshold * avgEnergy
	if energy <= threshold {
		return false, 0
	}

	overdrive := clamp((energy-threshold)/(a.peakEnergy-threshold+1e-9), 0, 1)
	return true, overdrive
}

func (a *Analyzer) pruneBeats(now time.Time) {
	cutoff := now.Add(-a.opts.BeatWindow)
	idx := 0
	for _, ts := range a.beatTimes {
		if ts.After(cutoff) {
			a.beatTimes[idx] = ts
			idx++
		}
	}
	a.beatTimes = a.beatTimes[:idx]
}

func (a *Analyzer) updateMode(ts time.Time, energyNorm, beatDensity, centroidNorm float64) {
	mode := a.currentMode
	switch mode {
	case ModeEnergyPulse:
		if ts.Sub(a.lastModeSwitch) < a.opts.ModeHold {
			break
		}
		if energyNorm > 0.6 && centroidNorm > 0.45 {
			mode = ModeSpectrumFlow
		} else if beatDensity > 0.55 && centroidNorm > 0.4 && energyNorm > 0.5 {
			mode = ModeSpectrumFlow
		}
	case ModeSpectrumFlow:
		if ts.Sub(a.lastModeSwitch) < a.opts.ModeHold {
			break
		}
		if energyNorm < 0.35 || centroidNorm < 0.3 {
			mode = ModeEnergyPulse
		} else if beatDensity < 0.25 && energyNorm < 0.45 {
			mode = ModeEnergyPulse
		}
	}

	if mode != a.currentMode {
		a.currentMode = mode
		a.lastModeSwitch = ts
	}
}

func ema(prev, value, alpha float64) float64 {
	if alpha <= 0 {
		return prev
	}
	if alpha >= 1 {
		return value
	}
	return prev + alpha*(value-prev)
}

func clamp(v, minVal, maxVal float64) float64 {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}
