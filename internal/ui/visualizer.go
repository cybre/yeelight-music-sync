package ui

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/crazy3lf/colorconv"
	"github.com/cybre/yeelight-music-sync/internal/utils"
)

type VisualizerFrame struct {
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

type Visualizer struct {
	program   *tea.Program
	mu        sync.Mutex
	lastSend  time.Time
	throttle  time.Duration
	closeOnce sync.Once
}

type frameMsg struct {
	frame      VisualizerFrame
	receivedAt time.Time
}

type visualizerModel struct {
	frame       VisualizerFrame
	lastUpdated time.Time
	ready       bool
	width       int
	height      int
	onExit      func()
	exitOnce    sync.Once
}

var (
	vizContainerStyle    = lipgloss.NewStyle().Padding(0, 2)
	vizTimestampStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	vizMetricLabelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	vizMetricValueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
	vizBeatActiveStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("197")).Bold(true)
	vizBeatInactiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	vizWaitingStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
	vizHintStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("239"))
)

const (
	vizBarWidth   = 32
	swatchBlocks  = 18
	renderLatency = 45 * time.Millisecond
)

func NewVisualizer(onExit func()) *Visualizer {
	model := &visualizerModel{onExit: onExit}
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithoutSignalHandler())

	v := &Visualizer{
		program:  program,
		throttle: renderLatency,
	}

	go program.Run()

	return v
}

func (v *Visualizer) Update(frame VisualizerFrame) {
	v.mu.Lock()
	if time.Since(v.lastSend) < v.throttle {
		v.mu.Unlock()
		return
	}
	v.lastSend = time.Now()
	v.mu.Unlock()

	v.program.Send(frameMsg{
		frame:      frame,
		receivedAt: time.Now(),
	})
}

func (v *Visualizer) Close() {
	v.closeOnce.Do(func() {
		v.program.Quit()
	})
}

func (m *visualizerModel) Init() tea.Cmd {
	return nil
}

func (m *visualizerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case frameMsg:
		m.frame = msg.frame
		m.lastUpdated = msg.receivedAt
		m.ready = true
	case tea.KeyMsg:
		switch {
		case msg.Type == tea.KeyCtrlC:
			m.invokeExit()
			return m, tea.Quit
		case msg.String() == "q", msg.String() == "esc":
			m.invokeExit()
			return m, tea.Quit
		}
	case tea.QuitMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m *visualizerModel) View() string {
	body := ""
	if !m.ready {
		header := titleStyle.Render("Audio Reactive Visualizer")
		waiting := vizWaitingStyle.Render("Waiting for audio frames…")
		body = lipgloss.JoinVertical(lipgloss.Left, header, "", waiting)
	} else {
		body = renderVisualizerView(m.frame, m.lastUpdated)
	}
	return vizContainerStyle.Render(body)
}

func renderVisualizerView(frame VisualizerFrame, updatedAt time.Time) string {
	header := renderHeader(frame, updatedAt)
	metrics := renderMetrics(frame)
	colorSwatch := renderColorSwatch(frame)
	bars := renderBars(frame)
	controls := vizHintStyle.Render("Press q / esc / ctrl+c to stop visualization")

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		metrics,
		"",
		colorSwatch,
		"",
		bars,
		"",
		controls,
	)
}

func renderHeader(frame VisualizerFrame, updatedAt time.Time) string {
	sat := utils.Clamp(frame.Saturation/100, 0.0, 1.0)
	val := utils.Clamp(frame.Brightness/100, 0.0, 1.0)
	color := lipgloss.Color(hexColorFromHSV(frame.Hue, sat, val))

	title := titleStyle.
		Foreground(color).
		Render("Audio Reactive Visualizer")
	timestamp := vizTimestampStyle.Render(updatedAt.Format("15:04:05.000"))

	return lipgloss.JoinHorizontal(lipgloss.Left, title, "  ", timestamp)
}

func renderMetrics(frame VisualizerFrame) string {
	mode := renderMetric("Mode", normalizeMode(frame.Mode))
	intensity := renderMetric("Intensity", fmt.Sprintf("%4.2f", utils.Clamp(frame.Intensity, 0.0, 1.0)))
	energy := renderMetric("Energy", fmt.Sprintf("%4.2f", utils.Clamp(frame.Energy, 0.0, 1.0)))

	hsv := renderMetric("HSV", fmt.Sprintf("%3.0f°/%3.0f%%/%3.0f%%",
		utils.Clamp(frame.Hue, 0.0, 359.0),
		utils.Clamp(frame.Saturation, 0.0, 100.0),
		utils.Clamp(frame.Brightness, 0.0, 100.0),
	))
	beat := renderBeatMetric(frame)
	pulse := renderMetric("Beat Pulse", fmt.Sprintf("%4.2f", utils.Clamp(frame.BeatPulse, 0.0, 1.0)))

	top := lipgloss.JoinHorizontal(lipgloss.Left, mode, "   ", intensity, "   ", energy)
	bottom := lipgloss.JoinHorizontal(lipgloss.Left, hsv, "   ", beat, "   ", pulse)

	return lipgloss.JoinVertical(lipgloss.Left, top, bottom)
}

func renderMetric(label, value string) string {
	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		vizMetricLabelStyle.Render(label+":"),
		" ",
		vizMetricValueStyle.Render(value),
	)
}

func renderBeatMetric(frame VisualizerFrame) string {
	marker := vizBeatInactiveStyle.Render("○")
	if frame.Beat {
		marker = vizBeatActiveStyle.Render("●")
	}
	strength := fmt.Sprintf("%4.2f", utils.Clamp(frame.BeatStrength, 0.0, 1.0))

	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		vizMetricLabelStyle.Render("Beat:"),
		" ",
		marker,
		" ",
		vizMetricValueStyle.Render(strength),
	)
}

func renderColorSwatch(frame VisualizerFrame) string {
	sat := utils.Clamp(frame.Saturation/100, 0.0, 1.0)
	bri := utils.Clamp(frame.Brightness/100, 0.0, 1.0)

	blocks := make([]string, swatchBlocks)
	for i := range swatchBlocks {
		progress := float64(i) / float64(swatchBlocks-1)
		value := utils.Clamp(0.15+0.85*progress*bri, 0.0, 1.0)
		color := lipgloss.Color(hexColorFromHSV(frame.Hue, sat, value))
		blocks[i] = lipgloss.NewStyle().Background(color).Render("  ")
	}

	swatch := strings.Join(blocks, "")
	info := vizMetricValueStyle.Render(fmt.Sprintf("Hue:%3.0f° Sat:%3.0f%% Bri:%3.0f%%",
		utils.Clamp(frame.Hue, 0.0, 359.0),
		utils.Clamp(frame.Saturation, 0.0, 100.0),
		utils.Clamp(frame.Brightness, 0.0, 100.0),
	))

	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		subtitleStyle.Render("Color"),
		"  ",
		swatch,
		"  ",
		info,
	)
}

func renderBars(frame VisualizerFrame) string {
	lines := []string{
		renderBar("Energy", frame.Energy, vizThemes["Energy"]),
		renderBar("Beat Pulse", frame.BeatPulse, vizThemes["Beat Pulse"]),
		renderBar("Bass", frame.Bass, vizThemes["Bass"]),
		renderBar("Mid", frame.Mid, vizThemes["Mid"]),
		renderBar("Treble", frame.Treble, vizThemes["Treble"]),
		renderBar("Sparkle", frame.Sparkle, vizThemes["Sparkle"]),
		renderBar("Centroid", frame.Centroid, vizThemes["Centroid"]),
		renderBar("Rolloff", frame.Rolloff, vizThemes["Rolloff"]),
	}
	return strings.Join(lines, "\n")
}

func renderBar(label string, value float64, theme barTheme) string {
	theme = normalizeBarTheme(theme)

	clamped := utils.Clamp(value, 0.0, 1.0)
	filled := int(math.Round(clamped * vizBarWidth))
	if clamped > 0 && filled == 0 {
		filled = 1
	}
	if filled > vizBarWidth {
		filled = vizBarWidth
	}

	builder := strings.Builder{}
	builder.Grow(128)
	builder.WriteString(theme.LabelStyle.Render(fmt.Sprintf("%-14s", label)))
	builder.WriteString(" [")

	if filled > 0 {
		steps := filled - 1
		if steps <= 0 {
			steps = 1
		}
		for i := 0; i < filled; i++ {
			progress := float64(i) / float64(steps)
			hue := theme.HueStart + (theme.HueEnd-theme.HueStart)*progress
			value := utils.Clamp(theme.ValueBase+theme.ValueSpan*progress, 0.0, 1.0)
			color := lipgloss.Color(hexColorFromHSV(hue, theme.Saturation, value))
			builder.WriteString(lipgloss.NewStyle().
				Foreground(color).
				Render(theme.FilledChar))
		}
	}

	empty := vizBarWidth - filled
	if empty > 0 {
		emptyBlock := theme.EmptyStyle.Render(theme.EmptyChar)
		for range empty {
			builder.WriteString(emptyBlock)
		}
	}

	builder.WriteString("] ")
	builder.WriteString(theme.ValueStyle.Render(fmt.Sprintf("%3.0f%%", clamped*100)))

	return builder.String()
}

type barTheme struct {
	LabelStyle lipgloss.Style
	ValueStyle lipgloss.Style
	EmptyStyle lipgloss.Style

	HueStart   float64
	HueEnd     float64
	Saturation float64
	ValueBase  float64
	ValueSpan  float64

	FilledChar string
	EmptyChar  string
}

var defaultBarTheme = barTheme{
	LabelStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("250")),
	ValueStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("250")),
	EmptyStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("236")),
	HueStart:   210,
	HueEnd:     210,
	Saturation: 0.8,
	ValueBase:  0.35,
	ValueSpan:  0.45,
	FilledChar: "█",
	EmptyChar:  "░",
}

var vizThemes = map[string]barTheme{
	"Energy": {
		LabelStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("45")).Bold(true),
		ValueStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("159")),
		EmptyStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("238")),
		HueStart:   190,
		HueEnd:     140,
		Saturation: 0.85,
		ValueBase:  0.35,
		ValueSpan:  0.55,
		FilledChar: "█",
		EmptyChar:  "░",
	},
	"Beat Pulse": {
		LabelStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("204")).Bold(true),
		ValueStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("213")),
		EmptyStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("237")),
		HueStart:   330,
		HueEnd:     360,
		Saturation: 0.9,
		ValueBase:  0.4,
		ValueSpan:  0.55,
	},
	"Bass": {
		LabelStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true),
		ValueStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("215")),
		EmptyStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("237")),
		HueStart:   25,
		HueEnd:     45,
		Saturation: 0.92,
		ValueBase:  0.4,
		ValueSpan:  0.5,
	},
	"Mid": {
		LabelStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true),
		ValueStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("229")),
		EmptyStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("236")),
		HueStart:   55,
		HueEnd:     75,
		Saturation: 0.9,
		ValueBase:  0.35,
		ValueSpan:  0.55,
	},
	"Treble": {
		LabelStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("123")).Bold(true),
		ValueStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("159")),
		EmptyStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("236")),
		HueStart:   210,
		HueEnd:     240,
		Saturation: 0.85,
		ValueBase:  0.35,
		ValueSpan:  0.5,
	},
	"Sparkle": {
		LabelStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("177")).Bold(true),
		ValueStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("213")),
		EmptyStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("236")),
		HueStart:   285,
		HueEnd:     315,
		Saturation: 0.95,
		ValueBase:  0.4,
		ValueSpan:  0.5,
	},
	"Centroid": {
		LabelStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true),
		ValueStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("153")),
		EmptyStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("236")),
		HueStart:   180,
		HueEnd:     200,
		Saturation: 0.78,
		ValueBase:  0.35,
		ValueSpan:  0.45,
	},
	"Rolloff": {
		LabelStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("45")).Bold(true),
		ValueStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("117")),
		EmptyStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("236")),
		HueStart:   150,
		HueEnd:     200,
		Saturation: 0.75,
		ValueBase:  0.35,
		ValueSpan:  0.45,
	},
}

func normalizeBarTheme(theme barTheme) barTheme {
	if theme.FilledChar == "" {
		theme.FilledChar = defaultBarTheme.FilledChar
	}
	if theme.EmptyChar == "" {
		theme.EmptyChar = defaultBarTheme.EmptyChar
	}
	if theme.Saturation <= 0 {
		theme.Saturation = defaultBarTheme.Saturation
	}
	if theme.ValueSpan <= 0 {
		theme.ValueSpan = defaultBarTheme.ValueSpan
	}
	if theme.ValueBase <= 0 {
		theme.ValueBase = defaultBarTheme.ValueBase
	}
	return theme
}

func hexColorFromHSV(h, s, v float64) string {
	s = utils.Clamp(s, 0.0, 1.0)
	v = utils.Clamp(v, 0.0, 1.0)
	r, g, b, err := colorconv.HSVToRGB(h, s, v)
	if err != nil {
		return "#FFFFFF"
	}
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func normalizeMode(mode string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return "unknown"
	}
	return mode
}

func (m *visualizerModel) invokeExit() {
	m.exitOnce.Do(func() {
		if m.onExit != nil {
			m.onExit()
		}
	})
}
