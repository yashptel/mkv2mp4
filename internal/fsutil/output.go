package fsutil

import (
	"os"
	"path/filepath"
	"strings"
)

// OutputResolver computes the .mp4 destination for a given .mkv source.
type OutputResolver struct {
	// OutputDir is the destination directory. Empty means "same directory as source".
	OutputDir string
	// Force suppresses skip-on-exists behaviour.
	Force bool
}

// Output returns the .mp4 path for input. The parent directory is NOT created.
func (o *OutputResolver) Output(input string) string {
	base := filepath.Base(input)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	dir := o.OutputDir
	if dir == "" {
		dir = filepath.Dir(input)
	}
	return filepath.Join(dir, stem+".mp4")
}

// ShouldSkip returns true when an existing .mp4 should be left alone.
func (o *OutputResolver) ShouldSkip(output string) bool {
	if o.Force {
		return false
	}
	st, err := os.Stat(output)
	return err == nil && !st.IsDir()
}

// EnsureParentDir creates the parent directory of path with 0755 perms.
func EnsureParentDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}
