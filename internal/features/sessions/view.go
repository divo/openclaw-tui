package sessions

import (
	"strings"
)

func View(items []string, offset, height int) string {
	if height < 1 {
		return ""
	}
	if len(items) == 0 {
		return "(empty)"
	}
	if offset > len(items)-1 {
		offset = max(0, len(items)-1)
	}
	end := min(len(items), offset+height)
	out := make([]string, 0, height)
	for _, it := range items[offset:end] {
		out = append(out, "- "+compactLine(it, 100))
	}
	return strings.Join(out, "\n")
}
