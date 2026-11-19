package src

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	containerTypes "github.com/docker/docker/api/types/container"
)

func TestDeleteImagePromptRefusesImageInUse(t *testing.T) {
	img := Image{ID: "sha256:aaa", Repository: "alpine", Tag: "3.20", Containers: 2}
	p, refusal := deleteImagePrompt(nil, img, []Image{img})
	if p != nil || !strings.Contains(refusal, "in use by 2 containers") {
		t.Fatalf("prompt=%v refusal=%q", p, refusal)
	}

	// An extra tag of an in-use image can still be removed: the image stays.
	extra := Image{ID: "sha256:aaa", Repository: "mytag", Tag: "tmp", Containers: 2}
	p, refusal = deleteImagePrompt(nil, extra, []Image{img, extra})
	if p == nil || refusal != "" || p.title != "Remove the tag mytag:tmp?" {
		t.Fatalf("extra tag: prompt=%+v refusal=%q", p, refusal)
	}
}

func TestDeleteImagePromptTagVsImage(t *testing.T) {
	a := Image{ID: "sha256:aaa", Repository: "alpine", Tag: "3.20", Size: 13_700_000}
	b := Image{ID: "sha256:aaa", Repository: "alpine", Tag: "latest", Size: 13_700_000}
	c := Image{ID: "sha256:ccc", Repository: "<none>", Tag: "<none>", Size: 1_000_000}
	all := []Image{a, b, c}

	p, _ := deleteImagePrompt(nil, b, all)
	if p == nil || p.title != "Remove the tag alpine:latest?" || !strings.Contains(p.lines[0], "also tagged alpine:3.20") {
		t.Errorf("tag prompt = %+v", p)
	}

	p, _ = deleteImagePrompt(nil, c, all)
	if p == nil || p.title != "Delete untagged image ccc?" || !strings.Contains(p.lines[0], "Frees 1MB") {
		t.Errorf("untagged prompt = %+v", p)
	}
	if imageRef(c) != "sha256:ccc" || imageRef(a) != "alpine:3.20" {
		t.Errorf("refs = %q, %q", imageRef(c), imageRef(a))
	}
}

func TestDeleteContainerPrompt(t *testing.T) {
	running := Container{Name: "web", Type: TypeContainer, State: containerTypes.StateRunning}
	p := deleteContainerPrompt(nil, running)
	if p.title != "Delete web?" || !strings.Contains(p.lines[0], "stopped and then deleted") {
		t.Errorf("running prompt = %+v", p)
	}

	stopped := Container{Name: "job", Type: TypeContainer, State: containerTypes.StateExited}
	if p := deleteContainerPrompt(nil, stopped); len(p.lines) != 1 {
		t.Errorf("stopped prompt should not mention stopping: %+v", p.lines)
	}

	stack := composeStackSummary("shop", []Container{running, stopped})
	p = deleteContainerPrompt(nil, stack)
	if p.title != "Delete all 2 containers in shop?" || !strings.Contains(p.lines[0], "1 of them are running") {
		t.Errorf("stack prompt = %+v", p)
	}
}

func TestHandleConfirmKey(t *testing.T) {
	ran := false
	p := &confirmPrompt{run: func() tea.Msg { ran = true; return nil }}

	if cmd, done := handleConfirmKey(p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}); cmd != nil || done {
		t.Error("other keys must be ignored")
	}
	if cmd, done := handleConfirmKey(p, tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil || !done {
		t.Error("esc must cancel without running")
	}
	cmd, done := handleConfirmKey(p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if !done || cmd == nil {
		t.Fatal("y must confirm")
	}
	cmd()
	if !ran {
		t.Error("y must return the action")
	}
}
