package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dimkarp93/git-repos/internal/cache"
	"github.com/dimkarp93/git-repos/internal/config"
)

func TestScanEstimateBuckets(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta", "gamma", ".hidden"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, name, "inner"), 0o755))
	}
	est := newScanEstimate(config.Default(), []string{root}, nil)
	if est.total != 3 || est.unit != "subtrees" {
		t.Fatalf("total = %d, unit = %q", est.total, est.unit)
	}
	resolved, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	require.Equal(t, 0, est.visit(resolved))
	require.Equal(t, 1, est.visit(filepath.Join(resolved, "beta", "inner")))
	require.Equal(t, 2, est.visit(filepath.Join(resolved, "gamma")))
}

func TestScanEstimateUsesCachedDirs(t *testing.T) {
	root := t.TempDir()
	store, err := cache.Open(t.TempDir())
	require.NoError(t, err)
	resolved, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	store.SetDirs(cache.ScanKey(resolved), 40)

	est := newScanEstimate(config.Default(), []string{root}, store)
	if !est.byDirs || est.total != 40 || est.unit != "directories" {
		t.Fatalf("est = %+v", est)
	}
	require.Equal(t, 1, est.visit(resolved))
	est.save(store)
	if dirs, ok := store.Dirs(cache.ScanKey(resolved)); !ok || dirs != 1 {
		t.Fatalf("dirs = %d, ok = %v", dirs, ok)
	}
}
