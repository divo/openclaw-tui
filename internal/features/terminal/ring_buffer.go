package terminal

import "strings"

type RingBuffer struct {
	MaxLines int
	MaxBytes int
	lines    []string
	bytes    int
}

func (r *RingBuffer) Append(chunk string) {
	if chunk == "" {
		return
	}
	for _, line := range strings.Split(chunk, "\n") {
		r.lines = append(r.lines, line)
		r.bytes += len(line)
	}
	r.trim()
}

func (r *RingBuffer) Lines() []string {
	if len(r.lines) == 0 {
		return nil
	}
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

func (r *RingBuffer) trim() {
	if r.MaxLines <= 0 && r.MaxBytes <= 0 {
		return
	}
	for len(r.lines) > 0 {
		overLines := r.MaxLines > 0 && len(r.lines) > r.MaxLines
		overBytes := r.MaxBytes > 0 && r.bytes > r.MaxBytes
		if !overLines && !overBytes {
			break
		}
		r.bytes -= len(r.lines[0])
		r.lines = r.lines[1:]
	}
}
