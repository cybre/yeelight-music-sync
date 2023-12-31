package main

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/crazy3lf/colorconv"
)

type vizFrame struct {
	Hue          float64
	Saturation   float64
	Brightness   float64
	Intensity    float64
	Energy       float64
	Beat         bool
	BeatStrength float64
	BeatPulse    float64
	Bass         float64
	Mid          float64
	Treble       float64
	Sparkle      float64
	Centroid     float64
	Rolloff      float64
	Mode         string
}

type visualizer struct {
	enabled    bool
	mu         sync.Mutex
	lastRender time.Time
}

func newVisualizer(enabled bool) *visualizer {
	return &visualizer{enabled: enabled}
}

const (
	ansiClear      = "\033[H\033[2J"
	ansiReset      = "\033[0m"
	vizBarWidth    = 30
	blockFilled    = "█"
	blockEmpty     = "░"
	emptyBlockTone = 236
)

type barTheme struct {
	LabelColor int
	TextColor  int
	EmptyColor int

	HueStart   float64
	HueEnd     float64
	Saturation float64
	ValueBase  float64
	ValueSpan  float64

	FilledChar string
	EmptyChar  string
}

var defaultTheme = barTheme{
	LabelColor: 250,
	TextColor:  250,
	EmptyColor: emptyBlockTone,
	HueStart:   210,
	HueEnd:     210,
	Saturation: 0.8,
	ValueBase:  0.35,
	ValueSpan:  0.45,
	FilledChar: blockFilled,
	EmptyChar:  blockEmpty,
}

var vizThemes = map[string]barTheme{
	"Energy": {
		LabelColor: 45,
		TextColor:  159,
		EmptyColor: 238,
		HueStart:   190,
		HueEnd:     140,
		Saturation: 0.85,
		ValueBase:  0.35,
		ValueSpan:  0.55,
		FilledChar: blockFilled,
		EmptyChar:  blockEmpty,
	},
	"Beat Pulse": {
		LabelColor: 204,
		TextColor:  213,
		EmptyColor: 237,
		HueStart:   330,
		HueEnd:     360,
		Saturation: 0.9,
		ValueBase:  0.4,
		ValueSpan:  0.55,
		FilledChar: blockFilled,
		EmptyChar:  blockEmpty,
	},
	"Bass": {
		LabelColor: 208,
		TextColor:  215,
		EmptyColor: 237,
		HueStart:   25,
		HueEnd:     45,
		Saturation: 0.92,
		ValueBase:  0.4,
		ValueSpan:  0.5,
		FilledChar: blockFilled,
		EmptyChar:  blockEmpty,
	},
	"Mid": {
		LabelColor: 226,
		TextColor:  229,
		EmptyColor: 236,
		HueStart:   55,
		HueEnd:     75,
		Saturation: 0.9,
		ValueBase:  0.35,
		ValueSpan:  0.55,
		FilledChar: blockFilled,
		EmptyChar:  blockEmpty,
	},
	"Treble": {
		LabelColor: 123,
		TextColor:  159,
		EmptyColor: 236,
		HueStart:   210,
		HueEnd:     240,
		Saturation: 0.85,
		ValueBase:  0.35,
		ValueSpan:  0.5,
		FilledChar: blockFilled,
		EmptyChar:  blockEmpty,
	},
	"Sparkle": {
		LabelColor: 177,
		TextColor:  213,
		EmptyColor: 236,
		HueStart:   285,
		HueEnd:     315,
		Saturation: 0.95,
		ValueBase:  0.4,
		ValueSpan:  0.5,
		FilledChar: blockFilled,
		EmptyChar:  blockEmpty,
	},
	"Centroid": {
		LabelColor: 81,
		TextColor:  153,
		EmptyColor: 236,
		HueStart:   180,
		HueEnd:     200,
		Saturation: 0.78,
		ValueBase:  0.35,
		ValueSpan:  0.45,
		FilledChar: blockFilled,
		EmptyChar:  blockEmpty,
	},
	"Rolloff": {
		LabelColor: 45,
		TextColor:  117,
		EmptyColor: 236,
		HueStart:   150,
		HueEnd:     200,
		Saturation: 0.75,
		ValueBase:  0.35,
		ValueSpan:  0.45,
		FilledChar: blockFilled,
		EmptyChar:  blockEmpty,
	},
}

func (v *visualizer) Update(frame vizFrame) {
	if v == nil || !v.enabled {
		return
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	now := time.Now()
	if now.Sub(v.lastRender) < 50*time.Millisecond {
		return
	}
	v.lastRender = now

	sat := clampFloat(frame.Saturation/100, 0, 1)
	val := clampFloat(frame.Brightness/100, 0, 1)
	headerColor := ansiColorFromHSV(frame.Hue, sat, val)
	mode := frame.Mode
	if mode == "" {
		mode = "unknown"
	}

	fmt.Print(ansiClear)
	fmt.Printf("%sAudio Reactive Visualizer%s  %s\n", colorSeq(headerColor), ansiReset, now.Format("15:04:05.000"))
	fmt.Printf("Mode: %s  HSV:%3.0f°/%3.0f%%/%3.0f%%  Intensity:%4.2f  Beat:%s (%4.2f)\n",
		colorizeText(mode, 249),
		frame.Hue,
		frame.Saturation,
		frame.Brightness,
		clampFloat(frame.Intensity, 0, 1),
		boolLabel(frame.Beat),
		clampFloat(frame.BeatStrength, 0, 1),
	)

	fmt.Println(renderColorSwatch(frame))
	fmt.Println()
	fmt.Println(renderBar("Energy", frame.Energy, vizThemes["Energy"]))
	fmt.Println(renderBar("Beat Pulse", frame.BeatPulse, vizThemes["Beat Pulse"]))
	fmt.Println(renderBar("Bass", frame.Bass, vizThemes["Bass"]))
	fmt.Println(renderBar("Mid", frame.Mid, vizThemes["Mid"]))
	fmt.Println(renderBar("Treble", frame.Treble, vizThemes["Treble"]))
	fmt.Println(renderBar("Sparkle", frame.Sparkle, vizThemes["Sparkle"]))
	fmt.Println(renderBar("Centroid", frame.Centroid, vizThemes["Centroid"]))
	fmt.Println(renderBar("Rolloff", frame.Rolloff, vizThemes["Rolloff"]))
}

func renderBar(label string, value float64, theme barTheme) string {
	clamped := clampFloat(value, 0, 1)
	filled := int(math.Round(clamped * vizBarWidth))
	if clamped > 0 && filled == 0 {
		filled = 1
	}
	if filled > vizBarWidth {
		filled = vizBarWidth
	}

	if theme.FilledChar == "" {
		theme.FilledChar = blockFilled
	}
	if theme.EmptyChar == "" {
		theme.EmptyChar = blockEmpty
	}
	if theme.EmptyColor == 0 {
		theme.EmptyColor = emptyBlockTone
	}
	if theme.ValueSpan <= 0 {
		theme.ValueSpan = defaultTheme.ValueSpan
	}
	if theme.ValueBase <= 0 {
		theme.ValueBase = defaultTheme.ValueBase
	}
	if theme.Saturation <= 0 {
		theme.Saturation = defaultTheme.Saturation
	}

	builder := strings.Builder{}
	builder.Grow(96)
	builder.WriteString(colorizeText(fmt.Sprintf("%-16s", label), theme.LabelColor))
	builder.WriteString(" [")
	builder.WriteString(renderFilledSegments(filled, theme))
	builder.WriteString(renderEmptySegments(vizBarWidth-filled, theme))
	builder.WriteString("] ")
	builder.WriteString(colorizeText(fmt.Sprintf("%3.0f%%", clamped*100), theme.TextColor))
	return builder.String()
}

func renderFilledSegments(count int, theme barTheme) string {
	if count <= 0 {
		return ""
	}
	builder := strings.Builder{}
	builder.Grow(count * len(theme.FilledChar))

	steps := count - 1
	if steps <= 0 {
		steps = 1
	}

	for i := range count {
		progress := float64(i) / float64(steps)
		hue := theme.HueStart + (theme.HueEnd-theme.HueStart)*progress
		value := clampFloat(theme.ValueBase+theme.ValueSpan*progress, 0, 1)
		color := ansiColorFromHSV(hue, theme.Saturation, value)
		builder.WriteString(fmt.Sprintf("\033[38;5;%dm%s", color, theme.FilledChar))
	}
	builder.WriteString(ansiReset)
	return builder.String()
}

func renderEmptySegments(count int, theme barTheme) string {
	if count <= 0 {
		return ""
	}
	builder := strings.Builder{}
	builder.Grow(count * len(theme.EmptyChar))
	seq := fmt.Sprintf("\033[38;5;%dm", theme.EmptyColor)
	for _ = range count {
		builder.WriteString(seq)
		builder.WriteString(theme.EmptyChar)
	}
	builder.WriteString(ansiReset)
	return builder.String()
}

func renderColorSwatch(frame vizFrame) string {
	sat := clampFloat(frame.Saturation/100, 0, 1)
	bri := clampFloat(frame.Brightness/100, 0, 1)
	const swatchSteps = 18

	builder := strings.Builder{}
	builder.Grow(128)
	builder.WriteString(colorizeText("Color Swatch    ", 245))

	for i := range swatchSteps {
		progress := float64(i) / float64(swatchSteps-1)
		value := clampFloat(0.15+0.85*progress*bri, 0, 1)
		color := ansiColorFromHSV(frame.Hue, sat, value)
		builder.WriteString(fmt.Sprintf("\033[48;5;%dm  \033[0m", color))
	}

	builder.WriteString(fmt.Sprintf("  Hue:%3.0f° Sat:%3.0f%% Bri:%3.0f%%",
		frame.Hue,
		frame.Saturation,
		frame.Brightness,
	))
	return builder.String()
}

func boolLabel(active bool) string {
	if active {
		return "\033[38;5;197m●\033[0m"
	}
	return "\033[38;5;244m○\033[0m"
}

func colorizeText(text string, color int) string {
	if color == 0 {
		color = 250
	}
	return fmt.Sprintf("\033[38;5;%dm%s%s", color, text, ansiReset)
}

func colorSeq(color int) string {
	return fmt.Sprintf("\033[38;5;%dm", color)
}

func ansiColorFromHSV(h, s, v float64) int {
	s = clampFloat(s, 0, 1)
	v = clampFloat(v, 0, 1)
	r, g, b, err := colorconv.HSVToRGB(h, s, v)
	if err != nil {
		return 250
	}
	rIdx := int(math.Round(float64(r) / 255 * 5))
	gIdx := int(math.Round(float64(g) / 255 * 5))
	bIdx := int(math.Round(float64(b) / 255 * 5))
	rIdx = clampInt(rIdx, 0, 5)
	gIdx = clampInt(gIdx, 0, 5)
	bIdx = clampInt(bIdx, 0, 5)
	return 16 + 36*rIdx + 6*gIdx + bIdx
}
