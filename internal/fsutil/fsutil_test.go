package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExpand_SingleFile(t *testing.T) {
	dir := t.TempDir()
	mkv := filepath.Join(dir, "movie.mkv")
	touch(t, mkv)

	got, err := Expand([]string{mkv}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != mkv {
		t.Errorf("got %v, want [%s]", got, mkv)
	}
}

func TestExpand_RejectsNonMKVFile(t *testing.T) {
	dir := t.TempDir()
	mp4 := filepath.Join(dir, "movie.mp4")
	touch(t, mp4)

	_, err := Expand([]string{mp4}, false)
	if err == nil || !strings.Contains(err.Error(), "not a .mkv") {
		t.Errorf("expected 'not a .mkv' error, got %v", err)
	}
}

func TestExpand_DirectoryNonRecursive(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "a.mkv"))
	touch(t, filepath.Join(dir, "b.mkv"))
	touch(t, filepath.Join(dir, "ignored.txt"))
	touch(t, filepath.Join(dir, "nested", "c.mkv")) // should NOT be picked up

	got, err := Expand([]string{dir}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d files, want 2: %v", len(got), got)
	}
	for _, p := range got {
		if !strings.HasSuffix(p, "a.mkv") && !strings.HasSuffix(p, "b.mkv") {
			t.Errorf("unexpected file: %s", p)
		}
	}
}

func TestExpand_DirectoryRecursive(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "a.mkv"))
	touch(t, filepath.Join(dir, "nested", "b.mkv"))
	touch(t, filepath.Join(dir, "nested", "deeper", "c.mkv"))

	got, err := Expand([]string{dir}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("got %d files, want 3: %v", len(got), got)
	}
}

func TestExpand_CaseInsensitiveExt(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "MOVIE.MKV"))

	got, err := Expand([]string{dir}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %v, want 1 file", got)
	}
}

func TestExpand_Glob(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "ep1.mkv"))
	touch(t, filepath.Join(dir, "ep2.mkv"))
	touch(t, filepath.Join(dir, "ep1.srt"))

	got, err := Expand([]string{filepath.Join(dir, "*.mkv")}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d files, want 2: %v", len(got), got)
	}
}

func TestExpand_Deduplicates(t *testing.T) {
	dir := t.TempDir()
	mkv := filepath.Join(dir, "x.mkv")
	touch(t, mkv)

	got, err := Expand([]string{mkv, mkv}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %d files, want 1: %v", len(got), got)
	}
}

func TestExpand_NoMatchingGlob(t *testing.T) {
	_, err := Expand([]string{"/nonexistent/path/*.mkv"}, false)
	if err == nil {
		t.Error("expected error for non-matching glob")
	}
}

func TestOutputResolver_DefaultSameDir(t *testing.T) {
	r := &OutputResolver{}
	got := r.Output(filepath.FromSlash("/some/dir/movie.mkv"))
	want := filepath.FromSlash("/some/dir/movie.mp4")
	if got != want {
		t.Errorf("Output: got %q, want %q", got, want)
	}
}

func TestOutputResolver_CustomOutputDir(t *testing.T) {
	r := &OutputResolver{OutputDir: "/out"}
	got := r.Output("/some/dir/movie.mkv")
	want := filepath.Join("/out", "movie.mp4")
	if got != want {
		t.Errorf("Output: got %q, want %q", got, want)
	}
}

func TestOutputResolver_ShouldSkip(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "movie.mp4")
	touch(t, existing)

	r := &OutputResolver{}
	if !r.ShouldSkip(existing) {
		t.Error("expected ShouldSkip=true for existing file")
	}

	if r.ShouldSkip(filepath.Join(dir, "absent.mp4")) {
		t.Error("expected ShouldSkip=false for absent file")
	}

	r.Force = true
	if r.ShouldSkip(existing) {
		t.Error("expected ShouldSkip=false when Force is true")
	}
}
