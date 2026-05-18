// Package fsutil handles input expansion (files, folders, globs) and output
// path resolution for the mkv2mp4 CLI.
package fsutil

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const mkvExt = ".mkv"

// Expand turns CLI positional arguments into a sorted, de-duplicated list of
// .mkv files. Arguments may be:
//   - a single .mkv file path
//   - a directory (scanned for .mkv files; recursive if true)
//   - a glob pattern (expanded via filepath.Glob)
//
// Symlinks are followed. Unknown extensions are skipped silently when the arg
// refers to a directory; a non-mkv file arg is returned as an error so the
// user notices the typo.
func Expand(args []string, recursive bool) ([]string, error) {
	seen := make(map[string]struct{})
	var out []string

	add := func(path string) {
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}
		if _, ok := seen[abs]; ok {
			return
		}
		seen[abs] = struct{}{}
		out = append(out, path)
	}

	for _, raw := range args {
		paths, err := resolveOne(raw)
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			info, err := os.Stat(p)
			if err != nil {
				return nil, fmt.Errorf("stat %s: %w", p, err)
			}
			if info.IsDir() {
				files, err := walkDir(p, recursive)
				if err != nil {
					return nil, err
				}
				for _, f := range files {
					add(f)
				}
				continue
			}
			if !hasMKVExt(p) {
				return nil, fmt.Errorf("%s: not a .mkv file", p)
			}
			add(p)
		}
	}

	sort.Strings(out)
	return out, nil
}

// resolveOne returns concrete filesystem paths for one arg. If arg exists
// directly, it returns [arg]. Otherwise, it tries to interpret arg as a glob.
func resolveOne(arg string) ([]string, error) {
	if _, err := os.Stat(arg); err == nil {
		return []string{arg}, nil
	}
	matches, err := filepath.Glob(arg)
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", arg, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("%s: no such file or directory", arg)
	}
	return matches, nil
}

// walkDir returns .mkv files under dir. With recursive=false, only direct
// children are considered.
func walkDir(dir string, recursive bool) ([]string, error) {
	var out []string
	if recursive {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if hasMKVExt(path) {
				out = append(out, path)
			}
			return nil
		})
		return out, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := filepath.Join(dir, e.Name())
		if hasMKVExt(name) {
			out = append(out, name)
		}
	}
	return out, nil
}

func hasMKVExt(path string) bool {
	return strings.EqualFold(filepath.Ext(path), mkvExt)
}
