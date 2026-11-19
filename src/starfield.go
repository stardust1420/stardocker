package src

import (
	"math"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// A live starfield behind the home screen: three parallax layers drifting
// left, each star twinkling on its own phase, and now and then a shooting
// star. Stars never draw over (or right next to) the home screen content.

const (
	frameInterval = 100 * time.Millisecond // ~10 fps
	cellsPerStar  = 45                     // density: one star per this many cells
	clearMarginX  = 3                      // empty columns kept around the content
	clearMarginY  = 1                      // empty rows kept around the content
)

// frameMsg advances the animation of the home screen with the same id.
type frameMsg struct{ id int }

func frameCmd(id int) tea.Cmd {
	return tea.Tick(frameInterval, func(time.Time) tea.Msg { return frameMsg{id: id} })
}

// reducedMotion freezes the sky (stars stay, nothing moves) when
// STARDOCKER_REDUCED_MOTION is set to anything but "" or "0".
func reducedMotion() bool {
	v := os.Getenv("STARDOCKER_REDUCED_MOTION")
	return v != "" && v != "0"
}

type starLayer struct {
	share      float64 // fraction of all stars in this layer
	drift      float64 // cells per frame, leftward
	minLevel   int     // brightness range (index into starGreys)
	maxLevel   int
	sparkle    bool // glyph grows with brightness: · + ✦
	tintChance float64
}

var starLayers = []starLayer{
	{share: 0.62, drift: 0.008, minLevel: 0, maxLevel: 3},                                  // far
	{share: 0.30, drift: 0.022, minLevel: 2, maxLevel: 5, tintChance: 0.10},                // middle
	{share: 0.08, drift: 0.055, minLevel: 3, maxLevel: 7, sparkle: true, tintChance: 0.35}, // near
}

// Brightness levels, dim to bright.
var starGreys = []string{"236", "238", "240", "243", "246", "249", "252", "255"}

// Tints for the brightest levels of some stars.
var (
	starCool = lipgloss.Color("#a9c7ff")
	starWarm = lipgloss.Color("#ffd28a")
)

type star struct {
	x, y   float64
	layer  int
	phase  float64 // twinkle phase
	rate   float64 // twinkle speed, radians per frame
	tint   int     // 0 none, 1 cool, 2 warm
	glyphs string  // non-sparkle glyph
}

type meteor struct {
	x, y   float64
	dx, dy float64
	trail  [][2]int // previous head cells, newest first
}

type starfield struct {
	w, h       int
	stars      []star
	meteor     *meteor
	frame      int
	nextMeteor int
	rng        *rand.Rand
	cache      map[string]string // rendered glyphs by glyph+level+tint
}

func newStarfield(w, h int, seed uint64) *starfield {
	sf := &starfield{
		w:     w,
		h:     h,
		rng:   rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
		cache: make(map[string]string),
	}
	n := w * h / cellsPerStar
	for i := 0; i < n; i++ {
		layer := sf.pickLayer()
		s := star{
			x:     sf.rng.Float64() * float64(w),
			y:     float64(sf.rng.IntN(max(1, h))),
			layer: layer,
			phase: sf.rng.Float64() * 2 * math.Pi,
			rate:  0.04 + sf.rng.Float64()*0.16,
		}
		if sf.rng.Float64() < starLayers[layer].tintChance {
			s.tint = 1 + sf.rng.IntN(2)
		}
		s.glyphs = "·"
		if layer == 1 && sf.rng.IntN(3) == 0 {
			s.glyphs = "∙"
		}
		sf.stars = append(sf.stars, s)
	}
	sf.nextMeteor = 30 + sf.rng.IntN(80) // first one within a few seconds
	return sf
}

func (sf *starfield) pickLayer() int {
	r := sf.rng.Float64()
	for i, l := range starLayers {
		if r < l.share {
			return i
		}
		r -= l.share
	}
	return len(starLayers) - 1
}

// step advances one frame: drift, twinkle (implicit in frame), meteors.
func (sf *starfield) step() {
	sf.frame++
	for i := range sf.stars {
		s := &sf.stars[i]
		s.x -= starLayers[s.layer].drift
		if s.x < 0 {
			s.x += float64(sf.w)
		}
	}

	if sf.meteor != nil {
		m := sf.meteor
		m.trail = append([][2]int{{int(m.x), int(m.y)}}, m.trail...)
		if len(m.trail) > 7 {
			m.trail = m.trail[:7]
		}
		m.x += m.dx
		m.y += m.dy
		// Gone once the whole tail has left the screen.
		if last := m.trail[len(m.trail)-1]; last[0] < 0 || last[1] >= sf.h {
			sf.meteor = nil
			sf.nextMeteor = sf.frame + 80 + sf.rng.IntN(120) // 8–20 s
		}
	} else if sf.frame >= sf.nextMeteor {
		// Start in the upper right, head down-left at a shallow angle.
		sf.meteor = &meteor{
			x:  float64(sf.w/3 + sf.rng.IntN(max(1, sf.w*2/3))),
			y:  float64(sf.rng.IntN(max(1, sf.h/3))),
			dx: -(1.8 + sf.rng.Float64()*0.8),
			dy: 0.45 + sf.rng.Float64()*0.25,
		}
	}
}

// glyph renders a cell once and caches it.
func (sf *starfield) glyph(g string, level, tint int) string {
	key := g + string(rune('0'+level)) + string(rune('0'+tint))
	if out, ok := sf.cache[key]; ok {
		return out
	}
	color := lipgloss.Color(starGreys[level])
	if level >= 5 && tint == 1 {
		color = starCool
	} else if level >= 5 && tint == 2 {
		color = starWarm
	}
	out := lipgloss.NewStyle().Foreground(color).Render(g)
	sf.cache[key] = out
	return out
}

type starCell struct {
	x int
	s string
}

// cells returns this frame's visible stars, by row.
func (sf *starfield) cells() map[int][]starCell {
	rows := make(map[int][]starCell)
	put := func(x, y int, s string) {
		if x < 0 || x >= sf.w || y < 0 || y >= sf.h {
			return
		}
		rows[y] = append(rows[y], starCell{x, s})
	}

	for _, s := range sf.stars {
		l := starLayers[s.layer]
		// Brightness follows a sine wave, squared so stars spend most of
		// the time dim and flare briefly.
		wave := (math.Sin(s.phase+float64(sf.frame)*s.rate) + 1) / 2
		level := l.minLevel + int(math.Round(wave*wave*float64(l.maxLevel-l.minLevel)))
		g := s.glyphs
		if l.sparkle {
			switch {
			case level >= 7:
				g = "✦"
			case level >= 5:
				g = "+"
			default:
				g = "·"
			}
		}
		put(int(s.x), int(s.y), sf.glyph(g, level, s.tint))
	}

	if m := sf.meteor; m != nil {
		for i, p := range m.trail {
			level := max(1, 6-i)
			g := "·"
			if i < 2 {
				g = "•"
			}
			put(p[0], p[1], sf.glyph(g, level, 0))
		}
		put(int(m.x), int(m.y), sf.glyph("✦", 7, 0))
	}
	return rows
}

// compose centres page in a w×h screen and fills everything around it with
// the starfield, keeping a clear margin around the content. It returns ""
// if the page doesn't fit, so the caller can fall back to plain centring.
func (sf *starfield) compose(page string) string {
	lines := strings.Split(page, "\n")
	bw, bh := 0, len(lines)
	for _, l := range lines {
		bw = max(bw, lipgloss.Width(l))
	}
	if bw > sf.w || bh > sf.h {
		return ""
	}
	top, left := (sf.h-bh)/2, (sf.w-bw)/2

	// The clear rectangle around the content.
	clearTop, clearBottom := top-clearMarginY, top+bh+clearMarginY // [top, bottom)
	clearLeft, clearRight := max(0, left-clearMarginX), min(sf.w, left+bw+clearMarginX)

	rows := sf.cells()
	out := make([]string, sf.h)
	for y := 0; y < sf.h; y++ {
		var b strings.Builder
		cells := rows[y]
		// Later cells win (the meteor is drawn over stars).
		occupied := make(map[int]string, len(cells))
		for _, c := range cells {
			occupied[c.x] = c.s
		}

		inClear := y >= clearTop && y < clearBottom
		content := ""
		if y >= top && y < top+bh {
			content = lines[y-top]
		}

		for x := 0; x < sf.w; x++ {
			if inClear && x == clearLeft {
				// Write the content (padded to the clear width) in one go.
				b.WriteString(strings.Repeat(" ", left-clearLeft))
				b.WriteString(content)
				b.WriteString(strings.Repeat(" ", clearRight-left-lipgloss.Width(content)))
				x = clearRight - 1
				continue
			}
			if s, ok := occupied[x]; ok {
				b.WriteString(s)
			} else {
				b.WriteByte(' ')
			}
		}
		out[y] = b.String()
	}
	return strings.Join(out, "\n")
}
