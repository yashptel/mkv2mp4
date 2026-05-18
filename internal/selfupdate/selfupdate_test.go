package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

func TestIsNewerVersion(t *testing.T) {
	cases := []struct {
		local, remote string
		want          bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.1.0", false},
		{"v1.0.0", "v1.0.0", false},
		{"0.1.0", "v0.2.0", true},   // missing v
		{"v0.1.0", "0.2.0", true},   // missing v on remote
		{"dev", "v0.1.0", true},     // dev is always older
		{"", "v0.1.0", true},        // empty is always older
		{"garbage", "v0.1.0", false}, // invalid local -> no update prompt
	}
	for _, c := range cases {
		t.Run(c.local+"_vs_"+c.remote, func(t *testing.T) {
			got := IsNewerVersion(c.local, c.remote)
			if got != c.want {
				t.Errorf("IsNewerVersion(%q, %q) = %v, want %v", c.local, c.remote, got, c.want)
			}
		})
	}
}

func TestAssetForPlatform(t *testing.T) {
	cases := []struct {
		version, goos, goarch string
		wantAsset             string
		wantFormat            ArchiveFormat
	}{
		{"v0.1.0", "darwin", "arm64", "mkv2mp4_0.1.0_darwin_arm64.tar.gz", ArchiveTarGz},
		{"v1.2.3", "linux", "amd64", "mkv2mp4_1.2.3_linux_amd64.tar.gz", ArchiveTarGz},
		{"v0.1.0", "windows", "amd64", "mkv2mp4_0.1.0_windows_amd64.zip", ArchiveZip},
		{"0.5.0", "linux", "arm64", "mkv2mp4_0.5.0_linux_arm64.tar.gz", ArchiveTarGz}, // missing v
	}
	for _, c := range cases {
		t.Run(c.goos+"_"+c.goarch, func(t *testing.T) {
			asset, format := AssetForPlatform(c.version, c.goos, c.goarch)
			if asset != c.wantAsset {
				t.Errorf("asset: got %q, want %q", asset, c.wantAsset)
			}
			if format != c.wantFormat {
				t.Errorf("format: got %d, want %d", format, c.wantFormat)
			}
		})
	}
}

func TestFindChecksum(t *testing.T) {
	body := `aaa111  mkv2mp4_0.1.0_linux_amd64.tar.gz
bbb222  mkv2mp4_0.1.0_darwin_arm64.tar.gz
ccc333  mkv2mp4_0.1.0_windows_amd64.zip
`
	cases := []struct {
		asset, want string
	}{
		{"mkv2mp4_0.1.0_linux_amd64.tar.gz", "aaa111"},
		{"mkv2mp4_0.1.0_darwin_arm64.tar.gz", "bbb222"},
		{"mkv2mp4_0.1.0_windows_amd64.zip", "ccc333"},
		{"absent.tar.gz", ""},
	}
	for _, c := range cases {
		t.Run(c.asset, func(t *testing.T) {
			if got := findChecksum(body, c.asset); got != c.want {
				t.Errorf("findChecksum(%q) = %q, want %q", c.asset, got, c.want)
			}
		})
	}
}

func TestExtractBinary_TarGz(t *testing.T) {
	want := []byte("fake-mkv2mp4-binary-bytes")
	archive := makeTarGz(t, "mkv2mp4_0.1.0_linux_amd64/mkv2mp4", want)
	archivePath := writeTempFile(t, archive)
	defer os.Remove(archivePath)

	got, err := ExtractBinary(archivePath, ArchiveTarGz, "mkv2mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(got)

	read, _ := os.ReadFile(got)
	if !bytes.Equal(read, want) {
		t.Errorf("extracted bytes mismatch")
	}
}

func TestExtractBinary_Zip(t *testing.T) {
	want := []byte("fake-windows-binary")
	archive := makeZip(t, "mkv2mp4_0.1.0_windows_amd64/mkv2mp4.exe", want)
	archivePath := writeTempFile(t, archive)
	defer os.Remove(archivePath)

	got, err := ExtractBinary(archivePath, ArchiveZip, "mkv2mp4.exe")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(got)

	read, _ := os.ReadFile(got)
	if !bytes.Equal(read, want) {
		t.Errorf("extracted bytes mismatch")
	}
}

func TestExtractBinary_MissingMember(t *testing.T) {
	archive := makeTarGz(t, "something/else", []byte("nope"))
	archivePath := writeTempFile(t, archive)
	defer os.Remove(archivePath)

	_, err := ExtractBinary(archivePath, ArchiveTarGz, "mkv2mp4")
	if err == nil {
		t.Error("expected error for missing member")
	}
}

func TestReplaceBinary_Unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-style atomic rename behavior")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "mkv2mp4")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "new")
	if err := os.WriteFile(src, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceBinary(target, src); err != nil {
		t.Fatalf("ReplaceBinary: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Errorf("target contents: got %q, want %q", got, "new")
	}
}

func TestReplaceBinary_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific .old rename dance")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "mkv2mp4.exe")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "new.exe")
	if err := os.WriteFile(src, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceBinary(target, src); err != nil {
		t.Fatalf("ReplaceBinary: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Errorf("target contents: got %q, want %q", got, "new")
	}
	if _, err := os.Stat(target + ".old"); err != nil {
		t.Errorf("expected .old to be present after Windows replace: %v", err)
	}
}

func TestRun_EndToEnd(t *testing.T) {
	// Build a mock GitHub server: /repos/.../releases/latest returns a tag,
	// /<repo>/releases/download/<tag>/<asset> serves the archive,
	// /<repo>/releases/download/<tag>/checksums.txt serves the checksum.
	binaryBytes := []byte("self-update-test-binary")
	memberName := "mkv2mp4"
	asset, format := AssetForPlatform("v9.9.9", runtime.GOOS, runtime.GOARCH)
	if format == ArchiveZip {
		memberName = "mkv2mp4.exe"
	}
	var archive []byte
	switch format {
	case ArchiveZip:
		archive = makeZip(t, memberName, binaryBytes)
	case ArchiveTarGz:
		archive = makeTarGz(t, memberName, binaryBytes)
	}
	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), asset)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/yashptel/mkv2mp4/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
	})
	mux.HandleFunc("/yashptel/mkv2mp4/releases/download/v9.9.9/"+asset, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/yashptel/mkv2mp4/releases/download/v9.9.9/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, checksums)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "mkv2mp4")
	if runtime.GOOS == "windows" {
		target += ".exe"
	}
	if err := os.WriteFile(target, []byte("OLD-VERSION"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), Options{
		Repo:           "yashptel/mkv2mp4",
		CurrentVersion: "v0.1.0",
		HTTPClient:     srv.Client(),
		APIBase:        srv.URL,
		DownloadBase:   srv.URL,
		TargetBinary:   target,
		Logger:         io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Updated {
		t.Errorf("expected Updated=true")
	}
	if res.LatestVersion != "v9.9.9" {
		t.Errorf("LatestVersion: got %q, want v9.9.9", res.LatestVersion)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binaryBytes) {
		t.Errorf("target was not replaced: got %q", string(got))
	}
}

func TestRun_AlreadyUpToDate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/yashptel/mkv2mp4/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v0.1.0"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	target := filepath.Join(t.TempDir(), "mkv2mp4")
	if err := os.WriteFile(target, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), Options{
		Repo:           "yashptel/mkv2mp4",
		CurrentVersion: "v0.1.0",
		HTTPClient:     srv.Client(),
		APIBase:        srv.URL,
		DownloadBase:   srv.URL,
		TargetBinary:   target,
		Logger:         io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated {
		t.Error("expected Updated=false when current == latest")
	}
}

func TestRun_BadChecksumAborts(t *testing.T) {
	asset, format := AssetForPlatform("v9.9.9", runtime.GOOS, runtime.GOARCH)
	memberName := "mkv2mp4"
	if format == ArchiveZip {
		memberName = "mkv2mp4.exe"
	}
	var archive []byte
	if format == ArchiveZip {
		archive = makeZip(t, memberName, []byte("payload"))
	} else {
		archive = makeTarGz(t, memberName, []byte("payload"))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/yashptel/mkv2mp4/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
	})
	mux.HandleFunc("/yashptel/mkv2mp4/releases/download/v9.9.9/"+asset, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/yashptel/mkv2mp4/releases/download/v9.9.9/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "deadbeef  %s\n", asset)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	target := filepath.Join(t.TempDir(), "mkv2mp4")
	if runtime.GOOS == "windows" {
		target += ".exe"
	}
	original := []byte("PRESERVED")
	if err := os.WriteFile(target, original, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Run(context.Background(), Options{
		Repo:           "yashptel/mkv2mp4",
		CurrentVersion: "v0.1.0",
		HTTPClient:     srv.Client(),
		APIBase:        srv.URL,
		DownloadBase:   srv.URL,
		TargetBinary:   target,
		Logger:         io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Errorf("expected sha256 mismatch error, got %v", err)
	}

	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, original) {
		t.Errorf("target should be untouched on checksum failure; got %q", got)
	}
}

func TestCheck_HasUpdate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/yashptel/mkv2mp4/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := Check(ctx, Options{
		Repo:           "yashptel/mkv2mp4",
		CurrentVersion: "v0.1.0",
		HTTPClient:     srv.Client(),
		APIBase:        srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasUpdate {
		t.Error("expected HasUpdate=true")
	}
	if res.LatestVersion != "v9.9.9" {
		t.Errorf("LatestVersion: got %q", res.LatestVersion)
	}
}

// --- helpers ---

func writeTempFile(t *testing.T, data []byte) string {
	t.Helper()
	f, err := os.CreateTemp("", "selfupdate-test-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func makeTarGz(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(data))}
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
	zw.Close()
	return buf.Bytes()
}
