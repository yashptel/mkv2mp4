package deps

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// binFile returns the filename on disk for a tool, mirroring cachedPath.
func binFile(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func TestMemberMatches(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		suffix  string
		want    bool
	}{
		{"basename match", "ffmpeg-7.0-amd64-static/ffmpeg", "ffmpeg", true},
		{"exact match", "ffmpeg", "ffmpeg", true},
		{"case insensitive", "FFmpeg", "ffmpeg", true},
		{"windows path separator", "dovi_tool\\dovi_tool.exe", "dovi_tool.exe", true},
		{"nested suffix", "x/y/z/ffmpeg", "ffmpeg", true},
		{"no match", "ffmpeg-7.0/README", "ffmpeg", false},
		{"empty suffix", "anything", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := memberMatches(c.path, c.suffix); got != c.want {
				t.Errorf("memberMatches(%q, %q) = %v, want %v", c.path, c.suffix, got, c.want)
			}
		})
	}
}

func TestVerifySHA256_Empty(t *testing.T) {
	tmp := writeTempFile(t, []byte("hello"))
	defer os.Remove(tmp)
	var buf bytes.Buffer
	if err := verifySHA256(tmp, "", &buf); err != nil {
		t.Errorf("empty hash should skip verification, got %v", err)
	}
	if !strings.Contains(buf.String(), "skipping verification") {
		t.Errorf("expected warning message, got %q", buf.String())
	}
}

func TestVerifySHA256_OK(t *testing.T) {
	content := []byte("ffmpeg fake")
	tmp := writeTempFile(t, content)
	defer os.Remove(tmp)
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	if err := verifySHA256(tmp, hash, io.Discard); err != nil {
		t.Errorf("verifySHA256: got %v, want nil", err)
	}
}

func TestVerifySHA256_Mismatch(t *testing.T) {
	tmp := writeTempFile(t, []byte("hi"))
	defer os.Remove(tmp)
	err := verifySHA256(tmp, "deadbeef", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("expected mismatch error, got %v", err)
	}
}

func TestResolver_DownloadAndExtractZip(t *testing.T) {
	binContent := []byte("#!/bin/sh\necho ffmpeg fake\n")
	zipBytes := makeZip(t, "ffmpeg-7.0-static/ffmpeg", binContent)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Write(zipBytes)
	}))
	defer srv.Close()

	cache := t.TempDir()
	r := &Resolver{
		CacheDir:   cache,
		HTTPClient: srv.Client(),
		Logger:     io.Discard,
	}

	archivePath, err := r.download(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer os.Remove(archivePath)

	dest := filepath.Join(cache, "ffmpeg")
	if err := extractMember(archivePath, dest, ArchiveZip, "ffmpeg"); err != nil {
		t.Fatalf("extract: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binContent) {
		t.Errorf("extracted content mismatch")
	}
	if runtime.GOOS != "windows" {
		// Windows doesn't expose Unix exec bits; skip the perm check there.
		st, _ := os.Stat(dest)
		if st.Mode().Perm()&0o100 == 0 {
			t.Errorf("expected executable, got mode %v", st.Mode())
		}
	}
}

func TestResolver_DownloadAndExtractTarGz(t *testing.T) {
	binContent := []byte("dovi_tool fake binary")
	archive := makeTarGz(t, "dovi_tool", binContent)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	}))
	defer srv.Close()

	cache := t.TempDir()
	r := &Resolver{
		CacheDir:   cache,
		HTTPClient: srv.Client(),
		Logger:     io.Discard,
	}

	archivePath, err := r.download(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(archivePath)

	dest := filepath.Join(cache, "dovi_tool")
	if err := extractMember(archivePath, dest, ArchiveTarGz, "dovi_tool"); err != nil {
		t.Fatalf("extract: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, binContent) {
		t.Errorf("extracted content mismatch: got %q", string(got))
	}
}

func TestResolver_ResolveFromCache(t *testing.T) {
	cache := t.TempDir()
	binPath := filepath.Join(cache, binFile("ffprobe"))
	if err := os.WriteFile(binPath, []byte("cached"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := &Resolver{CacheDir: cache, Logger: io.Discard}
	// Hide ffprobe from PATH by clearing PATH so exec.LookPath fails.
	t.Setenv("PATH", "")

	got, err := r.Resolve(context.Background(), ToolFFprobe)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != binPath {
		t.Errorf("Resolve: got %q, want %q", got, binPath)
	}
}

func TestResolver_ResolveFromPath(t *testing.T) {
	// Put a fake ffmpeg on PATH and ensure Resolve returns it.
	fakeDir := t.TempDir()
	fakePath := filepath.Join(fakeDir, binFile("ffmpeg"))
	if err := os.WriteFile(fakePath, []byte("#!/bin/sh\necho fake\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir)

	r := &Resolver{CacheDir: t.TempDir(), Logger: io.Discard}
	got, err := r.Resolve(context.Background(), ToolFFmpeg)
	if err != nil {
		t.Fatal(err)
	}
	if got != fakePath {
		t.Errorf("Resolve: got %q, want %q", got, fakePath)
	}
}

// --- helpers ---

func writeTempFile(t *testing.T, data []byte) string {
	t.Helper()
	f, err := os.CreateTemp("", "deps-test-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func makeZip(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeTarGz(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o755,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()
	return buf.Bytes()
}
