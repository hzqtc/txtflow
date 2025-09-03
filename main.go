package main

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/shlex"
)

const (
	helpTextEditMode = "Tab - switch focus | Esc - clear | Ctrl+X - exit and print | Ctrl+C - exit"
	helpTextViewMode = "Tab - switch focus | y - copy result | L - toggle line number | q - exit | Ctrl+X - exit and print | Ctrl+C - exit\n" +
		"hjkl/←↑↓→ - scroll | u/d - scroll half page | f/b/PgUp/PgDown - scroll full page | g/G - vertical 0/max | Home/End - horizontal 0/max"
)

// Define a consistent total horizontal margin for the entire app content area
// 2 characters on left, 2 on right
const (
	horizontalMargin     = 2
	outputVerticalMargin = 1
)

var (
	borderColor        = lipgloss.Color("#7F7F7F")
	focusedBorderColor = lipgloss.Color("#FFD580")
	errorColor         = lipgloss.Color("#FF5C5C")
	helpColor          = lipgloss.Color("#7F7F7F")

	roundedBorder = lipgloss.RoundedBorder()

	inputStyle = lipgloss.NewStyle().
			BorderStyle(roundedBorder).
			BorderForeground(borderColor).
			Padding(0, horizontalMargin)
	inputFocusedStyle = inputStyle.BorderForeground(focusedBorderColor)

	outputStyle = lipgloss.NewStyle().
			BorderStyle(roundedBorder).
			BorderForeground(borderColor).
			Padding(outputVerticalMargin, horizontalMargin)
	outputFocusedStyle = outputStyle.BorderForeground(focusedBorderColor)

	lineNumberStyle = lipgloss.NewStyle().Foreground(helpColor)

	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			BorderStyle(roundedBorder).
			BorderForeground(errorColor).
			Padding(0, horizontalMargin)

	helpStyle = lipgloss.NewStyle().
			Foreground(helpColor).
			Padding(0, horizontalMargin)
)

// commandResultMsg is a message type sent when the command processing is done.
type commandResultMsg struct {
	output       string
	errorMessage string // Human-readable error message, if any
}

type stdinMsg struct {
	ch   chan stdinMsg
	line string
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
	showLineNumber  bool   // Flag to indicate whether adding line numbers to output

	forceExit       key.Binding
	exitAndPrint    key.Binding
	exit            key.Binding
	enter           key.Binding
	esc             key.Binding
	tab             key.Binding
	copyResult      key.Binding
	toggleLineNum   key.Binding
	scrollTop       key.Binding
	scrollBottom    key.Binding
	scrollBeginning key.Binding
	scrollEnd       key.Binding
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

	m := model{
		textInput:    ti,
		viewport:     vp,
		stdinContent: "",

		forceExit:       key.NewBinding(key.WithKeys("ctrl+c")),
		exitAndPrint:    key.NewBinding(key.WithKeys("ctrl+x")),
		exit:            key.NewBinding(key.WithKeys("q")),
		enter:           key.NewBinding(key.WithKeys("enter")),
		esc:             key.NewBinding(key.WithKeys("esc")),
		tab:             key.NewBinding(key.WithKeys("tab")),
		copyResult:      key.NewBinding(key.WithKeys("y")),
		toggleLineNum:   key.NewBinding(key.WithKeys("L")),
		scrollTop:       key.NewBinding(key.WithKeys("g")),
		scrollBottom:    key.NewBinding(key.WithKeys("G")),
		scrollBeginning: key.NewBinding(key.WithKeys("home")),
		scrollEnd:       key.NewBinding(key.WithKeys("end")),
	}
	return m
}

func (m model) Init() tea.Cmd {
	stdinCh := make(chan stdinMsg)
	go func() {
		m.readStdin(stdinCh)
	}()
	return streamStdin(stdinCh)
}

func streamStdin(ch chan stdinMsg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

func (m *model) readStdin(ch chan stdinMsg) {
	// Check if stdin is a character device (terminal) and not a pipe or file.
	// If it's a terminal, we don't want to block the TUI startup waiting for stdin.
	// We assume that if the user is running interactively, they will use the textinput.
	if info, err := os.Stdin.Stat(); err != nil || info.Mode()&os.ModeCharDevice == os.ModeCharDevice {
		close(ch)
		return
	}

	// If stdin is not a terminal (e.g., piped input or redirected from a file),
	// then proceed to read its content.
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		ch <- stdinMsg{ch, scanner.Text()}
	}
	close(ch)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd = m.handleKeyMsg(msg)
	case tea.WindowSizeMsg:
		m.handleWindowSizeMsg(msg)
	case stdinMsg:
		if msg.line != "" && msg.ch != nil {
			m.stdinContent += msg.line + "\n"
			m.errorMessage = ""
			cmd = tea.Batch(streamStdin(msg.ch), m.runCommand())
		}
	case commandResultMsg:
		m.handleCommandResultMsg(msg)
		m.updateWindow()
	}
	return m, cmd
}

func (m *model) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	switch {
	case key.Matches(msg, m.forceExit):
		m.quitting = true
		return tea.Quit
	case key.Matches(msg, m.exitAndPrint):
		m.command = m.textInput.Value()
		m.quitting = true
		return tea.Quit
	case key.Matches(msg, m.enter):
		m.textInput.Blur()
	case key.Matches(msg, m.esc):
		m.textInput.SetValue("")
		m.textInput.Blur()
	case key.Matches(msg, m.tab):
		if m.textInput.Focused() {
			m.textInput.Blur()
		} else {
			m.textInput.Focus()
		}
	default:
		if m.textInput.Focused() {
			// Every other key goes to the input box
			m.textInput, _ = m.textInput.Update(msg)
			m.errorMessage = ""
			cmd = m.runCommand()
		} else {
			// Handle keys on the view port
			switch {
			case key.Matches(msg, m.exit):
				m.quitting = true
				return tea.Quit
			case key.Matches(msg, m.copyResult):
				err := clipboard.WriteAll(m.rawOutput)
				if err != nil {
					m.errorMessage = fmt.Sprintf("Failed to copy to clipboard: %v", err)
				}
			case key.Matches(msg, m.toggleLineNum):
				m.showLineNumber = !m.showLineNumber
				m.refreshOutput()
			case key.Matches(msg, m.scrollTop):
				m.viewport.GotoTop()
			case key.Matches(msg, m.scrollBottom):
				m.viewport.GotoBottom()
			case key.Matches(msg, m.scrollBeginning):
				m.viewport.SetXOffset(0)
			case key.Matches(msg, m.scrollEnd):
				m.viewport.SetXOffset(math.MaxInt64)
			default:
				m.viewport, cmd = m.viewport.Update(msg)
			}
		}
	}
	return cmd
}

func (m *model) updateOutput(output string) {
	m.rawOutput = strings.TrimSuffix(output, "\n")
	m.processedOutput = addLineNumbers(m.rawOutput)
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
	availableWidth := max(0, m.winWidth-2) // -2 for left and right margin

	// Set component inner width
	m.textInput.Width = availableWidth - horizontalMargin*2 - 2 // Additional -2 to account for the prompt '> '
	m.viewport.Width = availableWidth - horizontalMargin*2

	// Update all component to have the same outter width
	inputStyle = inputStyle.Width(availableWidth)
	inputFocusedStyle = inputFocusedStyle.Width(availableWidth)
	// Embed text in top border of the output panel
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
	outputFocusedStyle = outputStyle.BorderForeground(focusedBorderColor)
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
	if msg.errorMessage != "" {
		m.errorMessage = msg.errorMessage
		// Output remains unchanged (last good state or stdin content)
	} else {
		m.updateOutput(msg.output)
		m.errorMessage = ""
	}
	m.viewport.GotoTop()
}

const (
	emptyRune = '\x00'
)

// parsePipedCommands splits a command string by pipes, respecting quotes.
func parsePipedCommands(cmdStr string) ([]string, error) {
	var commands []string
	var currentCommand strings.Builder
	var openQuote rune

	for _, r := range cmdStr {
		switch r {
		case '\'', '"', '`':
			switch openQuote {
			case emptyRune:
				// Start quote
				openQuote = r
			case r:
				// Close quote
				openQuote = emptyRune
			default:
				return nil, fmt.Errorf("malformatted command string: mismatched quotes %c & %c", openQuote, r)
			}
			currentCommand.WriteRune(r)
		case '|':
			if openQuote == emptyRune {
				// Not in a quoted string, | separates commands
				commands = append(commands, strings.TrimSpace(currentCommand.String()))
				currentCommand.Reset()
			} else {
				// Inside a quoted string, treat | as a normal rune in the command string
				currentCommand.WriteRune(r)
			}
		default:
			currentCommand.WriteRune(r)
		}
	}

	if currentCommand.Len() > 0 {
		commands = append(commands, strings.TrimSpace(currentCommand.String()))
	}

	if openQuote != emptyRune {
		return nil, fmt.Errorf("malformatted command string: unclose quote %c", openQuote)
	}

	return commands, nil
}

// runCommand executes the user-entered command on the stdin content
// in a separate goroutine and sends a commandResultMsg back.
func (m *model) runCommand() tea.Cmd {
	return func() tea.Msg {
		trimmedCmdStr := strings.TrimSpace(m.textInput.Value())
		if trimmedCmdStr == "" {
			return commandResultMsg{output: m.stdinContent, errorMessage: ""}
		}

		// Run commands one by one and pipe the previous command's output to next command's input
		commands, err := parsePipedCommands(trimmedCmdStr)
		if err != nil {
			return commandResultMsg{
				output:       "",
				errorMessage: err.Error(),
			}
		}
		var lastOutput bytes.Buffer
		lastOutput.WriteString(m.stdinContent)

		for _, cmdSegment := range commands {
			cmdSegment = strings.TrimSpace(cmdSegment)
			if cmdSegment == "" {
				return commandResultMsg{
					output:       "",
					errorMessage: "Syntax error: missing command between pipes",
				}
			}

			parts, err := shlex.Split(cmdSegment)
			if err != nil || len(parts) == 0 {
				return commandResultMsg{
					output:       "",
					errorMessage: fmt.Sprintf("Failed to parse command %s: %s", cmdSegment, err),
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
				return commandResultMsg{output: "", errorMessage: errMsg}
			}

			lastOutput = output
		}
		return commandResultMsg{output: lastOutput.String(), errorMessage: ""}
	}
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	var sections []string
	if m.textInput.Focused() {
		sections = append(sections, inputFocusedStyle.Render(m.textInput.View()))
	} else {
		sections = append(sections, inputStyle.Render(m.textInput.View()))
	}
	if m.errorMessage != "" {
		sections = append(sections, errorStyle.Render(m.errorMessage))
	}
	if m.textInput.Focused() {
		sections = append(sections, outputStyle.Render(m.viewport.View()))
	} else {
		sections = append(sections, outputFocusedStyle.Render(m.viewport.View()))
	}
	sections = append(sections, helpStyle.Render(helpTextViewMode))

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
