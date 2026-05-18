package deps

import (
	"fmt"
	"runtime"
)

// ArchiveKind identifies how a downloaded asset is packaged.
type ArchiveKind int

const (
	ArchiveRaw   ArchiveKind = iota // single binary, no archive
	ArchiveZip                      // .zip
	ArchiveTarGz                    // .tar.gz / .tgz
	ArchiveTarXz                    // .tar.xz
)

// Source describes how to fetch a single tool binary for the current platform.
type Source struct {
	URL     string      // download URL (HTTPS)
	SHA256  string      // expected hash of the archive; empty = unverified (warns at runtime)
	Archive ArchiveKind // how to unpack the asset
	Member  string      // path inside archive that contains the tool binary; empty for ArchiveRaw
}

// Pinned versions. Bump these together to update the toolchain in one PR.
const (
	ffmpegLinuxVersion   = "release"            // johnvansickle "release" alias = latest stable
	ffmpegMacOSBuild     = "118553-g82d2cfdaa9" // evermeet.cx build identifier
	ffmpegWindowsTag     = "latest"             // BtbN FFmpeg-Builds tag (use "latest" for symlink)
	doviToolVersion      = "2.1.3"              // github.com/quietvoid/dovi_tool tag
	platformKeyDelimiter = "/"
)

// sourceFor returns the platform-specific source for tool.
// SHA256 values are intentionally empty in v1; the Resolver prints a warning
// when verification is skipped. Pin them in a follow-up PR per platform/version.
func sourceFor(tool Tool) (Source, error) {
	key := runtime.GOOS + platformKeyDelimiter + runtime.GOARCH
	switch tool {
	case ToolFFmpeg, ToolFFprobe:
		return ffmpegSource(tool, key)
	case ToolDoviTool:
		return doviSource(key)
	}
	return Source{}, fmt.Errorf("unknown tool: %s", tool)
}

func ffmpegSource(tool Tool, key string) (Source, error) {
	// Inside-archive paths use the binary name verbatim; on Windows append .exe.
	binName := string(tool)
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}

	switch key {
	case "linux/amd64":
		return Source{
			URL:     fmt.Sprintf("https://johnvansickle.com/ffmpeg/releases/ffmpeg-%s-amd64-static.tar.xz", ffmpegLinuxVersion),
			Archive: ArchiveTarXz,
			Member:  binName, // matched as suffix; archive root prefix varies by build
		}, nil
	case "linux/arm64":
		return Source{
			URL:     fmt.Sprintf("https://johnvansickle.com/ffmpeg/releases/ffmpeg-%s-arm64-static.tar.xz", ffmpegLinuxVersion),
			Archive: ArchiveTarXz,
			Member:  binName,
		}, nil
	case "darwin/amd64", "darwin/arm64":
		// evermeet.cx ships universal binaries inside a single-file zip.
		return Source{
			URL:     fmt.Sprintf("https://evermeet.cx/ffmpeg/%s-%s.zip", binName, ffmpegMacOSBuild),
			Archive: ArchiveZip,
			Member:  binName,
		}, nil
	case "windows/amd64":
		// BtbN GPL-shared build: shared dlls; for GPL-static replace "gpl-shared" with "gpl".
		return Source{
			URL:     fmt.Sprintf("https://github.com/BtbN/FFmpeg-Builds/releases/download/%s/ffmpeg-master-latest-win64-gpl-shared.zip", ffmpegWindowsTag),
			Archive: ArchiveZip,
			Member:  binName,
		}, nil
	}
	return Source{}, fmt.Errorf("no ffmpeg source for platform %s", key)
}

func doviSource(key string) (Source, error) {
	const base = "https://github.com/quietvoid/dovi_tool/releases/download/" + doviToolVersion
	switch key {
	case "linux/amd64":
		return Source{
			URL:     fmt.Sprintf("%s/dovi_tool-%s-x86_64-unknown-linux-musl.tar.gz", base, doviToolVersion),
			Archive: ArchiveTarGz,
			Member:  "dovi_tool",
		}, nil
	case "linux/arm64":
		return Source{
			URL:     fmt.Sprintf("%s/dovi_tool-%s-aarch64-unknown-linux-musl.tar.gz", base, doviToolVersion),
			Archive: ArchiveTarGz,
			Member:  "dovi_tool",
		}, nil
	case "darwin/amd64":
		return Source{
			URL:     fmt.Sprintf("%s/dovi_tool-%s-x86_64-apple-darwin.tar.gz", base, doviToolVersion),
			Archive: ArchiveTarGz,
			Member:  "dovi_tool",
		}, nil
	case "darwin/arm64":
		return Source{
			URL:     fmt.Sprintf("%s/dovi_tool-%s-aarch64-apple-darwin.tar.gz", base, doviToolVersion),
			Archive: ArchiveTarGz,
			Member:  "dovi_tool",
		}, nil
	case "windows/amd64":
		return Source{
			URL:     fmt.Sprintf("%s/dovi_tool-%s-x86_64-pc-windows-msvc.zip", base, doviToolVersion),
			Archive: ArchiveZip,
			Member:  "dovi_tool.exe",
		}, nil
	}
	return Source{}, fmt.Errorf("no dovi_tool source for platform %s", key)
}
