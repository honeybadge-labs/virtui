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

All errors include structured fields. When `--json` is set, errors are returned as JSON to stdout:

```json
{
  "code": "SESSION_NOT_FOUND",
  "category": "ERROR_CATEGORY_SESSION",
  "message": "session \"abc\" not found",
  "retryable": false,
  "suggestion": "Check the session ID with 'virtui sessions'.",
  "context": {"session_id": "abc"}
}
```

Non-gRPC errors (e.g., daemon not reachable) are returned as:
```json
{"code": "UNKNOWN", "message": "..."}
```

| Field | Description |
|-------|-------------|
| `code` | Machine-readable error code |
| `category` | Error category (`ERROR_CATEGORY_SESSION`, `ERROR_CATEGORY_TERMINAL`, `ERROR_CATEGORY_TIMEOUT`, `ERROR_CATEGORY_VALIDATION`, `ERROR_CATEGORY_DAEMON`) |
| `message` | Human-readable description |
| `retryable` | Whether the operation can be retried |
| `suggestion` | Suggested action to resolve the error |
| `context` | Map of key-value pairs with additional context (e.g., `session_id`) |

Common error codes:

| Code | Category | Retryable | Meaning |
|------|----------|-----------|---------|
| `SESSION_NOT_FOUND` | SESSION | no | Session was killed or doesn't exist |
| `SESSION_NOT_RUNNING` | SESSION | no | The process inside the session has exited |
| `TIMEOUT` | TIMEOUT | yes | Wait condition not met in time |
| `VALIDATION_ERROR` | VALIDATION | no | Invalid command arguments |
| `TERMINAL_ERROR` | TERMINAL | no | Terminal-related error |
| `DAEMON_ERROR` | DAEMON | no | Daemon-related error |
