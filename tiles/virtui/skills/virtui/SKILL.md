---
name: virtui
description: |
  Use when an AI agent needs to programmatically drive, test, or record a terminal (TUI) application.
  Launches programs via pseudo-TTY, captures screen output, sends keystrokes and control sequences,
  and records sessions to asciicast format. Trigger on: launching interactive terminal programs,
  automating CLI workflows, testing TUI apps, recording terminal sessions, sending keystrokes to
  running programs, waiting for terminal output, or any task that requires controlling a terminal
  application non-interactively.
---

# virtui — TUI Automation for AI Agents

## Prerequisites

Before using virtui, ensure it is installed and the daemon is running.

### Check / Install

```bash
# Check if virtui is available
virtui version
```

If not installed, install via Homebrew:
```bash
brew install honeybadge-labs/tap/virtui
```

### Start the Daemon

The daemon must be running before any session commands. Always check first:

```bash
virtui --json daemon status
# → {"running":true,"socket":"..."} or {"running":false,"socket":"..."}
```

If not running:
```bash
virtui --json daemon start
# → {"pid":1234,"socket":"/Users/you/.virtui/daemon.sock"}
```

The daemon manages sessions over a Unix socket at `~/.virtui/daemon.sock`.

## Core Workflow

The typical workflow is: **start daemon → run session → interact → screenshot → kill session**.

### 1. Launch a Session

```bash
virtui --json run <command...>
```

This returns a `session_id` you'll use for all subsequent commands. Common options:

| Flag | Default | Purpose |
|------|---------|---------|
| `--cols` | `80` | Terminal width |
| `--rows` | `24` | Terminal height |
| `--dir` | cwd | Working directory |
| `-e KEY=VAL` | | Environment variables (repeatable) |
| `--record` | off | Record session as asciicast v2 |
| `--record-path` | auto | Custom recording path |

Example:
```bash
virtui --json run --cols 120 --rows 40 bash
# → {"session_id":"a1b2c3d4","pid":1234,"recording_path":""}
```

### 2. Execute Commands

`exec` is the primary interaction command. It types input, presses Enter, and optionally waits
for a screen condition before returning.

> **Claude Code note:** The Bash tool strips trailing newlines from command arguments, so
> `exec` may not actually send Enter when called from Claude Code. Prefer using `type` + `press Enter`
> as separate steps (or use a `pipeline`) to guarantee Enter is sent. See the
> [Pipeline section](#pipeline-batch-operations) for a concrete example.

> **Wait-condition caveat:** Wait conditions check the screen immediately after input is sent.
> If the target text already appears on screen (e.g., in the typed command itself), the wait
> can resolve in 0 ms — before the command's actual output appears. For reliable results, use
> a `pipeline` with separate `type` → `press Enter` → `wait` steps, or follow `exec` with a
> standalone `wait` command.

```bash
virtui --json exec <session_id> "<input>" [wait flags]
```

**Wait strategies** (use exactly one, or none to return immediately with current screen state):

| Flag | When to use |
|------|------------|
| `--wait "text"` | Wait for specific text to appear on screen |
| `--wait-stable` | Wait for screen to stop changing (500 ms of no updates — does **not** guarantee the process finished) |
| `--wait-gone "text"` | Wait for text to disappear (e.g., a loading spinner) |
| `--wait-regex "pattern"` | Wait for a regex match on screen |
| `--timeout <ms>` | Override default 30s timeout (use with any wait flag) |

Examples:
```bash
# Run a command and wait for its output
virtui --json exec $SID "echo hello" --wait "hello"

# Run a build and wait for screen to settle (500ms of no changes)
virtui --json exec $SID "make build" --wait-stable --timeout 60000

# Wait for a loading indicator to disappear
virtui --json exec $SID "npm install" --wait-gone "resolving" --timeout 120000
```

### 3. Take Screenshots

Read the current screen contents at any time:

```bash
virtui --json screenshot <session_id>
virtui screenshot <session_id> --no-color
```

Returns `screen_text`, `screen_hash`, `screen_ansi`, cursor position, and terminal dimensions.
Use `screen_hash` for cheap change detection without comparing full screen text.

- Use `screen_text` for plain text assertions and screen-diff logic.
- Use `screen_ansi` when color, background color, bold, underline, or reverse video matter.
- Use `virtui screenshot --no-color <session_id>` when you want plain text output in non-JSON mode
  without ANSI escape sequences.

### 4. Send Keystrokes

For interactive TUI applications (menus, editors, prompts), use `press` and `type`:

```bash
# Send special keys
virtui press <session_id> Enter
virtui press <session_id> Ctrl+C
virtui press <session_id> ArrowDown --repeat 5
virtui press <session_id> Escape q      # multiple keys in sequence

# Type text without pressing Enter (for search fields, partial input)
virtui type <session_id> "search query"
```

See [references/keys-and-errors.md](references/keys-and-errors.md) for the full list of available key names and error codes.

### 5. Wait for Conditions

Wait independently of exec (useful after press/type or for polling):

```bash
virtui --json wait <session_id> --text "Ready"
virtui --json wait <session_id> --stable
virtui --json wait <session_id> --gone "Loading..."
virtui --json wait <session_id> --regex "v\d+\.\d+"
```

### 6. Clean Up

Always kill sessions when done:

```bash
virtui kill <session_id>
```

List active sessions:
```bash
virtui --json sessions show
# → {"sessions":[{"session_id":"a1b2c3d4","pid":1234,"command":["bash"],"cols":80,"rows":24,"running":true,"exit_code":-1,"created_at":"1711900000","recording_path":""}]}
```

### 7. Resize a Session

Resize the terminal dimensions of a running session. Both `--cols` and `--rows` are required
(unlike `run`, there are no defaults):

```bash
virtui resize <session_id> --cols 120 --rows 40
```

## Pipeline (Batch Operations)

For complex multi-step interactions, use `pipeline` to send a batch of steps in one call.

> **Recommended for Claude Code:** Because `exec` relies on Enter being sent and Claude Code's
> Bash tool can swallow trailing newlines, the most reliable pattern from Claude Code is to use
> a pipeline with explicit `type` + `press Enter` steps instead of `exec`.

### Example: Running a command reliably from Claude Code

```bash
echo '{"steps":[
  {"type":{"text":"echo hello world"}},
  {"press":{"keys":["Enter"]}},
  {"wait":{"condition":{"text":"hello world"},"timeout_ms":5000}},
  {"screenshot":{}}
],"stop_on_error":true}' | virtui --json pipeline $SID
```

This is equivalent to `virtui exec $SID "echo hello world" --wait "hello world"` but
guarantees Enter is actually sent regardless of how the shell tool handles newlines.

### Pipeline from file

Instead of piping JSON via stdin, you can pass a file containing the steps:

```bash
virtui --json pipeline <session_id> --file steps.json
```

### General pipeline example

```bash
echo '{"steps":[
  {"exec":{"input":"ls","wait":{"text":"README"}}},
  {"sleep":{"duration_ms":500}},
  {"screenshot":{}},
  {"press":{"keys":["Ctrl+C"]}}
],"stop_on_error":true}' | virtui --json pipeline <session_id>
```

Step types: `exec`, `press`, `type`, `wait`, `screenshot`, `sleep`.

### Pipeline output format

The pipeline returns a JSON object with a `results` array. Each entry contains the step
outcome **and** a `screenshot` of the screen state at that point:

```json
{
  "results": [
    {
      "step_index": 0,
      "success": true,
      "error_message": "",
      "screenshot": {
        "screen_text": "...",
        "screen_hash": "...",
        "screen_ansi": "...",
        "cursor_row": 4,
        "cursor_col": 10,
        "cols": 80,
        "rows": 24
      }
    }
  ]
}
```

## Recording Sessions

Record sessions as asciicast v2 (playable with `asciinema play`):

```bash
virtui --json run --record bash
# or with a custom path:
virtui --json run --record --record-path ./demo.cast bash
```

Recording stops when the session is killed or the process exits.

> **Note:** `--record-path` requires `--record` to be set. Using `--record-path` alone will not enable recording.

## Important Patterns

- Always pass `--json` (or `-j`) for machine-readable output with `session_id`, `screen_text`, `screen_hash`, `screen_ansi`, etc.
- JSON output uses proto3 JSON encoding: `int64` fields (`elapsed_ms`, `created_at`) are serialized as strings.
- Use `screen_hash` (SHA-256) for cheap change detection without comparing full screen text.
- Prefer `screen_text` for text matching and `screen_ansi` only when TUI state depends on styling or color.
- The default wait timeout is 30s. For long-running ops, increase it: `--timeout 120000`.
- If a wait times out, take a `screenshot` to see current state and decide how to proceed.
- Errors include `code`, `category`, `message`, `retryable`, `suggestion` — check `retryable` before retrying.
See [references/keys-and-errors.md](references/keys-and-errors.md) for error codes and key names.

## JSON Output Fields Reference

| Command | Key Fields |
|---------|------------|
| `run` | `session_id`, `pid`, `recording_path` |
| `exec` | `screen_text`, `screen_hash`, `cursor_row`, `cursor_col`, `elapsed_ms` |
| `screenshot` | `screen_text`, `screen_hash`, `screen_ansi`, `cursor_row`, `cursor_col`, `cols`, `rows` |
| `press` | `screen_text`, `screen_hash` |
| `type` | `screen_text`, `screen_hash` |
| `wait` | `screen_text`, `screen_hash`, `elapsed_ms` |
| `kill` | `ok` |
| `resize` | `ok` |
| `pipeline` | `results[]`: `step_index`, `success`, `error_message`, `screenshot` (see [Pipeline output format](#pipeline-output-format)) |
| `sessions` | `sessions[]`: `session_id`, `pid`, `command`, `cols`, `rows`, `running`, `exit_code`, `created_at`, `recording_path` |
| `daemon start` | `pid`, `socket` (background) or `socket` (foreground) |
| `daemon stop` | `ok`, optionally `message` |
| `daemon status` | `running`, `socket` |
