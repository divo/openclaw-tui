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
		if fields[0] != "direct" {
			continue
		}
		key := fields[1]
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
	if strings.HasPrefix(key, "agent:") {
		parts := strings.Split(key, ":")
		if len(parts) >= 4 {
			id := strings.Join(parts[3:], ":")
			if strings.TrimSpace(id) != "" {
				return id
			}
		}
	}
	return key
}
