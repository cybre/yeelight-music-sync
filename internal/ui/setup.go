package ui

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rotisserie/eris"
	"golang.org/x/term"

	"github.com/cybre/yeelight-music-sync/internal/utils"
)

var (
	ErrSelectionAborted = eris.New("selection aborted")
	ErrNoInteractiveTTY = eris.New("no interactive terminal available")
)

var (
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("213")).
			Bold(true)
	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("246"))
	pointerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("213"))
	inactivePointerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240"))
	itemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))
	selectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("219")).
				Bold(true)
	instructionKeyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("213")).
				Bold(true)
	instructionTextStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245"))
	instructionDividerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240"))
	summaryLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("246"))
	summaryValueStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Bold(true)
	emptyStateStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true)
)

type Option struct {
	Label string
}

type SetupConfig struct {
	RequireBulb   bool
	RequireDevice bool
	InitialBulb   int
	InitialDevice int
}

type SetupResult struct {
	BulbIndex   int
	DeviceIndex int
}

func RunSetup(bulbs []Option, devices []Option, cfg SetupConfig) (SetupResult, error) {
	if !cfg.RequireBulb && !cfg.RequireDevice {
		return SetupResult{
			BulbIndex:   utils.ClampIndex(cfg.InitialBulb, len(bulbs)),
			DeviceIndex: utils.ClampIndex(cfg.InitialDevice, len(devices)),
		}, nil
	}

	if !isInteractiveTerminal() {
		return SetupResult{}, ErrNoInteractiveTTY
	}

	program := tea.NewProgram(newSetupModel(bulbs, devices, cfg))
	finalModel, err := program.Run()
	if err != nil {
		return SetupResult{}, err
	}

	result := finalModel.(setupModel)
	if result.err != nil {
		return SetupResult{}, result.err
	}

	return SetupResult{
		BulbIndex:   utils.ClampIndex(result.bulbIndex, len(bulbs)),
		DeviceIndex: utils.ClampIndex(result.deviceIndex, len(devices)),
	}, nil
}

type setupStep int

const (
	stepSelectBulb setupStep = iota
	stepSelectDevice
	stepConfirm
	stepDone
)

type setupModel struct {
	step    setupStep
	cfg     SetupConfig
	bulbs   []Option
	devices []Option

	cursor      int
	bulbIndex   int
	deviceIndex int
	err         error
}

func newSetupModel(bulbs []Option, devices []Option, cfg SetupConfig) setupModel {
	m := setupModel{
		bulbs:       bulbs,
		devices:     devices,
		cfg:         cfg,
		bulbIndex:   utils.ClampIndex(cfg.InitialBulb, len(bulbs)),
		deviceIndex: utils.ClampIndex(cfg.InitialDevice, len(devices)),
	}

	switch {
	case cfg.RequireBulb && len(bulbs) > 0:
		m.step = stepSelectBulb
		m.cursor = utils.ClampIndex(cfg.InitialBulb, len(bulbs))
	case cfg.RequireDevice && len(devices) > 0:
		m.step = stepSelectDevice
		m.cursor = utils.ClampIndex(cfg.InitialDevice, len(devices))
	default:
		m.step = stepConfirm
	}

	return m
}

func (m setupModel) Init() tea.Cmd {
	return nil
}

func (m setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.step == stepDone {
		return m, tea.Quit
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.err = ErrSelectionAborted
			return m, tea.Quit
		case "up", "k":
			items := m.currentItems()
			if len(items) > 0 {
				m.cursor = wrapIndex(m.cursor-1, len(items))
			}
		case "down", "j":
			items := m.currentItems()
			if len(items) > 0 {
				m.cursor = wrapIndex(m.cursor+1, len(items))
			}
		case "tab", "right", "l":
			switch m.step {
			case stepSelectBulb:
				if m.cfg.RequireDevice && len(m.devices) > 0 {
					m.bulbIndex = m.cursor
					m.step = stepSelectDevice
					m.cursor = utils.ClampIndex(m.deviceIndex, len(m.devices))
				}
			case stepSelectDevice:
				m.deviceIndex = m.cursor
				m.step = stepConfirm
				m.cursor = 0
			}
		case "shift+tab", "left", "h":
			switch m.step {
			case stepSelectDevice:
				if m.cfg.RequireBulb && len(m.bulbs) > 0 {
					m.deviceIndex = m.cursor
					m.step = stepSelectBulb
					m.cursor = utils.ClampIndex(m.bulbIndex, len(m.bulbs))
				}
			case stepConfirm:
				if m.cfg.RequireDevice {
					m.step = stepSelectDevice
					m.cursor = utils.ClampIndex(m.deviceIndex, len(m.devices))
				} else if m.cfg.RequireBulb {
					m.step = stepSelectBulb
					m.cursor = utils.ClampIndex(m.bulbIndex, len(m.bulbs))
				}
			}
		case "enter":
			switch m.step {
			case stepSelectBulb:
				m.bulbIndex = m.cursor
				if m.cfg.RequireDevice && len(m.devices) > 0 {
					m.step = stepSelectDevice
					m.cursor = utils.ClampIndex(m.deviceIndex, len(m.devices))
				} else {
					m.step = stepConfirm
					m.cursor = 0
				}
			case stepSelectDevice:
				m.deviceIndex = m.cursor
				m.step = stepConfirm
				m.cursor = 0
			case stepConfirm:
				m.step = stepDone
				return m, tea.Quit
			}
		case "backspace", "b":
			if m.step == stepConfirm {
				if m.cfg.RequireDevice {
					m.step = stepSelectDevice
					m.cursor = utils.ClampIndex(m.deviceIndex, len(m.devices))
				} else if m.cfg.RequireBulb {
					m.step = stepSelectBulb
					m.cursor = utils.ClampIndex(m.bulbIndex, len(m.bulbs))
				}
			}
		}
	}

	return m, nil
}

func (m setupModel) View() string {
	switch m.step {
	case stepSelectBulb:
		return renderBulbView(m)
	case stepSelectDevice:
		return renderDeviceView(m)
	case stepConfirm:
		return renderSummaryView(m)
	default:
		return ""
	}
}

func (m setupModel) currentItems() []Option {
	switch m.step {
	case stepSelectDevice:
		return m.devices
	case stepSelectBulb:
		return m.bulbs
	default:
		return nil
	}
}

func renderBulbView(m setupModel) string {
	instructions := []string{"↑/k ↓/j move", "enter confirm"}
	if m.cfg.RequireDevice {
		instructions = append(instructions, "tab/right continue")
	}
	instructions = append(instructions, "esc cancel")

	lines := []string{
		"",
		titleStyle.Render("Select a Yeelight bulb"),
		"",
		renderOptionList(m.bulbs, m.cursor),
		"",
		renderInstructions(instructions),
		"",
	}
	return strings.Join(lines, "\n")
}

func renderDeviceView(m setupModel) string {
	instructions := []string{"↑/k ↓/j move", "enter confirm"}
	if m.cfg.RequireBulb {
		instructions = append(instructions, "shift+tab/left back")
	}
	instructions = append(instructions, "tab/right finish", "esc cancel")

	lines := []string{
		"",
		titleStyle.Render("Select an audio input device"),
	}

	if m.cfg.RequireBulb {
		lines = append(lines,
			"",
			renderSummaryRow("Bulb", m.selectedBulbLabel()),
		)
	}

	lines = append(lines,
		"",
		renderOptionList(m.devices, m.cursor),
		"",
		renderInstructions(instructions),
		"",
	)

	return strings.Join(lines, "\n")
}

func renderSummaryView(m setupModel) string {
	instructions := []string{"enter start", "←/h/b/backspace edit", "esc cancel"}

	lines := []string{
		"",
		titleStyle.Render("Ready to start"),
		"",
		renderSummaryRow("Bulb", m.selectedBulbLabel()),
		renderSummaryRow("Device", m.selectedDeviceLabel()),
		"",
		renderInstructions(instructions),
		"",
	}
	return strings.Join(lines, "\n")
}

func (m setupModel) selectedBulbLabel() string {
	if m.bulbIndex >= 0 && m.bulbIndex < len(m.bulbs) {
		return m.bulbs[m.bulbIndex].Label
	}
	return "not selected"
}

func (m setupModel) selectedDeviceLabel() string {
	if m.deviceIndex >= 0 && m.deviceIndex < len(m.devices) {
		return m.devices[m.deviceIndex].Label
	}
	return "not selected"
}

func renderPointer(active bool) string {
	if active {
		return pointerStyle.Render("›")
	}
	return inactivePointerStyle.Render(" ")
}

func renderOptionLabel(text string, active bool) string {
	if active {
		return selectedItemStyle.Render(text)
	}
	return itemStyle.Render(text)
}

func renderOptionList(items []Option, cursor int) string {
	if len(items) == 0 {
		return emptyStateStyle.Render("No options detected")
	}

	rows := make([]string, len(items))
	for i, item := range items {
		rows[i] = lipgloss.JoinHorizontal(lipgloss.Left,
			renderPointer(cursor == i),
			" ",
			renderOptionLabel(item.Label, cursor == i),
		)
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func renderInstructions(parts []string) string {
	if len(parts) == 0 {
		return ""
	}

	if len(parts) == 1 {
		return renderInstruction(parts[0])
	}

	var segments []string
	for i, part := range parts {
		if i > 0 {
			segments = append(segments, instructionDividerStyle.Render(" · "))
		}
		segments = append(segments, renderInstruction(part))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, segments...)
}

func renderInstruction(part string) string {
	tokens := strings.Fields(part)
	if len(tokens) == 0 {
		return ""
	}
	if len(tokens) == 1 {
		return instructionTextStyle.Render(tokens[0])
	}

	var segments []string
	keyTokens := tokens[:len(tokens)-1]
	for i, token := range keyTokens {
		if i > 0 {
			segments = append(segments, instructionTextStyle.Render(" "))
		}
		segments = append(segments, instructionKeyStyle.Render(token))
	}
	segments = append(segments, instructionTextStyle.Render(" "))
	segments = append(segments, instructionTextStyle.Render(tokens[len(tokens)-1]))
	return lipgloss.JoinHorizontal(lipgloss.Left, segments...)
}

func renderSummaryRow(label, value string) string {
	return lipgloss.JoinHorizontal(lipgloss.Left,
		summaryLabelStyle.Render(label+": "),
		summaryValueStyle.Render(value),
	)
}

func wrapIndex(idx, length int) int {
	if length <= 0 {
		return 0
	}
	idx = idx % length
	if idx < 0 {
		idx += length
	}
	return idx
}

func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
