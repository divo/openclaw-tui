# openclaw-tui

Status-first terminal UI for OpenClaw, built with Bubble Tea.

## Goals (v1)
- Header: gateway/session/model summary
- Left pane: current tasks + today log context
- Right pane: sessions + sub-agents
- Bottom pane: channel connection status/events

## Run

```bash
go run .
```

## Build

```bash
go build -o openclaw-tui .
./openclaw-tui
```

## Keybindings (current)
- `r` refresh
- `q` quit
- `ctrl+c` quit
