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
