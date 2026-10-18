package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func scanningProgress() *progress {
	pr := &progress{}
	pr.setPlan(2)
	pr.setPhase("local scan")
	pr.add("~/tools/foo")
	return pr
}

func checkingProgress() *progress {
	pr := scanningProgress()
	pr.setPhase("checking branches")
	pr.setTotal(4)
	pr.begin("user/foo")
	pr.end()
	pr.begin("user/bar")
	return pr
}

func TestProgressLabelWhileScanning(t *testing.T) {
	assert.Equal(t, "phase 1/2 · local scan · ~/tools/foo · found — 1", scanningProgress().label())
}

func TestProgressLabelWhileChecking(t *testing.T) {
	assert.Equal(t, "phase 2/2 · checking branches · user/bar · 25% (1 of 4)", checkingProgress().label())
}

func TestProgressLabelInFlight(t *testing.T) {
	pr := checkingProgress()

	pr.begin("user/baz")

	assert.Equal(t, "phase 2/2 · checking branches · user/baz · 25% (1 of 4) · in flight — 2", pr.label())
}

func TestProgressLabelWithoutPlan(t *testing.T) {
	pr := &progress{}
	pr.setPhase("fetch")

	assert.Equal(t, "fetch", pr.label())
}

func TestProgressLabelColors(t *testing.T) {
	pr := &progress{color: true}
	pr.setPlan(2)
	pr.setPhase("local scan")
	pr.setTotal(4)
	pr.setUnit("directories")

	pr.step("~/tools/foo", 1)

	want := "\x1b[1mphase 1/2 · local scan\x1b[0m · \x1b[90m~/tools/foo\x1b[0m · \x1b[32m25% (1 of 4 directories)\x1b[0m"
	assert.Equal(t, want, pr.label())
}
