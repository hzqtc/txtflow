package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/shlex"
)

const helpText = "Enter - execute | Ctrl+X - exit and print command | Ctrl+C - exit"

// Define a consistent total horizontal margin for the entire app content area
// 2 characters on left, 2 on right
const horizontalMargin = 2

var (
	inputStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#A072E3")). // Modern purple for input border
			Padding(0, horizontalMargin)

	outputStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#64FFDA")). // Modern aquamarine for output border
			Padding(1, horizontalMargin)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5C5C")). // Brighter red for error text
			BorderStyle(lipgloss.RoundedBorder()).
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
	rawError     error  // The underlying Go error, for debugging
}

// model represents the state of our TUI application
type model struct {
	winWidth        int
	winHeight       int
	textInput       textinput.Model
	viewport        viewport.Model
	stdinContent    string // Content read from os.Stdin
	processedOutput string // Result after processing stdinContent with commands (last successful or original)
	quitting        bool   // Flag to indicate if the app is quitting
	command         string // Stores the command entered when exiting with Ctrl+X
	errorMessage    string // Stores the error message to display in the UI
	err             error  // Stores the underlying Go error object (for internal use)
	isProcessing    bool   // Flag to indicate if a command is currently being processed
}

// initModel initializes the model with default values and reads stdin
func initModel() model {
	ti := textinput.New()
	ti.Placeholder = "Enter commands (e.g., 'grep hello | wc -l')"
	ti.Focus() // Set initial focus on the input box
	ti.CharLimit = 256
	ti.Width = 80 // Initial width, will be adjusted by WindowSizeMsg

	vp := viewport.New(80, 20) // Initial width and height, will be adjusted

	// Read stdin content
	stdinBytes, err := io.ReadAll(os.Stdin)
	var stdinStr string
	if err != nil {
		stdinStr = fmt.Sprintf("Error reading stdin: %v", err)
	} else {
		stdinStr = string(stdinBytes)
	}

	m := model{
		textInput:       ti,
		viewport:        vp,
		stdinContent:    stdinStr,
		processedOutput: stdinStr, // Initial output is stdin content
	}

	m.viewport.SetContent(m.processedOutput)

	return m
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m = m.handleWindowSizeMsg(msg)

	case commandResultMsg:
		m = m.handleCommandResultMsg(msg)
		m = m.updateWindow()
	}
	return m, nil
}

func (m model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg.Type {
	case tea.KeyCtrlC: // Exit immediately
		m.quitting = true
		return m, tea.Quit
	case tea.KeyCtrlX: // Exit and print command
		m.command = m.textInput.Value()
		m.quitting = true
		return m, tea.Quit
	case tea.KeyEnter: // Process command
		if m.isProcessing {
			return m, nil
		}
		m.errorMessage = ""
		m.err = nil
		m.isProcessing = true
		m.textInput.Blur()
		// Start the command processing in a goroutine
		return m, runCommand(m.textInput.Value(), m.stdinContent)
	default:
		// Every other key goes to the input box
		m.textInput, cmd = m.textInput.Update(msg)
		// If input is cleared AND we are not currently processing a command,
		// revert to showing original stdin content and clear error.
		if m.textInput.Value() == "" && !m.isProcessing {
			m.processedOutput = m.stdinContent
			m.viewport.SetContent(m.processedOutput)
			m.errorMessage = ""
			m.err = nil
			m = m.updateWindow()
		}

		return m, cmd
	}
}

// handleWindowSizeMsg recalculates component layouts based on new window size.
func (m model) handleWindowSizeMsg(msg tea.WindowSizeMsg) model {
	m.winWidth = msg.Width
	m.winHeight = msg.Height
	return m.updateWindow()
}

func (m model) updateWindow() model {
	availableWidth := m.winWidth - horizontalMargin
	if availableWidth < 0 { // Prevent negative width
		availableWidth = 0
	}
	// Update all component to have the same outter width
	inputStyle = inputStyle.Width(availableWidth)
	outputStyle = outputStyle.Width(availableWidth)
	errorStyle = errorStyle.Width(availableWidth)
	helpStyle = helpStyle.Width(availableWidth)

	// --- Vertical Height Calculations for all components ---
	inputBoxRendered := inputStyle.Render(m.textInput.View())
	inputBoxHeight := lipgloss.Height(inputBoxRendered)

	errorBoxHeight := 0
	if m.errorMessage != "" {
		errorBoxRendered := errorStyle.Render(m.errorMessage)
		errorBoxHeight = lipgloss.Height(errorBoxRendered)
	}

	helpTextRendered := helpStyle.Render(helpText)
	helpTextHeight := lipgloss.Height(helpTextRendered)

	// Gaps between elements
	const (
		gapAfterInput = 1
		gapAfterError = 0 // Thought this should be 1 but somehow it should be 0
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
	m.viewport.SetContent(m.processedOutput)
	return m
}

// Handles results from the user entered command
func (m model) handleCommandResultMsg(msg commandResultMsg) model {
	m.isProcessing = false
	m.textInput.Focus()

	if msg.rawError != nil {
		m.errorMessage = msg.errorMessage
		m.err = msg.rawError
		// processedOutput remains the same (last good state or stdin content)
	} else {
		m.processedOutput = msg.output
		m.errorMessage = ""
		m.err = nil
	}
	m.viewport.SetContent(m.processedOutput)
	m.viewport.GotoTop()

	return m
}

// runCommand executes the user-entered command on the stdin content
// in a separate goroutine and sends a commandResultMsg back.
func runCommand(cmdStr, stdinContent string) tea.Cmd {
	return func() tea.Msg {
		trimmedCmdStr := strings.TrimSpace(cmdStr)
		if trimmedCmdStr == "" {
			return commandResultMsg{output: stdinContent, errorMessage: "", rawError: nil}
		}

		// Run commands one by one and pipe the previous command's output to next command's input
		commands := strings.Split(trimmedCmdStr, "|")
		var lastOutput bytes.Buffer
		lastOutput.WriteString(stdinContent)

		for _, cmdSegment := range commands {
			cmdSegment = strings.TrimSpace(cmdSegment)
			if cmdSegment == "" {
				return commandResultMsg{
					output:       "",
					errorMessage: "Syntax error: missing command between pipes",
					rawError:     nil,
				}
			}

			parts, err := shlex.Split(cmdSegment)
			if err != nil || len(parts) == 0 {
				return commandResultMsg{
					output:       "",
					errorMessage: fmt.Sprintf("Failed to parse command %s: %s", cmdSegment, err),
					rawError:     err,
				}
			}
			cmd := exec.Command(parts[0], parts[1:]...)
			cmd.Stdin = &lastOutput

			var output bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &stderr

			err = cmd.Run()
			if err != nil {
				errMsg := fmt.Sprintf("Error: Command '%s' failed. ", cmdSegment)
				if stderr.Len() > 0 {
					errMsg += strings.TrimSpace(stderr.String())
				} else {
					errMsg += err.Error() // Fallback to Go's error message if stderr is empty
				}
				return commandResultMsg{output: "", errorMessage: errMsg, rawError: err}
			}

			lastOutput = output
		}
		return commandResultMsg{output: lastOutput.String(), errorMessage: "", rawError: nil}
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
	sections = append(sections, outputStyle.Render(m.viewport.View()))
	sections = append(sections, helpStyle.Render(helpText))

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
