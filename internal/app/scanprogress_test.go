package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dimkarp93/git-repos/internal/cache"
	"github.com/dimkarp93/git-repos/internal/config"
)

func TestScanEstimateBuckets(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta", "gamma", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, name, "inner"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	est := newScanEstimate(config.Default(), []string{root}, nil)
	if est.total != 3 || est.unit != "subtrees" {
		t.Fatalf("total = %d, unit = %q", est.total, est.unit)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := est.visit(resolved); got != 0 {
		t.Fatalf("visit(root) = %d", got)
	}
	if got := est.visit(filepath.Join(resolved, "beta", "inner")); got != 1 {
		t.Fatalf("visit(beta) = %d", got)
	}
	if got := est.visit(filepath.Join(resolved, "gamma")); got != 2 {
		t.Fatalf("visit(gamma) = %d", got)
	}
}

func TestScanEstimateUsesCachedDirs(t *testing.T) {
	root := t.TempDir()
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	store.SetDirs(cache.ScanKey(resolved), 40)

	est := newScanEstimate(config.Default(), []string{root}, store)
	if !est.byDirs || est.total != 40 || est.unit != "directories" {
		t.Fatalf("est = %+v", est)
	}
	if got := est.visit(resolved); got != 1 {
		t.Fatalf("visit = %d", got)
	}
	est.save(store)
	if dirs, ok := store.Dirs(cache.ScanKey(resolved)); !ok || dirs != 1 {
		t.Fatalf("dirs = %d, ok = %v", dirs, ok)
	}
}
