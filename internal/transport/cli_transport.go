package transport

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type CLITransport struct{}

func NewCLITransport() *CLITransport {
	return &CLITransport{}
}

func (t *CLITransport) StatusAll(ctx context.Context) (string, error) {
	return t.run(ctx, "status", "--all")
}

func (t *CLITransport) SessionsList(ctx context.Context) (string, error) {
	return t.run(ctx, "sessions", "list")
}

func (t *CLITransport) DiscoverMainSession(ctx context.Context) (string, error) {
	out, err := t.SessionsList(ctx)
	if err != nil {
		return "", fmt.Errorf("sessions list: %w", err)
	}
	key := ParseMainSessionKey(out)
	if key == "" {
		return "", fmt.Errorf("no main session found")
	}
	return key, nil
}

func (t *CLITransport) SendAgent(ctx context.Context, sessionKey, prompt string) (string, error) {
	return t.run(ctx, "agent", "--session-id", NormalizeSessionID(sessionKey), "--message", prompt)
}

func (t *CLITransport) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, openclawBinary(), args...)
	cmd.Env = append(os.Environ(), "PATH=/home/divo/.npm-global/bin:/run/current-system/sw/bin:/usr/bin:/bin:"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			return "", err
		}
		return text, fmt.Errorf("%w | %s", err, firstLine(text))
	}
	return text, nil
}

func openclawBinary() string {
	candidates := []string{"openclaw", "/home/divo/.npm-global/bin/openclaw"}
	for _, c := range candidates {
		if strings.Contains(c, "/") {
			if _, err := os.Stat(c); err == nil {
				return c
			}
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return "openclaw"
}

func firstLine(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(s), "\n")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
