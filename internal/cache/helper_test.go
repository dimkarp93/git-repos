package cache

import (
	"os"
	"path/filepath"
)

func writeCorrupt(dir string) error {
	return os.WriteFile(filepath.Join(dir, fileName), []byte("{not json"), 0o644)
}
