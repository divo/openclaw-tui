package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
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

// ResolveSessionFilePath reads the local sessions.json store and returns the
// JSONL conversation file path for the given session key.
func (t *CLITransport) ResolveSessionFilePath(sessionKey string) (string, error) {
	// The sessions list output tells us the store path on its first line:
	//   Session store: /path/to/sessions.json
	// We use a short-timeout context since this is a local CLI call.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	raw, err := t.SessionsList(ctx)
	if err != nil {
		return "", fmt.Errorf("sessions list: %w", err)
	}

	storePath := ParseSessionStorePath(raw)
	if storePath == "" {
		return "", fmt.Errorf("could not find session store path in sessions list output")
	}

	data, err := os.ReadFile(storePath)
	if err != nil {
		return "", fmt.Errorf("read sessions.json: %w", err)
	}

	var store map[string]struct {
		SessionFile string `json:"sessionFile"`
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return "", fmt.Errorf("parse sessions.json: %w", err)
	}

	entry, ok := store[sessionKey]
	if !ok {
		return "", fmt.Errorf("session %q not found in store", sessionKey)
	}
	if entry.SessionFile == "" {
		return "", fmt.Errorf("session %q has no file path", sessionKey)
	}
	return entry.SessionFile, nil
}

// SendAgentFire starts an agent turn in a background goroutine and returns a
// channel that emits nil (success) or an error when the turn completes.
// The caller doesn't need to wait — tail the JSONL file for the actual reply.
func (t *CLITransport) SendAgentFire(ctx context.Context, sessionKey, prompt string) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := t.SendAgent(ctx, sessionKey, prompt)
		done <- err
	}()
	return done
}

// ReadNewJSONLLines reads any new newline-delimited lines from filePath that
// start at byteOffset. Returns the lines, the updated offset, and any error.
func (t *CLITransport) ReadNewJSONLLines(filePath string, offset int64) ([]string, int64, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, offset, fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, fmt.Errorf("seek: %w", err)
	}

	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 512*1024), 512*1024) // lines can be large (tool output)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
		offset += int64(len(sc.Bytes())) + 1 // +1 for newline
	}
	if err := sc.Err(); err != nil {
		return lines, offset, err
	}
	return lines, offset, nil
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

// (no trailing constants)
