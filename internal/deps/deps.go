// Package deps resolves the external binaries the tool depends on
// (ffmpeg, ffprobe, dovi_tool). Resolution order:
//  1. Look on $PATH.
//  2. Look in the per-user cache dir.
//  3. Download from the platform-specific pinned URL, verify SHA256
//     (if pinned), unpack, and install into the cache.
package deps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

type Tool string

const (
	ToolFFmpeg   Tool = "ffmpeg"
	ToolFFprobe  Tool = "ffprobe"
	ToolDoviTool Tool = "dovi_tool"
)

// AllTools is the canonical order: dependency-free tools first.
var AllTools = []Tool{ToolFFprobe, ToolFFmpeg, ToolDoviTool}

// Resolver finds and (if needed) installs tool binaries.
type Resolver struct {
	CacheDir   string
	HTTPClient *http.Client
	Update     bool      // skip PATH and cache; force re-download
	Logger     io.Writer // status messages
}

// NewResolver constructs a Resolver. If cacheDirOverride is empty, the standard
// user cache dir for the current OS is used.
func NewResolver(cacheDirOverride string) (*Resolver, error) {
	dir := cacheDirOverride
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("locate user cache dir: %w", err)
		}
		dir = filepath.Join(base, "mkv2mp4", "bin")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir %s: %w", dir, err)
	}
	return &Resolver{
		CacheDir:   dir,
		HTTPClient: &http.Client{Timeout: 10 * time.Minute},
		Logger:     os.Stderr,
	}, nil
}

// Resolve returns the absolute path to the named tool, installing it if needed.
func (r *Resolver) Resolve(ctx context.Context, tool Tool) (string, error) {
	if !r.Update {
		if path, err := exec.LookPath(string(tool)); err == nil {
			return path, nil
		}
		if path := r.cachedPath(tool); fileExists(path) {
			return path, nil
		}
	}
	return r.install(ctx, tool)
}

func (r *Resolver) cachedPath(tool Tool) string {
	name := string(tool)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(r.CacheDir, name)
}

func (r *Resolver) install(ctx context.Context, tool Tool) (string, error) {
	src, err := sourceFor(tool)
	if err != nil {
		return "", err
	}
	dest := r.cachedPath(tool)
	r.logf("Installing %s from %s ...\n", tool, src.URL)
	archivePath, err := r.download(ctx, src.URL)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", tool, err)
	}
	defer os.Remove(archivePath)
	if err := verifySHA256(archivePath, src.SHA256, r.Logger); err != nil {
		return "", err
	}
	member := src.Member
	if member == "" {
		member = string(tool)
		if runtime.GOOS == "windows" {
			member += ".exe"
		}
	}
	if err := extractMember(archivePath, dest, src.Archive, member); err != nil {
		return "", fmt.Errorf("extract %s: %w", tool, err)
	}
	r.logf("Installed %s -> %s\n", tool, dest)
	return dest, nil
}

func (r *Resolver) download(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	client := r.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	tmp, err := os.CreateTemp("", "mkv2mp4-dl-*")
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

func verifySHA256(path, expected string, logger io.Writer) error {
	if expected == "" {
		fmt.Fprintf(logger, "warning: no SHA256 pinned for %s; skipping verification\n", filepath.Base(path))
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		return fmt.Errorf("sha256 mismatch: got %s, expected %s", got, expected)
	}
	return nil
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func (r *Resolver) logf(format string, args ...any) {
	if r.Logger == nil {
		return
	}
	fmt.Fprintf(r.Logger, format, args...)
}
