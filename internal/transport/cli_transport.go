package transport

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
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

// run executes an openclaw subcommand, killing the entire process group on
// context cancellation so that child processes don't keep stdout open.
func (t *CLITransport) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.Command(openclawBinary(), args...)
	cmd.Env = append(os.Environ(), "PATH=/home/divo/.npm-global/bin:/run/current-system/sw/bin:/usr/bin:/bin:"+os.Getenv("PATH"))
	// Put the child in its own process group so we can kill the whole tree.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		return "", err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		text := strings.TrimSpace(buf.String())
		if err != nil {
			if text == "" {
				return "", err
			}
			return text, fmt.Errorf("%w | %s", err, firstLine(text))
		}
		return text, nil

	case <-ctx.Done():
		// Kill the entire process group.
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		<-done // drain so the goroutine exits
		return "", ctx.Err()
	}
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
