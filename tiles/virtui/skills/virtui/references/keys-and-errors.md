# Key Names and Error Codes

## Key Names

The `press` command accepts the following key names:

| Category | Keys |
|----------|------|
| Standard | `Enter`, `Tab`, `Backspace`, `Escape`, `Space`, `Delete` |
| Arrows | `ArrowUp`/`Up`, `ArrowDown`/`Down`, `ArrowRight`/`Right`, `ArrowLeft`/`Left` |
| Navigation | `Home`, `End`, `PageUp`, `PageDown`, `Insert` |
| Function | `F1` through `F12` |
| Ctrl | `Ctrl+A` through `Ctrl+Z`, `Ctrl+[`, `Ctrl+]`, `Ctrl+\` |

Single characters (e.g., `a`, `1`, `/`) are also accepted and sent as-is.

## Error Codes

All errors include structured fields:

| Field | Description |
|-------|-------------|
| `code` | Machine-readable error code |
| `category` | Error category (`SESSION`, `TERMINAL`, `TIMEOUT`, `VALIDATION`, `DAEMON`) |
| `message` | Human-readable description |
| `retryable` | Whether the operation can be retried |
| `suggestion` | Suggested action to resolve the error |

Common error codes:

| Code | Category | Retryable | Meaning |
|------|----------|-----------|---------|
| `SESSION_NOT_FOUND` | SESSION | no | Session was killed or doesn't exist |
| `SESSION_NOT_RUNNING` | SESSION | no | The process inside the session has exited |
| `TIMEOUT` | TIMEOUT | yes | Wait condition not met in time |
