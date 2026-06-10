// Package tui provides an interactive terminal command picker using bubbletea.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/javanhut/imlazy/parser"
)

const (
	maxVisible       = 10
	nameColumnWidth  = 20
	runPreviewMaxLen = 60
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86"))

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	previewStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true)
)

// model represents the TUI state
type model struct {
	commands  []parser.CommandInfo
	filtered  []parser.CommandInfo
	cursor    int
	textInput textinput.Model
	selected  string
	quitting  bool
}

// initialModel creates the initial model state
func initialModel(commands []parser.CommandInfo) model {
	ti := textinput.New()
	ti.Placeholder = "Type to filter commands..."
	ti.Focus()
	ti.CharLimit = 50
	ti.Width = 40

	return model{
		commands:  commands,
		filtered:  commands,
		textInput: ti,
	}
}

// Init initializes the model
func (m model) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit

		case "enter":
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				m.selected = m.filtered[m.cursor].Name
			}
			m.quitting = true
			return m, tea.Quit

		case "up", "ctrl+p":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case "down", "ctrl+n":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil

		case "ctrl+u":
			m.textInput.SetValue("")
			m.filterCommands()
			return m, nil
		}

	}

	// Handle text input
	m.textInput, cmd = m.textInput.Update(msg)

	// Filter commands based on input
	m.filterCommands()

	return m, cmd
}

// filterCommands filters the command list based on the search input
func (m *model) filterCommands() {
	query := strings.ToLower(m.textInput.Value())
	if query == "" {
		m.filtered = m.commands
		if m.cursor >= len(m.filtered) {
			m.cursor = 0
		}
		return
	}

	var filtered []parser.CommandInfo
	for _, cmd := range m.commands {
		nameLower := strings.ToLower(cmd.Name)
		descLower := strings.ToLower(cmd.Description)

		// Match if query appears in name or description
		if strings.Contains(nameLower, query) || strings.Contains(descLower, query) {
			filtered = append(filtered, cmd)
			continue
		}

		// Fuzzy match: check if all chars appear in order
		if fuzzyMatch(nameLower, query) {
			filtered = append(filtered, cmd)
		}
	}

	m.filtered = filtered
	if m.cursor >= len(m.filtered) {
		m.cursor = 0
	}
}

// fuzzyMatch checks if all characters in needle appear in haystack in order
func fuzzyMatch(haystack, needle string) bool {
	hi := 0
	for _, char := range needle {
		found := false
		for hi < len(haystack) {
			if rune(haystack[hi]) == char {
				found = true
				hi++
				break
			}
			hi++
		}
		if !found {
			return false
		}
	}
	return true
}

// View renders the UI
func (m model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render("ImLazy Command Picker"))
	b.WriteString("\n\n")

	// Search input
	b.WriteString(m.textInput.View())
	b.WriteString("\n\n")

	// Show match count
	if m.textInput.Value() != "" {
		b.WriteString(dimStyle.Render(fmt.Sprintf("Showing %d of %d commands", len(m.filtered), len(m.commands))))
		b.WriteString("\n\n")
	}

	// Command list
	start := 0
	if m.cursor >= maxVisible {
		start = m.cursor - maxVisible + 1
	}

	for i := start; i < len(m.filtered) && i < start+maxVisible; i++ {
		cmd := m.filtered[i]

		// Cursor indicator
		cursor := "  "
		style := normalStyle
		if i == m.cursor {
			cursor = "> "
			style = selectedStyle
		}

		// Command name and description
		line := fmt.Sprintf("%s%-*s", cursor, nameColumnWidth, cmd.Name)
		if cmd.Description != "" {
			line += fmt.Sprintf(" %s", dimStyle.Render(cmd.Description))
		}

		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}

	// Preview of selected command
	if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
		selected := m.filtered[m.cursor]
		if len(selected.Run) > 0 {
			b.WriteString("\n")
			b.WriteString(dimStyle.Render("Run: "))
			runPreview := strings.Join(selected.Run, " && ")
			if len(runPreview) > runPreviewMaxLen {
				runPreview = runPreview[:runPreviewMaxLen-3] + "..."
			}
			b.WriteString(previewStyle.Render(runPreview))
			b.WriteString("\n")
		}

		// Show aliases if any
		if len(selected.Aliases) > 0 {
			b.WriteString(dimStyle.Render("Aliases: "))
			b.WriteString(previewStyle.Render(strings.Join(selected.Aliases, ", ")))
			b.WriteString("\n")
		}
	}

	// Help
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("↑/↓ navigate • enter select • esc cancel • ctrl+u clear"))

	return b.String()
}

// RunPicker opens the interactive command picker and returns the selected
// command. When history is provided, commands are ordered by frecency so the
// most-used commands surface first.
func RunPicker(cfg *parser.Config, history *parser.HistoryStore) (string, error) {
	commands := cfg.GetCommandsInfo()
	if len(commands) == 0 {
		return "", fmt.Errorf("no commands defined")
	}

	if history != nil {
		if entries, err := history.Get(100); err == nil && len(entries) > 0 {
			parser.SortByFrecency(commands, entries)
		}
	}

	m := initialModel(commands)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	final := finalModel.(model)
	return final.selected, nil
}
