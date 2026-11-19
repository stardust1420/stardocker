package src

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	containerTypes "github.com/docker/docker/api/types/container"
)

func TestFormatPorts(t *testing.T) {
	ports := []containerTypes.Port{
		{IP: "::", PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
		{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
		{IP: "127.0.0.1", PrivatePort: 5432, PublicPort: 5433, Type: "tcp"},
		{PrivatePort: 9000, Type: "tcp"},
	}
	// Sorted by public port (unpublished first), then private port; the
	// IPv6 duplicate of 8080 is collapsed.
	want := "9000/tcp, 127.0.0.1:5433->5432/tcp, 8080->80/tcp"
	if got := formatPorts(ports); got != want {
		t.Errorf("formatPorts = %q; want %q", got, want)
	}
	if got := formatPorts(nil); got != "" {
		t.Errorf("formatPorts(nil) = %q; want empty", got)
	}
}

func TestComposeStackSummary(t *testing.T) {
	children := []Container{
		{Name: "db", State: containerTypes.StateRunning, Ports: []containerTypes.Port{{PrivatePort: 5432, PublicPort: 5432, Type: "tcp"}}},
		{Name: "web", State: containerTypes.StateExited},
	}
	s := composeStackSummary("shop", children)
	if s.Status != "1/2 running" || s.State != "partial" || s.Image != "2 services" {
		t.Errorf("summary = %q / %q / %q", s.Status, s.State, s.Image)
	}
	if got := formatPorts(s.Ports); got != "5432->5432/tcp" {
		t.Errorf("stack ports = %q", got)
	}
}

func TestEntriesItemsAligned(t *testing.T) {
	stack := composeStackSummary("shop", []Container{
		{Name: "db", Type: TypeContainer, ID: "aaaaaaaaaaaaaaaa"},
		{Name: "web", Type: TypeContainer, ID: "bbbbbbbbbbbbbbbb"},
	})
	solo := Container{Name: "solo", Type: TypeContainer, ID: "cccccccccccccccc"}

	// Stacks start open: heading, its services, then the standalone section.
	l := listContainersModel{Collapsed: make(StringSet)}
	entries, items := l.entries([]Container{stack, solo})
	if len(entries) != 5 || len(items) != 5 {
		t.Fatalf("expanded: %d entries, %d items", len(entries), len(items))
	}
	if !entries[0].heading || !entries[0].selectable || entries[0].title != "▾ shop" {
		t.Errorf("stack heading = %+v", entries[0])
	}
	if items[1].Name != "db" || items[2].Name != "web" || items[4].Name != "solo" {
		t.Errorf("unexpected order: %v", names(items))
	}
	if !entries[3].heading || entries[3].selectable || entries[3].title != "Standalone" {
		t.Errorf("standalone heading = %+v", entries[3])
	}

	l.Collapsed.Add("shop")
	entries, items = l.entries([]Container{stack, solo})
	if len(entries) != 3 || len(items) != 3 || entries[0].title != "▸ shop" {
		t.Fatalf("collapsed: %d entries, %d items, title %q", len(entries), len(items), entries[0].title)
	}

	// Without stacks there is no "Standalone" heading.
	entries, _ = l.entries([]Container{solo})
	if len(entries) != 1 || entries[0].heading {
		t.Errorf("solo only: %+v", entries)
	}
}

func TestPlanStartStop(t *testing.T) {
	web := Container{Name: "web", Type: TypeContainer, State: containerTypes.StateRunning}
	db := Container{Name: "db", Type: TypeContainer, State: containerTypes.StateRunning}
	worker := Container{Name: "worker", Type: TypeContainer, State: containerTypes.StateExited}
	fresh := Container{Name: "fresh", Type: TypeContainer, State: containerTypes.StateCreated}
	paused := Container{Name: "paused", Type: TypeContainer, State: containerTypes.StatePaused}

	names := func(cs []Container) []string {
		out := []string{}
		for _, c := range cs {
			out = append(out, c.Name)
		}
		return out
	}

	if p, ok := planStartStop(web); !ok || p.verb != "stop" || p.done != "Stopped web" {
		t.Errorf("running container: %+v %v", p, ok)
	}
	if p, ok := planStartStop(fresh); !ok || p.verb != "start" || p.done != "Started fresh" {
		t.Errorf("created container: %+v %v", p, ok)
	}
	if _, ok := planStartStop(paused); ok {
		t.Error("paused container: s should do nothing")
	}

	// Partly running stack: stop only the running services.
	p, ok := planStartStop(composeStackSummary("demo", []Container{db, web, worker}))
	if !ok || p.verb != "stop" || strings.Join(names(p.targets), ",") != "db,web" || p.progress != "Stopping 2 containers in demo…" {
		t.Errorf("partial stack: %+v %v", p, ok)
	}

	// Fully stopped stack: start everything that can start.
	stopped := composeStackSummary("demo", []Container{worker, fresh, paused})
	p, ok = planStartStop(stopped)
	if !ok || p.verb != "start" || strings.Join(names(p.targets), ",") != "worker,fresh" || p.done != "Started 2 containers in demo" {
		t.Errorf("stopped stack: %+v %v", p, ok)
	}
}

func TestEscReturnsHomeWithCardSelected(t *testing.T) {
	esc := tea.KeyMsg{Type: tea.KeyEsc}

	m, _ := InitListImagesModel(nil, 120, 40).Update(esc)
	if home, ok := m.(indexModel); !ok || home.selected != cardImages {
		t.Errorf("from images: got %T selected=%v", m, m)
	}

	m, _ = InitListContainersModel(nil, 120, 40).Update(esc)
	if home, ok := m.(indexModel); !ok || home.selected != cardContainers {
		t.Errorf("from containers: got %T", m)
	}
}
