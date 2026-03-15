package transport

import (
	"context"
	"strings"
)

type Transport interface {
	StatusAll(ctx context.Context) (string, error)
	SessionsList(ctx context.Context) (string, error)
	DiscoverMainSession(ctx context.Context) (string, error)
	SendAgent(ctx context.Context, sessionKey, prompt string) (string, error)

	// ResolveSessionFilePath returns the JSONL file path for the given session key
	// by reading the local sessions.json store.
	ResolveSessionFilePath(sessionKey string) (string, error)

	// SendAgentFire starts an agent turn in a goroutine and returns a channel
	// that will receive nil (success) or an error when the turn completes.
	// The caller does not need to wait on the channel — tailing the JSONL file
	// is the primary way to observe the reply.
	SendAgentFire(ctx context.Context, sessionKey, prompt string) <-chan error

	// ReadNewJSONLLines reads any new lines from filePath that appear after
	// byteOffset. Returns the new lines, the updated offset, and any error.
	ReadNewJSONLLines(filePath string, offset int64) ([]string, int64, error)
}

// ParseSessionStorePath extracts the sessions.json path from `openclaw sessions list` output.
// The first line is expected to be: "Session store: /path/to/sessions.json"
func ParseSessionStorePath(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		const prefix = "Session store:"
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func ParseMainSessionKey(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		kindIdx := 0
		if fields[0] == "-" {
			kindIdx = 1
		}
		if kindIdx+1 >= len(fields) {
			continue
		}
		if fields[kindIdx] != "direct" {
			continue
		}

		key := fields[kindIdx+1]
		if strings.Contains(key, ":cron:") {
			continue
		}
		return key
	}
	return ""
}

func NormalizeSessionID(sessionKey string) string {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return "main"
	}
	if strings.HasPrefix(strings.ToLower(key), "agent:") {
		parts := strings.Split(key, ":")
		// openclaw agent --session-id expects the raw session id (no colons),
		// while our UI tracks canonical keys like agent:main:main.
		// For the common direct key shape, map agent:<agentId>:<sessionId> -> <sessionId>.
		if len(parts) == 3 {
			if id := strings.TrimSpace(parts[2]); id != "" {
				return id
			}
		}
	}
	return key
}
