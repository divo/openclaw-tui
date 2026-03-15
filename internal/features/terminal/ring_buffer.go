package terminal

import (
	"regexp"
	"strings"
)

// ansiEscRe matches ANSI/VT100 escape sequences so we can strip them before
// storing output. We render into a lipgloss pane, not a real terminal, so raw
// escape codes produce garbage.
//
// Covers:
//   - CSI sequences:          ESC [ <params> <final>        e.g. ESC[1;32m ESC[2J ESC[?25h
//   - G0/G1 charset:          ESC ( <char>  ESC ) <char>    e.g. ESC(B
//   - Simple 2-char escapes:  ESC <letter>                   e.g. ESC M (reverse-index) ESC 7
var ansiEscRe = regexp.MustCompile(
	`\x1b\[[0-9;?]*[a-zA-Z]` + // CSI
		`|\x1b[()][0-9A-Za-z]` + // charset designation
		`|\x1b[A-Za-z0-9]`, // simple 2-char
)

// RingBuffer stores the last N lines of terminal output, with automatic
// trimming by line count and byte size.
type RingBuffer struct {
	MaxLines int
	MaxBytes int
	lines    []string
	bytes    int
	pending  string // incomplete line waiting for a newline
}

// Append ingests a raw PTY chunk: strips ANSI codes, normalises line endings,
// and flushes complete lines into the buffer. Partial (non-newline-terminated)
// content is held in pending until the next chunk completes it.
func (r *RingBuffer) Append(chunk string) {
	if chunk == "" {
		return
	}

	// 1. Strip ANSI escape sequences.
	chunk = ansiEscRe.ReplaceAllString(chunk, "")

	// 2. Strip remaining lone ESC bytes, BEL, NUL.
	chunk = strings.Map(func(c rune) rune {
		switch c {
		case 0x00, 0x07, 0x1b:
			return -1
		}
		return c
	}, chunk)

	// 3. Normalise CRLF → LF, then lone CR → LF.
	chunk = strings.ReplaceAll(chunk, "\r\n", "\n")
	chunk = strings.ReplaceAll(chunk, "\r", "\n")

	// 4. Prepend any previously incomplete line.
	data := r.pending + chunk
	r.pending = ""

	parts := strings.Split(data, "\n")

	// If data does NOT end with \n, the last element is a partial line.
	if !strings.HasSuffix(data, "\n") && len(parts) > 0 {
		r.pending = parts[len(parts)-1]
		parts = parts[:len(parts)-1]
	} else if len(parts) > 0 {
		// data ends with \n → trailing empty element is an artefact, discard.
		parts = parts[:len(parts)-1]
	}

	for _, line := range parts {
		r.lines = append(r.lines, line)
		r.bytes += len(line)
	}
	r.trim()
}

// Flush commits any pending partial line as-is (e.g. on session exit or prompt
// lines that never get a newline appended).
func (r *RingBuffer) Flush() {
	if r.pending != "" {
		r.lines = append(r.lines, r.pending)
		r.bytes += len(r.pending)
		r.pending = ""
		r.trim()
	}
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
