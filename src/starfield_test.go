package src

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestStarfieldCompose(t *testing.T) {
	const w, h = 120, 40
	sf := newStarfield(w, h, 42)
	page := "+------+\n| HOME |\n+------+"

	for frame := 0; frame < 300; frame++ { // long enough for meteors to come and go
		out := sf.compose(page)
		lines := strings.Split(out, "\n")
		if len(lines) != h {
			t.Fatalf("frame %d: %d lines, want %d", frame, len(lines), h)
		}
		for y, l := range lines {
			if lw := lipgloss.Width(l); lw != w {
				t.Fatalf("frame %d line %d: width %d, want %d", frame, y, lw, w)
			}
		}

		// The page sits centred and untouched, with blank margins around it.
		top, left := (h-3)/2, (w-8)/2
		for i, want := range strings.Split(page, "\n") {
			row := []rune(sgr.ReplaceAllString(lines[top+i], ""))
			if got := string(row[left : left+8]); got != want {
				t.Fatalf("frame %d: content row %d = %q, want %q", frame, i, got, want)
			}
		}
		for y := top - clearMarginY; y < top+3+clearMarginY; y++ {
			row := []rune(sgr.ReplaceAllString(lines[y], ""))
			for x := left - clearMarginX; x < left+8+clearMarginX; x++ {
				if x >= left && x < left+8 && y >= top && y < top+3 {
					continue
				}
				if row[x] != ' ' {
					t.Fatalf("frame %d: star at (%d,%d) inside the clear margin", frame, x, y)
				}
			}
		}
		sf.step()
	}
}

func TestStarfieldMeteorComesAndGoes(t *testing.T) {
	sf := newStarfield(120, 40, 7)
	seen, ended := false, false
	for i := 0; i < 600; i++ {
		sf.step()
		if sf.meteor != nil {
			seen = true
		} else if seen {
			ended = true
			break
		}
	}
	if !seen || !ended {
		t.Errorf("meteor seen=%v ended=%v", seen, ended)
	}
}

func TestStarfieldDriftWraps(t *testing.T) {
	sf := newStarfield(50, 10, 1)
	for i := 0; i < 20000; i++ {
		sf.step()
	}
	for _, s := range sf.stars {
		if s.x < 0 || s.x >= 50 {
			t.Fatalf("star drifted off screen: x=%v", s.x)
		}
	}
}

func TestStarfieldFitsFallback(t *testing.T) {
	sf := newStarfield(10, 5, 1)
	if out := sf.compose(strings.Repeat("x", 20)); out != "" {
		t.Error("a page wider than the screen should fall back")
	}
}

func BenchmarkStarfieldFrame(b *testing.B) {
	sf := newStarfield(180, 52, 3)
	page := strings.Repeat(strings.Repeat("x", 90)+"\n", 20)
	for i := 0; i < b.N; i++ {
		sf.step()
		_ = sf.compose(page)
	}
}
