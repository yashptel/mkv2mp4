// Package convert turns a ConversionPlan into a concrete ffmpeg invocation.
//
// BuildArgs handles the single-pass remux/transcode case used when no Dolby
// Vision profile conversion is required. For DV Profile 7 -> 8.1 sources, see
// dovi.go which orchestrates a multi-step pipeline.
package convert

import (
	"fmt"
	"sort"

	"github.com/yashptel/mkv2mp4/internal/plan"
)

const (
	// EAC3 640k preserves Atmos when input is TrueHD-Atmos (via JOC) and is the
	// safest target for FLAC / DTS-HD / DTS / Opus when targeting LG OLED.
	AudioCodec   = "eac3"
	AudioBitrate = "640k"

	// libx265 CRF 22 / preset slow when the user has confirmed a video
	// transcode (e.g. AV1 -> HEVC) via --yes.
	VideoCodec  = "libx265"
	VideoCRF    = "22"
	VideoPreset = "slow"

	// hvc1 (not hev1) is required for Apple/LG MP4 playback of HEVC.
	HEVCTag = "hvc1"
)

// audioOut tracks one kept audio stream's output position (used to emit the
// right `-c:a:N` override).
type audioOut struct {
	outputPos int
	action    plan.StreamAction
}

// BuildArgs returns the argument list (without the binary name) for the ffmpeg
// invocation that produces p.Output from p.Input.
//
// Caller responsibilities (NOT handled here):
//   - check p.NeedsDVConvert and route to the DV pipeline instead
//   - confirm interactive transcode for incompatible video before calling
//   - ensure p.Input is reachable and p.Output's parent dir exists
func BuildArgs(p *plan.ConversionPlan) []string {
	args := []string{
		"-hide_banner",
		"-y",
		"-nostdin",
	}

	args = append(args, "-i", p.Input)
	for _, s := range p.SidecarSubs {
		args = append(args, "-i", s.Path)
	}

	hasVideo := p.VideoStream >= 0 && p.VideoAction != plan.ActionDrop
	if hasVideo {
		args = append(args, "-map", fmt.Sprintf("0:%d", p.VideoStream))
	}

	audios, args := mapAudio(args, p, 0)
	embeddedSubsOut, args := mapEmbeddedSubs(args, p, 0)

	// Sidecar subs: one extra input per file, each with a single stream.
	for i := range p.SidecarSubs {
		args = append(args, "-map", fmt.Sprintf("%d:0", i+1))
	}

	args = append(args, "-c", "copy")

	if hasVideo {
		args = appendVideoCodec(args, p)
	}
	args = appendCodecOverrides(args, audios, embeddedSubsOut, p.SidecarSubs)

	args = append(args, p.Output)
	return args
}

// mapAudio adds -map flags for kept audio streams from inputIdx and returns
// the list of output positions for later -c:a:N overrides.
func mapAudio(args []string, p *plan.ConversionPlan, inputIdx int) ([]audioOut, []string) {
	audios := make([]audioOut, 0, len(p.AudioActions))
	for _, idx := range sortedKeys(p.AudioActions) {
		action := p.AudioActions[idx]
		if action == plan.ActionDrop {
			continue
		}
		args = append(args, "-map", fmt.Sprintf("%d:%d", inputIdx, idx))
		audios = append(audios, audioOut{len(audios), action})
	}
	return audios, args
}

// mapEmbeddedSubs adds -map flags for kept subtitle streams from inputIdx and
// returns the number of output sub positions consumed (for sidecar metadata
// numbering).
func mapEmbeddedSubs(args []string, p *plan.ConversionPlan, inputIdx int) (int, []string) {
	out := 0
	for _, idx := range sortedKeys(p.SubActions) {
		if p.SubActions[idx] == plan.ActionDrop {
			continue
		}
		args = append(args, "-map", fmt.Sprintf("%d:%d", inputIdx, idx))
		out++
	}
	return out, args
}

func appendVideoCodec(args []string, p *plan.ConversionPlan) []string {
	if p.VideoAction == plan.ActionTranscode {
		return append(args,
			"-c:v", VideoCodec,
			"-crf", VideoCRF,
			"-preset", VideoPreset,
			"-tag:v", HEVCTag,
		)
	}
	if p.VideoCodec == "hevc" {
		return append(args, "-tag:v", HEVCTag)
	}
	return args
}

func appendCodecOverrides(args []string, audios []audioOut, embeddedSubsOut int, sidecars []plan.SidecarSub) []string {
	for _, a := range audios {
		if a.action == plan.ActionTranscode {
			args = append(args,
				fmt.Sprintf("-c:a:%d", a.outputPos), AudioCodec,
				fmt.Sprintf("-b:a:%d", a.outputPos), AudioBitrate,
			)
		}
	}
	if embeddedSubsOut > 0 || len(sidecars) > 0 {
		args = append(args, "-c:s", "mov_text")
	}
	for i, s := range sidecars {
		if s.Language == "" {
			continue
		}
		outPos := embeddedSubsOut + i
		args = append(args, fmt.Sprintf("-metadata:s:s:%d", outPos), "language="+s.Language)
	}
	args = append(args, "-strict", "unofficial")
	return args
}

func sortedKeys(m map[int]plan.StreamAction) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
