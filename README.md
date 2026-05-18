# mkv2mp4

A cross-platform CLI that converts MKV files to LG-OLED-friendly MP4 so Dolby Vision plays from a USB stick or local network share.

LG OLEDs only carry Dolby Vision in the MP4 container, and almost all torrent rips ship as MKV. This tool does what you actually need to make the file play correctly:

- **Remux** video and compatible audio without re-encoding (lossless).
- **Drop** PGS / DVD bitmap subtitles that MP4 can't carry.
- **Transcode** TrueHD / DTS-HD MA / FLAC / DTS / Opus audio to EAC3 640k so the TV's decoder is happy (Atmos preserved via EAC3-JOC where the source supports it).
- **Convert** Dolby Vision Profile 7 (the dual-layer flavour most BD-rips use) to Profile 8.1 (the single-layer flavour LG OLEDs accept in MP4).
- **Auto-fetch** `ffmpeg`, `ffprobe`, and `dovi_tool` on first run if you don't already have them on `$PATH`. Zero manual setup.

## Install

### macOS and Linux

```sh
curl -fsSL https://raw.githubusercontent.com/yashptel/mkv2mp4/main/install.sh | sh
```

Installs to `/usr/local/bin/mkv2mp4`. Set `MKV2MP4_PREFIX=~/bin` to put it elsewhere, or `MKV2MP4_VERSION=vX.Y.Z` to pin a release.

### Windows

Grab the latest `mkv2mp4_*_windows_amd64.zip` from the [Releases](https://github.com/yashptel/mkv2mp4/releases) page, extract `mkv2mp4.exe`, and add the folder to your `PATH`.

### From source

```sh
go install github.com/yashptel/mkv2mp4/cmd/mkv2mp4@latest
```

## Usage

```
mkv2mp4 movie.mkv                       # single file
mkv2mp4 *.mkv                           # shell-glob
mkv2mp4 ./Season1/                      # all .mkv directly in the folder
mkv2mp4 -r ./Show/                      # recurse into subfolders
mkv2mp4 movie.mkv --srt en.srt:eng      # add a sidecar subtitle
mkv2mp4 -f movie.mkv                    # overwrite an existing .mp4
mkv2mp4 -o ./out/ movie.mkv             # write output to a different directory
mkv2mp4 --delete-source movie.mkv       # delete the .mkv on success
mkv2mp4 --yes ./batch/                  # batch mode: auto-confirm transcode prompts
mkv2mp4 --update-deps                   # refetch ffmpeg / dovi_tool
```

### Flag reference

| Flag | Description |
|---|---|
| `-r`, `--recursive` | Recurse into subdirectories |
| `-o`, `--output DIR` | Output directory (default: alongside the source) |
| `-f`, `--force` | Overwrite existing `.mp4` |
| `--delete-source` | Delete the source `.mkv` after a successful convert |
| `--srt PATH[:LANG]` | Add a sidecar subtitle (repeatable, e.g. `--srt en.srt:eng`) |
| `-v`, `--verbose` | Show full ffmpeg log |
| `-q`, `--quiet` | Errors only |
| `--yes` | Auto-confirm interactive prompts (e.g. video transcode) |
| `--update-deps` | Force redownload of ffmpeg / ffprobe / dovi_tool |
| `--bin-dir DIR` | Override binary cache location (default: `os.UserCacheDir()/mkv2mp4/bin`) |
| `--version` | Print version |
| `-h`, `--help` | Help |

## What gets copied vs. transcoded

| Stream type | Source codec | Action | Notes |
|---|---|---|---|
| Video | h264, hevc | copy | HEVC tagged `hvc1` for LG / Apple compatibility |
| Video | av1, vp9, mpeg2, ... | **prompt** | Interactive `[y/N]`; `--yes` auto-confirms. Re-encodes to HEVC (CRF 22, preset slow). Takes hours. |
| Audio | aac, ac3, eac3, mp3 | copy | LG OLED decodes these natively |
| Audio | truehd, dts(-HD MA), flac, opus, vorbis, ... | transcode | EAC3 640k, channel layout preserved; Atmos preserved via JOC for TrueHD-Atmos sources |
| Subtitle | srt, ass, ssa, webvtt, mov_text | transcode | All become `mov_text` (Note: ASS styling is stripped) |
| Subtitle | hdmv_pgs_subtitle, dvd_subtitle, vobsub | **drop** | Bitmap subs can't ride in MP4 |
| Dolby Vision | Profile 5 / 8.x | copy | Already MP4-compatible |
| Dolby Vision | Profile 7 | `dovi_tool` | Converted to Profile 8.1 (HDR10 fallback retained) |

## Troubleshooting

**LG isn't showing the Dolby Vision badge.**
- The video stream needs to be HEVC + a DV-compatible profile (5 or 8.x). Run `mkv2mp4 -v` and check the ffprobe output. Profile 7 sources are auto-converted by this tool to 8.1.
- The TV needs an MP4 container. Files renamed from `.mkv` to `.mp4` won't work.
- DV doesn't fall back gracefully — if the RPU is malformed, you'll see HDR10 with no DV badge. Try `mkv2mp4 --update-deps` to grab the latest `dovi_tool`.

**Audio is silent / can't be selected.**
- LG OLEDs in 2026 still don't decode FLAC, DTS-HD MA, TrueHD, or Opus in MP4. mkv2mp4 should transcode those to EAC3 by default. Confirm the output with `ffprobe out.mp4` — every audio track should be `aac`, `ac3`, or `eac3`.

**`ffmpeg not found` on Windows even after install.**
- First runs auto-download to `%LocalAppData%\mkv2mp4\bin`. If the download is blocked by antivirus or a corporate proxy, install ffmpeg manually (`winget install Gyan.FFmpeg`) and mkv2mp4 will pick it up via `PATH`.

**"sha256 mismatch" during auto-download.**
- The pinned hashes in `internal/deps/sources.go` are intentionally empty in v1; downloads are unverified by default with a warning. If you see this error, an explicit hash was added and the upstream artifact changed.

**Conversion is stuck at 0%.**
- The first ffmpeg call always seeks the input, which can take a while on slow disks. The progress bar only starts moving once output begins.
- For DV Profile 7 sources, the actual mux step is preceded by an HEVC extract step (no progress bar there, fast) and a `dovi_tool convert` step (very fast, no bar). Be patient.

## How the conversion actually works

For a non-DV-Profile-7 source, `mkv2mp4` runs a single ffmpeg invocation, roughly:

```
ffmpeg -hide_banner -y -nostdin -i in.mkv \
  -map 0:0 -map 0:1 -map 0:3 \
  -c copy -tag:v hvc1 \
  -c:a:0 eac3 -b:a:0 640k \
  -c:s mov_text \
  -strict unofficial \
  out.mp4
```

For a Dolby Vision Profile 7 source, it runs a three-step pipeline:

```
# 1) extract Annex-B HEVC bitstream
ffmpeg -i in.mkv -map 0:v:0 -c copy -bsf:v hevc_mp4toannexb -f hevc tmp.hevc

# 2) rewrite RPU from P7 to P8.1 (single-layer + HDR10 fallback)
dovi_tool -m 2 convert --discard -o tmp_p81.hevc tmp.hevc

# 3) mux converted video with original audio/subs
ffmpeg -i tmp_p81.hevc -i in.mkv \
  -map 0:v:0 -map 1:a -map 1:s \
  -c copy -tag:v hvc1 \
  -c:a:0 eac3 -b:a:0 640k \
  -c:s mov_text \
  -strict unofficial \
  out.mp4
```

The exact arguments are in [`internal/convert/convert.go`](internal/convert/convert.go) and [`internal/convert/dovi.go`](internal/convert/dovi.go).

## Origin

This is a from-scratch Go rewrite of a PowerShell one-liner that was Windows-only. The original lives at [`docs/legacy/Microsoft.PowerShell_profile.ps1`](docs/legacy/Microsoft.PowerShell_profile.ps1) for reference.

## License

MIT — see [LICENSE](LICENSE).
