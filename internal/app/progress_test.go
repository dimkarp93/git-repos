package app

import "testing"

func TestProgressLabelPhases(t *testing.T) {
	pr := &progress{}
	pr.setPlan(2)
	pr.setPhase("local scan")
	pr.add("~/tools/foo")
	if got, want := pr.label(), "phase 1/2 · local scan · ~/tools/foo · found — 1"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}

	pr.setPhase("checking branches")
	pr.setTotal(4)
	pr.begin("user/foo")
	pr.end()
	pr.begin("user/bar")
	if got, want := pr.label(), "phase 2/2 · checking branches · user/bar · 25% (1 of 4)"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}

	pr.begin("user/baz")
	if got, want := pr.label(), "phase 2/2 · checking branches · user/baz · 25% (1 of 4) · in flight — 2"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}

func TestProgressLabelColors(t *testing.T) {
	pr := &progress{color: true}
	pr.setPlan(2)
	pr.setPhase("local scan")
	pr.setTotal(4)
	pr.setUnit("directories")
	pr.step("~/tools/foo", 1)
	want := "\x1b[1mphase 1/2 · local scan\x1b[0m · \x1b[90m~/tools/foo\x1b[0m · \x1b[32m25% (1 of 4 directories)\x1b[0m"
	if got := pr.label(); got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}
