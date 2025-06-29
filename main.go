package main

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	helpTextEditMode = "Tab - switch focus | Enter - execute | Ctrl+X - exit and print | Ctrl+C - exit"
	helpTextViewMode = "Tab - switch focus | y - copy result | l - toggle line number | q - exit | Ctrl+X - exit and print | Ctrl+C - exit\n" +
		"hjkl/←↑↓→ - scroll | u/d - scroll half page | f/b/PgUp/PgDown - scroll full page | g/G - vertical 0/max | Home/End - horizontal 0/max"
)

// Define a consistent total horizontal margin for the entire app content area
// 2 characters on left, 2 on right
const (
	horizontalMargin     = 2
	outputVerticalMargin = 1
)

var (
	roundedBorder = lipgloss.RoundedBorder()

	inputStyle = lipgloss.NewStyle().
			BorderStyle(roundedBorder).
			BorderForeground(lipgloss.Color("#A072E3")). // Modern purple for input border
			Padding(0, horizontalMargin)

	outputStyle = lipgloss.NewStyle().
			BorderStyle(roundedBorder).
			BorderForeground(lipgloss.Color("#64FFDA")). // Modern aquamarine for output border
			Padding(outputVerticalMargin, horizontalMargin)

	outputFocusedBorderColor = lipgloss.Color("#FFD580") // Soft orange
	outputFocusedStyle       = outputStyle.Copy().BorderForeground(outputFocusedBorderColor)

	lineNumberStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7F7F7F")) // subtle gray

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5C5C")). // Brighter red for error text
			BorderStyle(roundedBorder).
			BorderForeground(lipgloss.Color("#FF5C5C")). // Red border for error box
			Padding(0, horizontalMargin)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7F7F7F")). // Subtle grey for help text
			Padding(0, horizontalMargin)
)

// commandResultMsg is a message type sent when the command processing is done.
type commandResultMsg struct {
	output       string
	errorMessage string // Human-readable error message, if any
}

// model represents the state of our TUI application
type model struct {
	winWidth        int
	winHeight       int
	textInput       textinput.Model
	viewport        viewport.Model
	stdinContent    string // Content read from os.Stdin
	rawOutput       string // Raw output after executing all commands
	processedOutput string // Processed output, may contain line numbers
	quitting        bool   // Flag to indicate if the app is quitting
	command         string // Stores the command entered when exiting with Ctrl+X
	errorMessage    string // Stores the error message to display in the UI
	isProcessing    bool   // Flag to indicate if a command is currently being processed
	showLineNumber  bool   // Flag to indicate whether adding line numbers to output
}

// initModel initializes the model with default values and reads stdin
func initModel() model {
	ti := textinput.New()
	ti.Placeholder = "Enter commands (e.g., 'grep hello | wc -l')"
	ti.Focus() // Set initial focus on the input box
	ti.CharLimit = 256
	ti.Width = 80 // Initial width, will be adjusted by WindowSizeMsg

	vp := viewport.New(80, 20) // Initial width and height, will be adjusted
	vp.SetHorizontalStep(10)   // Enable horizontal scroll in 10 incrementals

	// Read stdin content
	stdinBytes, err := io.ReadAll(os.Stdin)
	var stdinStr string
	if err != nil {
		stdinStr = fmt.Sprintf("Error reading stdin: %v", err)
	} else {
		stdinStr = string(stdinBytes)
	}

	m := model{
		textInput:    ti,
		viewport:     vp,
		stdinContent: stdinStr,
	}
	m.updateOutput(stdinStr) // Initial output is stdin content
	return m
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd = m.handleKeyMsg(msg)
	case tea.WindowSizeMsg:
		m.handleWindowSizeMsg(msg)
	case commandResultMsg:
		m.handleCommandResultMsg(msg)
		m.updateWindow()
	}
	return m, cmd
}

func (m *model) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyCtrlC: // Exit immediately
		m.quitting = true
		return tea.Quit
	case tea.KeyCtrlX: // Exit and print command
		m.command = m.textInput.Value()
		m.quitting = true
		return tea.Quit
	case tea.KeyEnter: // Process command
		if m.isProcessing || !m.textInput.Focused() {
			return nil
		}
		m.errorMessage = ""
		m.isProcessing = true
		m.textInput.Blur()
		// Start the command processing in a goroutine
		return runCommand(m.textInput.Value(), m.stdinContent)
	case tea.KeyTab: // Switch focus between input and viewport
		if m.textInput.Focused() {
			m.textInput.Blur()
		} else {
			m.textInput.Focus()
		}
		return nil
	default:
		var cmd tea.Cmd
		if m.textInput.Focused() {
			switch msg.Type {
			case tea.KeyCtrlD:
				if m.textInput.Value() == "" {
					m.quitting = true
					return tea.Quit
				}
			default:
				// Every other key goes to the input box
				m.textInput, cmd = m.textInput.Update(msg)
			}
		} else {
			// Handle keys on the view port
			switch msg.Type {
			case tea.KeyHome:
				m.viewport.SetXOffset(0)
			case tea.KeyEnd:
				m.viewport.SetXOffset(math.MaxInt64)
			}
			switch msg.String() {
			case "q":
				m.quitting = true
				return tea.Quit
			case "y":
				err := clipboard.WriteAll(m.rawOutput)
				if err != nil {
					m.errorMessage = fmt.Sprintf("Failed to copy to clipboard: %v", err)
				}
			case "l":
				m.showLineNumber = !m.showLineNumber
				m.refreshOutput()
			case "g":
				m.viewport.GotoTop()
			case "G":
				m.viewport.GotoBottom()
			default:
				m.viewport, cmd = m.viewport.Update(msg)
			}
		}
		return cmd
	}
}

func (m *model) updateOutput(output string) {
	m.rawOutput = output
	m.processedOutput = addLineNumbers(output)
	m.refreshOutput()
}

func (m *model) refreshOutput() {
	if m.showLineNumber {
		m.viewport.SetContent(m.processedOutput)
	} else {
		m.viewport.SetContent(m.rawOutput)
	}
}

func addLineNumbers(content string) string {
	lines := strings.Split(content, "\n")
	width := len(fmt.Sprintf("%d", len(lines))) // width for alignment (e.g., 3 for up to 999)

	var b strings.Builder
	for i, line := range lines {
		lineNum := lineNumberStyle.Render(fmt.Sprintf("%*d ", width, i+1))
		b.WriteString(lineNum + line + "\n")
	}
	return b.String()
}

// handleWindowSizeMsg recalculates component layouts based on new window size.
func (m *model) handleWindowSizeMsg(msg tea.WindowSizeMsg) {
	m.winWidth = msg.Width
	m.winHeight = msg.Height
	m.updateWindow()
}

func (m *model) updateWindow() {
	availableWidth := m.winWidth - 2 // -2 for left and right margin
	if availableWidth < 0 {          // Prevent negative width
		availableWidth = 0
	}

	// Set component inner width
	m.textInput.Width = availableWidth - horizontalMargin*2 - 2 // Additional -2 to account for the prompt '> '
	m.viewport.Width = availableWidth - horizontalMargin*2

	// Update all component to have the same outter width
	inputStyle = inputStyle.Width(availableWidth)
	outputBorder := lipgloss.Border{
		Top:         getBorderTopWithTitle(fmt.Sprintf(" Output (%d lines) ", countLines(m.rawOutput)), availableWidth-2),
		Bottom:      roundedBorder.Bottom,
		Left:        roundedBorder.Left,
		Right:       roundedBorder.Right,
		TopLeft:     roundedBorder.TopLeft,
		TopRight:    roundedBorder.TopRight,
		BottomLeft:  roundedBorder.BottomLeft,
		BottomRight: roundedBorder.BottomRight,
	}
	outputStyle = outputStyle.BorderStyle(outputBorder).Width(availableWidth)
	outputFocusedStyle = outputStyle.Copy().BorderForeground(outputFocusedBorderColor)
	errorStyle = errorStyle.Width(availableWidth)
	helpStyle = helpStyle.Width(availableWidth)

	// Vertical Height Calculations for all components
	inputBoxRendered := inputStyle.Render(m.textInput.View())
	inputBoxHeight := lipgloss.Height(inputBoxRendered)

	errorBoxHeight := 0
	if m.errorMessage != "" {
		errorBoxRendered := errorStyle.Render(m.errorMessage)
		errorBoxHeight = lipgloss.Height(errorBoxRendered)
	}

	helpTextRendered := helpStyle.Render(helpTextEditMode)
	helpTextHeight := lipgloss.Height(helpTextRendered)

	// Gaps between elements
	const (
		gapAfterInput = 1
		gapAfterError = 0 // Thought this should be 1 but somehow only 0 seems to work
		gapBeforeHelp = 1
	)

	// Remaining height for the viewport's *content*
	remainingHeight := m.winHeight
	remainingHeight -= (inputBoxHeight + gapAfterInput)
	remainingHeight -= outputStyle.GetVerticalFrameSize()
	remainingHeight -= (gapBeforeHelp + helpTextHeight)
	if errorBoxHeight > 0 {
		remainingHeight -= (errorBoxHeight + gapAfterError)
	}
	if remainingHeight < 0 { // Prevent negative height if terminal is too small
		remainingHeight = 0
	}
	m.viewport.Height = remainingHeight

	// Re-set content to re-flow text within new viewport dimensions if needed
	m.refreshOutput()
}

func countLines(s string) int {
	if s == "" {
		return 0
	}

	count := strings.Count(s, "\n")
	// If the string doesn't end with a newline, add 1
	if !strings.HasSuffix(s, "\n") {
		count++
	}

	return count
}

// Build a custom border top for lipgloss that embeds a title in it
func getBorderTopWithTitle(title string, width int) string {
	const filler = "─"
	const lead = 4

	if width <= 0 {
		return ""
	} else if width <= len(title) {
		return title[:width] // truncate if title too long
	}

	// Compute how many dashes go on each side
	var left, right int
	if width <= len(title)+lead {
		left = 1
	} else {
		left = lead
	}
	right = width - len(title) - left

	return strings.Repeat(filler, left) + title + strings.Repeat(filler, right)
}

// Handles results from the user entered command
func (m *model) handleCommandResultMsg(msg commandResultMsg) {
	m.isProcessing = false
	m.textInput.Focus()

	if msg.errorMessage != "" {
		m.errorMessage = msg.errorMessage
		// Output remains unchanged (last good state or stdin content)
	} else {
		m.updateOutput(msg.output)
		m.errorMessage = ""
	}
	m.viewport.GotoTop()
}

// runCommand executes the user-entered command on the stdin content
// in a separate goroutine and sends a commandResultMsg back.
func runCommand(cmdStr, stdinContent string) tea.Cmd {
	return func() tea.Msg {
		trimmedCmdStr := strings.TrimSpace(cmdStr)
		if trimmedCmdStr == "" {
			return commandResultMsg{output: stdinContent, errorMessage: ""}
		}

		cmd := exec.Command("sh", "-c", trimmedCmdStr)

		cmd.Stdin = strings.NewReader(stdinContent)

		var output bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err != nil {
			errMsg := fmt.Sprintf("Error: Command '%s' failed. ", cmdStr)
			if stderr.Len() > 0 {
				errMsg += strings.TrimSpace(stderr.String())
			} else {
				errMsg += err.Error()
			}
			return commandResultMsg{output: "", errorMessage: errMsg}
		}

		return commandResultMsg{output: output.String(), errorMessage: ""}
	}
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	var sections []string
	sections = append(sections, inputStyle.Render(m.textInput.View()))
	if m.errorMessage != "" {
		sections = append(sections, errorStyle.Render(m.errorMessage))
	}
	if m.textInput.Focused() {
		sections = append(sections, outputStyle.Render(m.viewport.View()))
		sections = append(sections, helpStyle.Render(helpTextEditMode))
	} else {
		sections = append(sections, outputFocusedStyle.Render(m.viewport.View()))
		sections = append(sections, helpStyle.Render(helpTextViewMode))
	}

	// Combine all sections vertically, aligned to the left implicitly by JoinVertical
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func main() {
	m := initModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Printf("Alas, there's been an error: %v\n", err)
		os.Exit(1)
	}

	// Type assert the returned model back to our specific model type
	if appModel, ok := finalModel.(model); ok {
		if appModel.command != "" {
			fmt.Println(appModel.command)
		}
	}
}
