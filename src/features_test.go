package src

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	containerTypes "github.com/docker/docker/api/types/container"
)

func TestCPUPercent(t *testing.T) {
	prev := cpuSample{total: 1_000, system: 100_000, online: 4}
	cur := cpuSample{total: 6_000, system: 200_000, online: 4}
	got, ok := cpuPercent(prev, cur)
	if !ok || got != 20 { // 5000/100000 * 4 CPUs * 100
		t.Errorf("cpuPercent = %v, %v; want 20, true", got, ok)
	}
	if _, ok := cpuPercent(cpuSample{}, cur); ok {
		t.Error("no previous sample should give no CPU figure")
	}
	stats := mergeStats(map[string]cpuSample{"a": prev}, map[string]cpuSample{"a": cur, "b": cur})
	if !stats["a"].hasCPU || stats["b"].hasCPU {
		t.Errorf("mergeStats: %+v", stats)
	}
}

func TestMemoryUsage(t *testing.T) {
	v2 := containerTypes.MemoryStats{Usage: 1000, Stats: map[string]uint64{"inactive_file": 300}}
	if got := memoryUsage(v2); got != 700 {
		t.Errorf("cgroup v2 = %d; want 700", got)
	}
	v1 := containerTypes.MemoryStats{Usage: 1000, Stats: map[string]uint64{"total_inactive_file": 100}}
	if got := memoryUsage(v1); got != 900 {
		t.Errorf("cgroup v1 = %d; want 900", got)
	}
	if got := humanBinarySize(5 * 1024 * 1024); got != "5MiB" {
		t.Errorf("humanBinarySize = %q", got)
	}
}

func TestPlanRestartAndPause(t *testing.T) {
	run := Container{Name: "web", Type: TypeContainer, State: containerTypes.StateRunning}
	exited := Container{Name: "job", Type: TypeContainer, State: containerTypes.StateExited}
	paused := Container{Name: "cache", Type: TypeContainer, State: containerTypes.StatePaused}

	if p, ok := planRestart(exited); !ok || p.verb != "restart" || p.done != "Restarted job" {
		t.Errorf("restart exited: %+v %v", p, ok)
	}
	if _, ok := planRestart(paused); ok {
		t.Error("restart should skip a paused container")
	}
	if p, ok := planPause(run); !ok || p.verb != "pause" {
		t.Errorf("pause running: %+v %v", p, ok)
	}
	if p, ok := planPause(paused); !ok || p.verb != "unpause" {
		t.Errorf("unpause paused: %+v %v", p, ok)
	}

	stack := composeStackSummary("shop", []Container{run, exited, paused})
	if p, ok := planRestart(stack); !ok || len(p.targets) != 2 || p.progress != "Restarting 2 containers in shop…" {
		t.Errorf("restart stack: %+v %v", p, ok)
	}
	if p, ok := planPause(stack); !ok || p.verb != "pause" || len(p.targets) != 1 {
		t.Errorf("pause stack: %+v %v", p, ok)
	}
}

func TestContainerFilterAndRunningOnly(t *testing.T) {
	web := Container{Name: "shop-web", Type: TypeContainer, ID: "aaaa", Image: "nginx", State: containerTypes.StateRunning}
	db := Container{Name: "shop-db", Type: TypeContainer, ID: "bbbb", Image: "postgres", State: containerTypes.StateExited}
	stack := composeStackSummary("shop", []Container{web, db})
	solo := Container{Name: "redis", Type: TypeContainer, ID: "cccc", Image: "redis", State: containerTypes.StateExited}

	l := listContainersModel{ShowChildrenSet: make(StringSet), filter: newFilterBar()}
	l.filter.field.value = "postgres"
	rows, items := l.getRows(l.visible([]Container{stack, solo}))
	// Matched through a service: the stack opens and shows only that service.
	if len(rows) != 2 || items[0].Name != "shop" || items[1].Name != "shop-db" {
		t.Fatalf("filter postgres: %v", names(items))
	}

	l.filter.field.value = "shop"
	_, items = l.getRows(l.visible([]Container{stack, solo}))
	if len(items) != 1 { // stack name matches; stays collapsed
		t.Errorf("filter shop: %v", names(items))
	}

	l.filter.field.value = ""
	l.runningOnly = true
	l.ShowChildrenSet.Add("shop")
	_, items = l.getRows(l.visible([]Container{stack, solo}))
	if strings.Join(names(items), ",") != "shop,shop-web" {
		t.Errorf("running only: %v", names(items))
	}
}

func names(cs []Container) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

func TestFilterBar(t *testing.T) {
	f := newFilterBar()
	f.open()
	typed := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	if !f.handle(typed("Web")) || f.query() != "web" || f.crumb() != "/Web" {
		t.Errorf("typing: %q %q", f.query(), f.crumb())
	}
	f.handle(tea.KeyMsg{Type: tea.KeyEnter})
	if f.editing || f.query() != "web" {
		t.Error("enter should keep the filter and stop editing")
	}
	f.open()
	if !f.handle(tea.KeyMsg{Type: tea.KeyEsc}) || f.query() != "" || f.editing {
		t.Error("esc should clear the filter")
	}
	if !matches("", "x") || !matches("eb", "WEB") || matches("zz", "web") {
		t.Error("matches")
	}
}

func TestHighlightAndAppendCapped(t *testing.T) {
	if got := highlight("no match here", "zzz"); got != "no match here" {
		t.Errorf("highlight without match = %q", got)
	}
	if got := highlight("Error and error", "error"); strings.Count(got, "rror") != 2 || !strings.Contains(got, "Error") {
		t.Errorf("highlight lost text: %q", got)
	}

	big := strings.Repeat("x", maxLogBytes-5) + "\n"
	got := appendCapped(big, "line two\nline three\n")
	if len(got) > maxLogBytes || !strings.HasSuffix(got, "line three\n") || strings.HasPrefix(got, "xxx") {
		t.Errorf("appendCapped kept %d bytes, prefix %q", len(got), got[:min(10, len(got))])
	}
}

func TestShellError(t *testing.T) {
	if shellError("web", nil) != nil {
		t.Error("nil error")
	}
	exit := func(code int) error {
		err := exec.Command("sh", "-c", "exit "+string(rune('0'+code/100))+string(rune('0'+code/10%10))+string(rune('0'+code%10))).Run()
		return err
	}
	if err := shellError("web", exit(1)); err != nil {
		t.Errorf("the shell's own exit status isn't an error: %v", err)
	}
	if err := shellError("web", exit(127)); err == nil || !strings.Contains(err.Error(), "no shell") {
		t.Errorf("127 should mean no shell: %v", err)
	}
	if err := shellError("web", errors.New("boom")); err == nil {
		t.Error("other errors are reported")
	}
}
