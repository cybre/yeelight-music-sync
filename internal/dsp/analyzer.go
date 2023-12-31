package dsp

import (
	"math"
	"math/cmplx"
	"time"

	"github.com/mjibson/go-dsp/fft"

	"github.com/cybre/yeelight-music-sync/internal/utils"
)

// FrequencyBand represents an inclusive frequency span in Hz used for energy bucketing.
type FrequencyBand struct {
	Low  float64
	High float64
}

// DefaultBands covers the typical low/mid/high groupings for music content.
func DefaultBands() [3]FrequencyBand {
	return [3]FrequencyBand{
		{Low: 20, High: 250},
		{Low: 250, High: 2000},
		{Low: 2000, High: 8000},
	}
}

// Features is the set of per-frame DSP metrics that downstream stages consume.
type Features struct {
	Timestamp             time.Time
	RMS                   float64
	ZeroCrossingRate      float64
	SpectralCentroid      float64
	SpectralCentroidNorm  float64
	SpectralRolloff       float64
	SpectralRolloffNorm   float64
	BandEnergy            [3]float64
	BandEnergyNormalized  [3]float64
	TotalEnergy           float64
	PeakFrequency         float64
	PeakMagnitude         float64
	FrameDuration         time.Duration
	SpectralBalanceLowMid float64
	SpectralBalanceMidHi  float64
}

// Analyzer transforms mono frames into spectral features. It reuses scratch buffers to
// keep allocations predictable for real-time processing.
type Analyzer struct {
	sampleRate    float64
	frameSize     int
	bands         [3]FrequencyBand
	rolloffRatio  float64
	window        []float64
	windowedFrame []float64
	magnitudes    []float64
	bandWidth     float64
}

// NewAnalyzer constructs an Analyzer configured for a given sample rate/frame size.
func NewAnalyzer(sampleRate float64, frameSize int, bands [3]FrequencyBand) *Analyzer {
	if frameSize <= 0 {
		panic("dsp: frameSize must be > 0")
	}
	if sampleRate <= 0 {
		panic("dsp: sampleRate must be > 0")
	}

	var bandCopy [3]FrequencyBand
	if bands[0].Low == 0 && bands[0].High == 0 &&
		bands[1].Low == 0 && bands[1].High == 0 &&
		bands[2].Low == 0 && bands[2].High == 0 {
		bandCopy = DefaultBands()
	} else {
		bandCopy = bands
	}

	window := HannWindow(frameSize)
	return &Analyzer{
		sampleRate:    sampleRate,
		frameSize:     frameSize,
		bands:         bandCopy,
		rolloffRatio:  0.85,
		window:        window,
		windowedFrame: make([]float64, frameSize),
		magnitudes:    make([]float64, frameSize/2+1),
		bandWidth:     sampleRate / float64(frameSize),
	}
}

// Process computes spectral features for the supplied mono frame. The frame length must
// match the configured frameSize.
func (a *Analyzer) Process(frame []float64, ts time.Time) Features {
	if len(frame) != a.frameSize {
		panic("dsp: frame length mismatch")
	}

	copy(a.windowedFrame, frame)
	ApplyWindowInPlace(a.windowedFrame, a.window)

	spectrum := fft.FFTReal(a.windowedFrame)
	half := len(spectrum)/2 + 1
	if len(a.magnitudes) != half {
		a.magnitudes = make([]float64, half)
	}

	var totalEnergy float64
	var centroidNumerator float64
	var magnitudeSum float64
	var peakMagnitude float64
	var peakFreq float64
	for i := range half {
		bin := spectrum[i]
		mag := cmplx.Abs(bin)
		a.magnitudes[i] = mag
		energy := mag * mag
		totalEnergy += energy

		freq := float64(i) * a.bandWidth
		centroidNumerator += freq * mag
		magnitudeSum += mag

		if mag > peakMagnitude {
			peakMagnitude = mag
			peakFreq = freq
		}
	}

	rms := RootMeanSquare(frame)
	zcr := ZeroCrossingRate(frame)

	centroid := 0.0
	if magnitudeSum > 1e-9 {
		centroid = centroidNumerator / magnitudeSum
	}
	normFactor := a.sampleRate / 2
	centroidNorm := 0.0
	if normFactor > 0 {
		centroidNorm = utils.Clamp(centroid/normFactor, 0.0, 1.0)
	}

	rolloff := a.computeRolloff(totalEnergy)
	rolloffNorm := 0.0
	if normFactor > 0 {
		rolloffNorm = utils.Clamp(rolloff/normFactor, 0.0, 1.0)
	}

	bandEnergy, bandNorm := a.computeBandEnergy(totalEnergy)

	return Features{
		Timestamp:             ts,
		RMS:                   rms,
		ZeroCrossingRate:      zcr,
		SpectralCentroid:      centroid,
		SpectralCentroidNorm:  centroidNorm,
		SpectralRolloff:       rolloff,
		SpectralRolloffNorm:   rolloffNorm,
		BandEnergy:            bandEnergy,
		BandEnergyNormalized:  bandNorm,
		TotalEnergy:           totalEnergy,
		PeakFrequency:         peakFreq,
		PeakMagnitude:         peakMagnitude,
		FrameDuration:         time.Duration(float64(a.frameSize) / a.sampleRate * float64(time.Second)),
		SpectralBalanceLowMid: utils.SpectralBalance(bandNorm[0], bandNorm[1]),
		SpectralBalanceMidHi:  utils.SpectralBalance(bandNorm[1], bandNorm[2]),
	}
}

func (a *Analyzer) computeRolloff(totalEnergy float64) float64 {
	if totalEnergy <= 1e-9 {
		return 0
	}
	target := totalEnergy * a.rolloffRatio
	var cumulative float64
	for i, mag := range a.magnitudes {
		cumulative += mag * mag
		if cumulative >= target {
			return float64(i) * a.bandWidth
		}
	}
	return a.sampleRate / 2
}

func (a *Analyzer) computeBandEnergy(totalEnergy float64) ([3]float64, [3]float64) {
	var energies [3]float64
	for i, band := range a.bands {
		lower := max(band.Low, 0)
		upper := math.Max(band.High, lower)
		start := int(math.Floor(lower / a.bandWidth))
		end := int(math.Ceil(upper / a.bandWidth))
		if end >= len(a.magnitudes) {
			end = len(a.magnitudes) - 1
		}
		if start < 0 {
			start = 0
		}
		var bandTotal float64
		for bin := start; bin <= end; bin++ {
			mag := a.magnitudes[bin]
			bandTotal += mag * mag
		}
		energies[i] = bandTotal
	}

	var normalized [3]float64
	if totalEnergy > 1e-9 {
		for i := range energies {
			normalized[i] = utils.Clamp(energies[i]/totalEnergy, 0.0, 1.0)
		}
	}
	return energies, normalized
}

// RootMeanSquare computes the RMS value of a frame.
func RootMeanSquare(frame []float64) float64 {
	if len(frame) == 0 {
		return 0
	}
	var sumSquares float64
	for _, sample := range frame {
		sumSquares += sample * sample
	}
	return math.Sqrt(sumSquares / float64(len(frame)))
}

// ZeroCrossingRate returns the fraction of sign changes across consecutive samples.
func ZeroCrossingRate(frame []float64) float64 {
	if len(frame) < 2 {
		return 0
	}
	var crossings int
	prev := frame[0]
	for i := 1; i < len(frame); i++ {
		curr := frame[i]
		if (prev >= 0 && curr < 0) || (prev < 0 && curr >= 0) {
			crossings++
		}
		prev = curr
	}
	return float64(crossings) / float64(len(frame)-1)
}

// ToMono averages interleaved multi-channel data into a mono frame.
func ToMono(samples []float32, channels int, dst []float64) []float64 {
	if channels <= 0 {
		channels = 1
	}
	frameLen := len(samples) / channels
	if cap(dst) < frameLen {
		dst = make([]float64, frameLen)
	} else {
		dst = dst[:frameLen]
	}
	if frameLen == 0 {
		return dst
	}
	idx := 0
	for i := range frameLen {
		sum := 0.0
		for c := 0; c < channels; c++ {
			sum += float64(samples[idx])
			idx++
		}
		dst[i] = sum / float64(channels)
	}
	return dst
}

// HannWindow returns a precomputed Hann window for the requested size.
func HannWindow(n int) []float64 {
	if n <= 0 {
		return nil
	}
	window := make([]float64, n)
	if n == 1 {
		window[0] = 1
		return window
	}
	for i := range n {
		window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}
	return window
}

// ApplyWindowInPlace multiplies samples by a window function in-place.
func ApplyWindowInPlace(samples []float64, window []float64) {
	switch {
	case len(samples) == 0:
		return
	case len(samples) != len(window):
		panic("dsp: window length mismatch")
	}
	for i := range samples {
		samples[i] *= window[i]
	}
}

// Smoother implements a simple exponential moving average.
type Smoother struct {
	alpha       float64
	initialized bool
	value       float64
}

// NewSmoother constructs a Smoother using the supplied alpha (0..1).
// Smaller values produce heavier smoothing.
func NewSmoother(alpha float64) *Smoother {
	alpha = utils.Clamp(alpha, 0.0, 1.0)
	return &Smoother{alpha: alpha}
}

// Step updates the internal state and returns the smoothed value.
func (s *Smoother) Step(v float64) float64 {
	if !s.initialized {
		s.value = v
		s.initialized = true
		return v
	}
	s.value += s.alpha * (v - s.value)
	return s.value
}

// Value returns the current smoothed value without updating it.
func (s *Smoother) Value() float64 {
	return s.value
}
