package src

import (
	"testing"
	"time"
)

func TestSplitImageRef(t *testing.T) {
	cases := []struct{ ref, repo, tag string }{
		{"alpine:3.20", "alpine", "3.20"},
		{"localhost:5000/app:dev", "localhost:5000/app", "dev"},
		{"localhost:5000/app", "localhost:5000/app", "<none>"},
		{"<none>:<none>", "<none>", "<none>"},
	}
	for _, c := range cases {
		repo, tag := splitImageRef(c.ref)
		if repo != c.repo || tag != c.tag {
			t.Errorf("splitImageRef(%q) = %q, %q; want %q, %q", c.ref, repo, tag, c.repo, c.tag)
		}
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{0: "0B", 999: "999B", 7_800_000: "7.8MB", 1_230_000_000: "1.23GB"}
	for in, want := range cases {
		if got := humanSize(in); got != want {
			t.Errorf("humanSize(%d) = %q; want %q", in, got, want)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	cases := map[time.Duration]string{
		10 * time.Second:     "seconds",
		time.Minute:          "1 minute",
		5 * time.Hour:        "5 hours",
		3 * 24 * time.Hour:   "3 days",
		400 * 24 * time.Hour: "1 year",
	}
	for in, want := range cases {
		if got := humanDuration(in); got != want {
			t.Errorf("humanDuration(%v) = %q; want %q", in, got, want)
		}
	}
}
