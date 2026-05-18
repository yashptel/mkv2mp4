// Package selfupdate implements `mkv2mp4 --update`: fetch the latest GitHub
// release for the running platform, verify its SHA256 against the release's
// checksums.txt, and atomically replace the running binary.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	defaultAPIBase      = "https://api.github.com"
	defaultDownloadBase = "https://github.com"
)

// Options configures a self-update operation.
type Options struct {
	Repo           string        // "owner/name" on GitHub
	CurrentVersion string        // e.g. "v0.1.1" or "dev"
	HTTPClient     *http.Client  // optional; defaults to a 5-minute-timeout client
	Logger         io.Writer     // optional status output
	APIBase        string        // override (tests)
	DownloadBase   string        // override (tests)
	TargetBinary   string        // override (tests); default = os.Executable()
}

// CheckResult is returned by Check.
type CheckResult struct {
	CurrentVersion string
	LatestVersion  string
	HasUpdate      bool
}

// UpdateResult is returned by Run.
type UpdateResult struct {
	CurrentVersion string
	LatestVersion  string
	Updated        bool   // true if the binary was actually replaced
	BinaryPath     string // path of the (now-updated) binary
}

// Check queries GitHub for the latest release without touching the local
// binary. Use this for a non-intrusive "are we out of date?" probe.
func Check(ctx context.Context, opts Options) (*CheckResult, error) {
	latest, err := latestVersion(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &CheckResult{
		CurrentVersion: opts.CurrentVersion,
		LatestVersion:  latest,
		HasUpdate:      IsNewerVersion(opts.CurrentVersion, latest),
	}, nil
}

// Run performs an end-to-end self-update.
func Run(ctx context.Context, opts Options) (*UpdateResult, error) {
	latest, err := latestVersion(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("query latest release: %w", err)
	}
	res := &UpdateResult{CurrentVersion: opts.CurrentVersion, LatestVersion: latest}

	if !IsNewerVersion(opts.CurrentVersion, latest) {
		return res, nil
	}

	target := opts.TargetBinary
	if target == "" {
		t, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("locate self: %w", err)
		}
		if resolved, err := filepath.EvalSymlinks(t); err == nil {
			t = resolved
		}
		target = t
	}
	res.BinaryPath = target

	asset, format := AssetForPlatform(latest, runtime.GOOS, runtime.GOARCH)
	url := fmt.Sprintf("%s/%s/releases/download/%s/%s", downloadBase(opts), opts.Repo, latest, asset)
	logf(opts, "Downloading %s\n", url)
	archive, err := downloadToTemp(ctx, client(opts), url)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer os.Remove(archive)

	if err := verifyChecksum(ctx, opts, latest, asset, archive); err != nil {
		return nil, err
	}

	memberName := "mkv2mp4"
	if runtime.GOOS == "windows" {
		memberName += ".exe"
	}
	tmpBin, err := ExtractBinary(archive, format, memberName)
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}
	defer os.Remove(tmpBin)

	if err := ReplaceBinary(target, tmpBin); err != nil {
		return nil, fmt.Errorf("replace binary: %w", err)
	}
	res.Updated = true
	return res, nil
}

// IsNewerVersion reports whether remote is strictly newer than local. A "dev"
// or empty local is always considered older than any tagged release.
func IsNewerVersion(local, remote string) bool {
	if local == "" || local == "dev" {
		return true
	}
	local = ensureV(local)
	remote = ensureV(remote)
	if !semver.IsValid(local) || !semver.IsValid(remote) {
		return false
	}
	return semver.Compare(local, remote) < 0
}

// AssetForPlatform returns the goreleaser-style asset name + archive format
// for the given platform.
func AssetForPlatform(version, goos, goarch string) (asset string, format ArchiveFormat) {
	plain := strings.TrimPrefix(version, "v")
	base := fmt.Sprintf("mkv2mp4_%s_%s_%s", plain, goos, goarch)
	if goos == "windows" {
		return base + ".zip", ArchiveZip
	}
	return base + ".tar.gz", ArchiveTarGz
}

// ArchiveFormat is the kind of release asset to expect for a platform.
type ArchiveFormat int

const (
	ArchiveZip ArchiveFormat = iota
	ArchiveTarGz
)

// ExtractBinary returns the path of a temp file containing the named member
// extracted from the archive.
func ExtractBinary(archivePath string, format ArchiveFormat, memberName string) (string, error) {
	out, err := os.CreateTemp("", "mkv2mp4-new-*")
	if err != nil {
		return "", err
	}
	outPath := out.Name()
	cleanup := func() { out.Close(); os.Remove(outPath) }

	switch format {
	case ArchiveZip:
		if err := extractZip(archivePath, out, memberName); err != nil {
			cleanup()
			return "", err
		}
	case ArchiveTarGz:
		if err := extractTarGz(archivePath, out, memberName); err != nil {
			cleanup()
			return "", err
		}
	default:
		cleanup()
		return "", fmt.Errorf("unknown archive format: %d", format)
	}
	if err := out.Close(); err != nil {
		os.Remove(outPath)
		return "", err
	}
	if err := os.Chmod(outPath, 0o755); err != nil {
		os.Remove(outPath)
		return "", err
	}
	return outPath, nil
}

func extractZip(archivePath string, out io.Writer, memberName string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if filepath.Base(f.Name) != memberName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		return err
	}
	return fmt.Errorf("member %q not found in zip", memberName)
}

func extractTarGz(archivePath string, out io.Writer, memberName string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if filepath.Base(hdr.Name) != memberName {
			continue
		}
		_, err = io.Copy(out, tr)
		return err
	}
	return fmt.Errorf("member %q not found in tar", memberName)
}

// ReplaceBinary atomically replaces target with src. On Windows, a running
// .exe can't be overwritten; we rename it to <target>.old first. The next
// invocation should call CleanupOldBinary() to remove the .old file once the
// previous process has exited.
func ReplaceBinary(target, src string) error {
	if err := os.Chmod(src, 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		oldPath := target + ".old"
		_ = os.Remove(oldPath) // ignore: file may not exist
		if err := os.Rename(target, oldPath); err != nil {
			return fmt.Errorf("rename current binary: %w", err)
		}
		if err := os.Rename(src, target); err != nil {
			// best-effort restore
			_ = os.Rename(oldPath, target)
			return fmt.Errorf("install new binary: %w", err)
		}
		return nil
	}
	// Unix: same-FS rename is atomic. Fall back to copy for cross-FS.
	if err := os.Rename(src, target); err == nil {
		return nil
	}
	return copyFile(src, target)
}

// CleanupOldBinary removes the <self>.old file that ReplaceBinary leaves
// behind on Windows. Safe to call at every startup; no-op on Unix and when
// no leftover exists.
func CleanupOldBinary() {
	if runtime.GOOS != "windows" {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	_ = os.Remove(self + ".old")
}

// --- helpers ---

type release struct {
	TagName string `json:"tag_name"`
}

func latestVersion(ctx context.Context, opts Options) (string, error) {
	if opts.Repo == "" {
		return "", errors.New("Repo is required")
	}
	url := fmt.Sprintf("%s/repos/%s/releases/latest", apiBase(opts), opts.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client(opts).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	var r release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if r.TagName == "" {
		return "", errors.New("release response missing tag_name")
	}
	return r.TagName, nil
}

func downloadToTemp(ctx context.Context, c *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	tmp, err := os.CreateTemp("", "mkv2mp4-update-*")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

func verifyChecksum(ctx context.Context, opts Options, version, asset, archivePath string) error {
	url := fmt.Sprintf("%s/%s/releases/download/%s/checksums.txt", downloadBase(opts), opts.Repo, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client(opts).Do(req)
	if err != nil {
		logf(opts, "warning: could not fetch checksums.txt; skipping verification (%v)\n", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		logf(opts, "warning: checksums.txt returned %d; skipping verification\n", resp.StatusCode)
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	expected := findChecksum(string(body), asset)
	if expected == "" {
		logf(opts, "warning: no checksum entry for %s; skipping verification\n", asset)
		return nil
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("sha256 mismatch: got %s, expected %s", actual, expected)
	}
	return nil
}

// findChecksum returns the SHA256 hex string for asset in a goreleaser-style
// checksums.txt body, or "" if not present.
func findChecksum(body, asset string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[len(fields)-1] == asset {
			return fields[0]
		}
	}
	return ""
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func client(opts Options) *http.Client {
	if opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func apiBase(opts Options) string {
	if opts.APIBase != "" {
		return opts.APIBase
	}
	return defaultAPIBase
}

func downloadBase(opts Options) string {
	if opts.DownloadBase != "" {
		return opts.DownloadBase
	}
	return defaultDownloadBase
}

func logf(opts Options, format string, args ...any) {
	if opts.Logger == nil {
		return
	}
	fmt.Fprintf(opts.Logger, format, args...)
}

func ensureV(s string) string {
	if strings.HasPrefix(s, "v") {
		return s
	}
	return "v" + s
}
