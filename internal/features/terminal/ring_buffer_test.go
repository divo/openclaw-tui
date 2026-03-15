package terminal

import (
	"strings"
	"testing"
)

func TestRingBuffer_basicCRLF(t *testing.T) {
	var rb RingBuffer
	rb.Append("hello\r\nworld\r\n")
	lines := rb.Lines()
	if len(lines) != 2 || lines[0] != "hello" || lines[1] != "world" {
		t.Fatalf("expected [hello world], got %v", lines)
	}
}

func TestRingBuffer_partialLine(t *testing.T) {
	var rb RingBuffer
	rb.Append("hel")
	if len(rb.Lines()) != 0 {
		t.Fatal("partial chunk should not produce a line yet")
	}
	rb.Append("lo\r\n")
	lines := rb.Lines()
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("expected [hello], got %v", lines)
	}
}

func TestRingBuffer_singleCharChunks(t *testing.T) {
	// Local echo delivers one char at a time — all should merge into one line.
	var rb RingBuffer
	for _, ch := range "bash$ " {
		rb.Append(string(ch))
	}
	if len(rb.Lines()) != 0 {
		t.Fatalf("single-char chunks without newline should not create lines yet: %v", rb.Lines())
	}
	// Newline flushes the accumulated pending.
	rb.Append("\n")
	lines := rb.Lines()
	if len(lines) != 1 || lines[0] != "bash$ " {
		t.Fatalf("expected [bash$ ], got %v", lines)
	}
}

func TestRingBuffer_ansiStripped(t *testing.T) {
	var rb RingBuffer
	// Colour codes, bold, cursor movement — none should survive.
	rb.Append("\x1b[1;32mOK\x1b[0m\r\n")
	lines := rb.Lines()
	if len(lines) != 1 || lines[0] != "OK" {
		t.Fatalf("ANSI codes should be stripped, got %v", lines)
	}
}

func TestRingBuffer_loneCR(t *testing.T) {
	var rb RingBuffer
	// Some programs use bare \r for progress overwrite.
	rb.Append("loading...\rDone\n")
	lines := rb.Lines()
	// \r becomes \n so we get two lines
	if len(lines) < 1 || lines[len(lines)-1] != "Done" {
		t.Fatalf("expected last line 'Done', got %v", lines)
	}
}

func TestRingBuffer_flush(t *testing.T) {
	var rb RingBuffer
	rb.Append("prompt$ ") // no newline
	if len(rb.Lines()) != 0 {
		t.Fatal("should be pending before Flush")
	}
	rb.Flush()
	lines := rb.Lines()
	if len(lines) != 1 || lines[0] != "prompt$ " {
		t.Fatalf("expected [prompt$ ] after Flush, got %v", lines)
	}
}

func TestRingBuffer_maxLinesTrim(t *testing.T) {
	rb := RingBuffer{MaxLines: 3}
	for i := 0; i < 5; i++ {
		rb.Append("line\n")
	}
	if len(rb.Lines()) > 3 {
		t.Fatalf("expected at most 3 lines, got %d", len(rb.Lines()))
	}
}

func TestRingBuffer_multilineChunk(t *testing.T) {
	var rb RingBuffer
	rb.Append("a\nb\nc\n")
	lines := rb.Lines()
	if len(lines) != 3 || strings.Join(lines, ",") != "a,b,c" {
		t.Fatalf("expected [a b c], got %v", lines)
	}
}
