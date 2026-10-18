package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dimkarp93/git-repos/internal/cache"
	"github.com/dimkarp93/git-repos/internal/config"
)

func resolvedTemp(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)

	return root, resolved
}

func subtreeEstimate(t *testing.T) (*scanEstimate, string) {
	t.Helper()

	root, resolved := resolvedTemp(t)
	for _, name := range []string{"alpha", "beta", "gamma", ".hidden"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, name, "inner"), 0o755))
	}

	return newScanEstimate(config.Default(), []string{root}, nil), resolved
}

func TestScanEstimatePlansSubtrees(t *testing.T) {
	est, _ := subtreeEstimate(t)

	assert.Equal(t, []any{3, "subtrees"}, []any{est.total, est.unit})
}

func TestScanEstimateBuckets(t *testing.T) {
	est, resolved := subtreeEstimate(t)

	got := []int{
		est.visit(resolved),
		est.visit(filepath.Join(resolved, "beta", "inner")),
		est.visit(filepath.Join(resolved, "gamma")),
		est.visit(filepath.Join(resolved, "alpha")),
		est.visit("/elsewhere"),
	}

	assert.Equal(t, []int{0, 1, 2, 2, 2}, got)
}

func cachedEstimate(t *testing.T) (*scanEstimate, *cache.Cache, string) {
	t.Helper()

	root, resolved := resolvedTemp(t)
	store, err := cache.Open(t.TempDir())
	require.NoError(t, err)
	store.SetDirs(cache.ScanKey(resolved), 40)

	return newScanEstimate(config.Default(), []string{root}, store), store, resolved
}

func TestScanEstimateUsesCachedDirs(t *testing.T) {
	est, _, _ := cachedEstimate(t)

	assert.Equal(t, []any{true, 40, "directories"}, []any{est.byDirs, est.total, est.unit})
}

func TestScanEstimateSavesVisitedDirs(t *testing.T) {
	est, store, resolved := cachedEstimate(t)

	t.Run("counts visits", func(t *testing.T) {
		assert.Equal(t, 1, est.visit(resolved))
	})
	t.Run("saves count", func(t *testing.T) {
		est.save(store)
		dirs, ok := store.Dirs(cache.ScanKey(resolved))
		assert.Equal(t, []any{1, true}, []any{dirs, ok})
	})
}
