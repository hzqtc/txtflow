# txtflow - TUI Pipe Processor

`txtflow` is a command-line tool with a terminal user interface (TUI) that allows you to pipe data through a series of commands, similar to how you would use pipes in a shell. It's designed for interactive data processing and exploration.

`txtflow` is heavily insipred by [`up`](https://github.com/akavel/up) but built with [`bubbletea`](https://github.com/charmbracelet/bubbletea).

![](https://raw.github.com/hzqtc/txtflow/master/demo.gif)

## Features

-   **Interactive Command Input:** Enter shell commands in a text input field.
-   **Piping:** Chain commands together using the `|` (pipe) symbol, just like in a shell.
-   **Stdin Support:** Reads data from standard input (`stdin`).
-   **TUI Display:** Displays the processed output within a scrollable viewport in the terminal.
-   **Error Handling:** Shows error messages in the TUI if a command fails.
-   **Exit Options:**
    -   `Ctrl+C`: Exits the application.
    -   `Ctrl+X`: Exits the application and prints the last entered command to standard output. This is useful for copying the command for later use.

## Installation

**Prerequisites:**

-   Go (version 1.18 or later) must be installed.

Clone the repo and run

```bash
make install
```

The binary would built and installed to `~/.local/bin`, make sure it is in your `$PATH`.

## Usage

1.  **Pipe data to `txtflow`:**

    ```bash
    cat myfile.txt | txtflow
    # OR
    echo "Hello, world!" | txtflow
    # OR
    txtflow < myfile.txt
    ```

2.  **Enter commands:**

    -   Once `txtflow` is running, a text input field will appear.
    -   Enter shell commands like `grep`, `wc`, `sed`, `awk`, etc.
    -   Use pipes (`|`) to chain multiple commands together.

    Example commands:

    ```
    grep hello
    grep error | wc -l
    sed 's/world/TUI/g'
    jq -r '.[].name' | sort | uniq
    ```

3.  **Execute commands:**

    -   Press `Enter` to execute the command. The output will be displayed in the viewport.

4.  **Exit:**

    -   Press `Ctrl+C` to exit the application.
    -   Press `Ctrl+X` to exit the application and print the last entered command to standard output.

## Dependencies

-   [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea): A Go framework for building terminal apps.
-   [charmbracelet/bubbles](https://github.com/charmbracelet/bubbles): Provides UI components for Bubble Tea.
-   [charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss): Style definitions for console layouts.
-   [google/shlex](https://github.com/google/shlex):  Shell-style syntax parser.
