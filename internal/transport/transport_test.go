package transport

import "testing"

func TestNormalizeSessionID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		out  string
	}{
		{name: "empty defaults to main", in: "", out: "main"},
		{name: "plain id stays as is", in: "main", out: "main"},
		{name: "canonical direct key maps to id", in: "agent:main:main", out: "main"},
		{name: "non-direct key left untouched", in: "agent:main:cron:abc123", out: "agent:main:cron:abc123"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeSessionID(tc.in)
			if got != tc.out {
				t.Fatalf("NormalizeSessionID(%q) = %q, want %q", tc.in, got, tc.out)
			}
		})
	}
}

func TestParseMainSessionKey(t *testing.T) {
	raw := `Kind   Key                        Age
 direct agent:main:cron:123        1m ago
 direct agent:main:main            2m ago`

	got := ParseMainSessionKey(raw)
	if got != "agent:main:main" {
		t.Fatalf("ParseMainSessionKey() = %q, want %q", got, "agent:main:main")
	}
}
