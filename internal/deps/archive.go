package deps

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"
)

// extractMember extracts the file matching memberSuffix from a downloaded asset
// at archivePath into destBinPath. The expected file is copied with mode 0755.
//
// memberSuffix is matched as a path suffix (case-insensitive) so callers don't
// need to know the archive's top-level directory, which varies between builds.
// For ArchiveRaw, memberSuffix is ignored.
func extractMember(archivePath, destBinPath string, kind ArchiveKind, memberSuffix string) error {
	switch kind {
	case ArchiveRaw:
		return copyRaw(archivePath, destBinPath)
	case ArchiveZip:
		return extractZip(archivePath, destBinPath, memberSuffix)
	case ArchiveTarGz:
		return extractTar(archivePath, destBinPath, memberSuffix, gzipReader)
	case ArchiveTarXz:
		return extractTar(archivePath, destBinPath, memberSuffix, xzReader)
	}
	return fmt.Errorf("unknown archive kind: %d", kind)
}

func copyRaw(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return writeBinary(in, dst)
}

func extractZip(archivePath, destBinPath, memberSuffix string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !memberMatches(f.Name, memberSuffix) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open zip member %s: %w", f.Name, err)
		}
		err = writeBinary(rc, destBinPath)
		rc.Close()
		return err
	}
	return fmt.Errorf("member %q not found in %s", memberSuffix, archivePath)
}

type decompressor func(io.Reader) (io.Reader, error)

func gzipReader(r io.Reader) (io.Reader, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	return gr, nil
}

func xzReader(r io.Reader) (io.Reader, error) {
	return xz.NewReader(r)
}

func extractTar(archivePath, destBinPath, memberSuffix string, decompress decompressor) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	cr, err := decompress(f)
	if err != nil {
		return fmt.Errorf("decompress: %w", err)
	}
	tr := tar.NewReader(cr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		if !memberMatches(hdr.Name, memberSuffix) {
			continue
		}
		return writeBinary(tr, destBinPath)
	}
	return fmt.Errorf("member %q not found in %s", memberSuffix, archivePath)
}

func memberMatches(memberPath, suffix string) bool {
	if suffix == "" {
		return false
	}
	memberPath = strings.ReplaceAll(memberPath, "\\", "/")
	base := filepath.Base(memberPath)
	return strings.EqualFold(base, suffix) || strings.EqualFold(memberPath, suffix) ||
		strings.HasSuffix(strings.ToLower(memberPath), "/"+strings.ToLower(suffix))
}

func writeBinary(src io.Reader, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("write binary: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
