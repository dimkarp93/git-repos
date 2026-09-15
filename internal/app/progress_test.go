package app

import "testing"

func TestProgressLabelPhases(t *testing.T) {
	pr := &progress{}
	pr.setPlan(2)
	pr.setPhase("локальный скан")
	pr.add("~/tools/foo")
	if got, want := pr.label(), "фаза 1/2 · локальный скан · ~/tools/foo · найдено — 1"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}

	pr.setPhase("проверка веток")
	pr.setTotal(4)
	pr.begin("user/foo")
	pr.end()
	pr.begin("user/bar")
	if got, want := pr.label(), "фаза 2/2 · проверка веток · user/bar · 25% (1 из 4)"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}

	pr.begin("user/baz")
	if got, want := pr.label(), "фаза 2/2 · проверка веток · user/baz · 25% (1 из 4) · в работе — 2"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}

func TestProgressLabelColors(t *testing.T) {
	pr := &progress{color: true}
	pr.setPlan(2)
	pr.setPhase("локальный скан")
	pr.setTotal(4)
	pr.setUnit("каталогов")
	pr.step("~/tools/foo", 1)
	want := "\x1b[1mфаза 1/2 · локальный скан\x1b[0m · \x1b[90m~/tools/foo\x1b[0m · \x1b[32m25% (1 из 4 каталогов)\x1b[0m"
	if got := pr.label(); got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}
