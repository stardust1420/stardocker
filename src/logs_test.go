package src

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	containerTypes "github.com/docker/docker/api/types/container"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestDescribeContainerState(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339Nano) }

	cases := []struct {
		name string
		st   *containerTypes.State
		gone bool
		want string
	}{
		{"running", &containerTypes.State{Running: true, Status: "running", StartedAt: ago(12 * time.Minute)}, false, "● Running for 12 minutes"},
		{"exited", &containerTypes.State{Status: "exited", ExitCode: 1, FinishedAt: ago(3 * time.Minute)}, false, "○ Exited with code 1, 3 minutes ago"},
		{"oom", &containerTypes.State{Status: "exited", ExitCode: 137, OOMKilled: true, FinishedAt: ago(time.Hour)}, false, "○ Exited out of memory, 1 hour ago"},
		{"created", &containerTypes.State{Status: "created", FinishedAt: "0001-01-01T00:00:00Z"}, false, "○ Not started yet"},
		{"paused", &containerTypes.State{Running: true, Paused: true, Status: "paused"}, false, "◐ Paused"},
		{"gone", nil, true, "○ Deleted (showing its last logs)"},
		{"unknown", nil, false, "State unknown"},
		{"just started", &containerTypes.State{Running: true, Status: "running", StartedAt: ago(5 * time.Second)}, false, "● Running for a few seconds"},
	}
	for _, c := range cases {
		got := ansiRe.ReplaceAllString(describeContainerState(c.st, c.gone, now), "")
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLastLogTimestampAndSince(t *testing.T) {
	logs := "2026-11-19T00:00:01.000000001Z tick 1\n2026-11-19T00:00:03.500000000Z tick 2\n\n"
	ts, ok := lastLogTimestamp(logs)
	if !ok || ts.Format(time.RFC3339Nano) != "2026-11-19T00:00:03.5Z" {
		t.Fatalf("lastLogTimestamp = %v, %v", ts, ok)
	}
	if _, ok := lastLogTimestamp("no timestamps here"); ok {
		t.Error("expected no timestamp")
	}

	l := logsModel{}
	if l.since() != "1h" {
		t.Errorf("default since = %q", l.since())
	}
	l.hours = 1.5
	if l.since() != "1.5h" {
		t.Errorf("1.5 hours since = %q", l.since())
	}
	l.clearedAfter = ts
	if got := l.since(); got != "2026-11-19T00:00:03.500000001Z" {
		t.Errorf("cleared since = %q", got)
	}
}

func TestParseAndFormatHours(t *testing.T) {
	for in, want := range map[string]float64{"6": 6, " 0.5 ": 0.5, "48": 48} {
		if got, err := parseHours(in); err != nil || got != want {
			t.Errorf("parseHours(%q) = %v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "0", ".", "abc", "9999999"} {
		if _, err := parseHours(bad); err == nil {
			t.Errorf("parseHours(%q) should fail", bad)
		}
	}
	for in, want := range map[float64]string{0.5: "30 minutes", 1: "1 hour", 1.5: "1.5 hours", 24: "24 hours", 0.01: "1 minute"} {
		if got := formatHours(in); got != want {
			t.Errorf("formatHours(%v) = %q; want %q", in, got, want)
		}
	}
}

func TestNumberField(t *testing.T) {
	f := newRangeInput()
	typed := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	f.update(typed("1a2.5.3"))
	if f.value != "12.53" {
		t.Errorf("value = %q; want letters and a second dot ignored", f.value)
	}
	f.update(tea.KeyMsg{Type: tea.KeyBackspace})
	if f.value != "12.5" {
		t.Errorf("after backspace = %q", f.value)
	}
	f.update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if f.value != "" {
		t.Errorf("after ctrl+u = %q", f.value)
	}
	f.update(typed(strings.Repeat("9", 20)))
	if len(f.value) != f.limit {
		t.Errorf("limit not enforced: %q", f.value)
	}
}
