package convert

import (
	"strings"
	"testing"

	"github.com/yashptel/mkv2mp4/internal/plan"
)

// containsArg returns true if needle appears as a contiguous subsequence of args.
func containsArg(args []string, needle ...string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(args); i++ {
		match := true
		for j, n := range needle {
			if args[i+j] != n {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestBuildArgs_SimpleRemux(t *testing.T) {
	p := &plan.ConversionPlan{
		Input:        "in.mkv",
		Output:       "out.mp4",
		VideoStream:  0,
		VideoCodec:   "h264",
		VideoAction:  plan.ActionCopy,
		AudioActions: map[int]plan.StreamAction{1: plan.ActionCopy},
		SubActions:   map[int]plan.StreamAction{},
	}
	args := BuildArgs(p)

	if !containsArg(args, "-i", "in.mkv") {
		t.Errorf("missing input flag")
	}
	if args[len(args)-1] != "out.mp4" {
		t.Errorf("output must be last arg, got %s", args[len(args)-1])
	}
	if !containsArg(args, "-map", "0:0") {
		t.Errorf("missing video map")
	}
	if !containsArg(args, "-map", "0:1") {
		t.Errorf("missing audio map")
	}
	if !containsArg(args, "-c", "copy") {
		t.Errorf("missing default copy codec")
	}
	// h264 source must NOT get hvc1 tag
	if containsArg(args, "-tag:v", "hvc1") {
		t.Errorf("h264 source should not be tagged hvc1: %v", args)
	}
}

func TestBuildArgs_HEVCGetsHvc1Tag(t *testing.T) {
	p := &plan.ConversionPlan{
		Input:        "in.mkv",
		Output:       "out.mp4",
		VideoStream:  0,
		VideoCodec:   "hevc",
		VideoAction:  plan.ActionCopy,
		AudioActions: map[int]plan.StreamAction{1: plan.ActionCopy},
		SubActions:   map[int]plan.StreamAction{},
	}
	args := BuildArgs(p)
	if !containsArg(args, "-tag:v", "hvc1") {
		t.Errorf("hevc source must be tagged hvc1: %v", args)
	}
}

func TestBuildArgs_VideoTranscode(t *testing.T) {
	p := &plan.ConversionPlan{
		Input:        "in.mkv",
		Output:       "out.mp4",
		VideoStream:  0,
		VideoCodec:   "av1",
		VideoAction:  plan.ActionTranscode,
		AudioActions: map[int]plan.StreamAction{},
		SubActions:   map[int]plan.StreamAction{},
	}
	args := BuildArgs(p)
	if !containsArg(args, "-c:v", "libx265") {
		t.Errorf("expected libx265 codec for transcode: %v", args)
	}
	if !containsArg(args, "-crf", "22") {
		t.Errorf("expected CRF 22: %v", args)
	}
	if !containsArg(args, "-tag:v", "hvc1") {
		t.Errorf("transcode must produce HEVC tagged hvc1: %v", args)
	}
}

func TestBuildArgs_AudioTranscodeIndexing(t *testing.T) {
	// First audio is incompatible (truehd), second is compatible (ac3).
	// Output positions: truehd -> 0 (transcode), ac3 -> 1 (copy).
	p := &plan.ConversionPlan{
		Input:       "in.mkv",
		Output:      "out.mp4",
		VideoStream: 0,
		VideoCodec:  "hevc",
		VideoAction: plan.ActionCopy,
		AudioActions: map[int]plan.StreamAction{
			1: plan.ActionTranscode, // truehd
			2: plan.ActionCopy,      // ac3
		},
		SubActions: map[int]plan.StreamAction{},
	}
	args := BuildArgs(p)

	if !containsArg(args, "-c:a:0", "eac3") {
		t.Errorf("output position 0 (truehd) must be transcoded to eac3: %v", args)
	}
	if !containsArg(args, "-b:a:0", "640k") {
		t.Errorf("output position 0 must be 640k: %v", args)
	}
	// Output position 1 (ac3) should NOT have a -c:a:1 override; default -c copy handles it.
	if containsArg(args, "-c:a:1", "eac3") {
		t.Errorf("ac3 should be copied, not transcoded: %v", args)
	}
}

func TestBuildArgs_DropPGSSubs(t *testing.T) {
	p := &plan.ConversionPlan{
		Input:       "in.mkv",
		Output:      "out.mp4",
		VideoStream: 0,
		VideoCodec:  "hevc",
		VideoAction: plan.ActionCopy,
		AudioActions: map[int]plan.StreamAction{1: plan.ActionCopy},
		SubActions: map[int]plan.StreamAction{
			2: plan.ActionDrop,      // PGS
			3: plan.ActionTranscode, // SRT -> mov_text
		},
	}
	args := BuildArgs(p)
	// PGS index 2 must NOT be mapped.
	if containsArg(args, "-map", "0:2") {
		t.Errorf("PGS stream 2 should be dropped, not mapped: %v", args)
	}
	if !containsArg(args, "-map", "0:3") {
		t.Errorf("SRT stream 3 should be mapped: %v", args)
	}
	if !containsArg(args, "-c:s", "mov_text") {
		t.Errorf("subs must be transcoded to mov_text: %v", args)
	}
}

func TestBuildArgs_SidecarSubs(t *testing.T) {
	p := &plan.ConversionPlan{
		Input:        "in.mkv",
		Output:       "out.mp4",
		VideoStream:  0,
		VideoCodec:   "hevc",
		VideoAction:  plan.ActionCopy,
		AudioActions: map[int]plan.StreamAction{1: plan.ActionCopy},
		SubActions:   map[int]plan.StreamAction{},
		SidecarSubs: []plan.SidecarSub{
			{Path: "english.srt", Language: "eng"},
			{Path: "spanish.srt", Language: "spa"},
		},
	}
	args := BuildArgs(p)

	if !containsArg(args, "-i", "english.srt") {
		t.Errorf("missing sidecar input: %v", args)
	}
	if !containsArg(args, "-i", "spanish.srt") {
		t.Errorf("missing sidecar input: %v", args)
	}
	if !containsArg(args, "-map", "1:0") {
		t.Errorf("missing sidecar 1 map: %v", args)
	}
	if !containsArg(args, "-map", "2:0") {
		t.Errorf("missing sidecar 2 map: %v", args)
	}
	if !containsArg(args, "-c:s", "mov_text") {
		t.Errorf("sidecars must be transcoded to mov_text: %v", args)
	}
	if !containsArg(args, "-metadata:s:s:0", "language=eng") {
		t.Errorf("missing eng language tag: %v", args)
	}
	if !containsArg(args, "-metadata:s:s:1", "language=spa") {
		t.Errorf("missing spa language tag: %v", args)
	}
}

func TestBuildArgs_AudioOnlyNoMapVideo(t *testing.T) {
	p := &plan.ConversionPlan{
		Input:        "audio.mkv",
		Output:       "audio.mp4",
		VideoStream:  -1,
		VideoAction:  plan.ActionCopy,
		AudioActions: map[int]plan.StreamAction{0: plan.ActionTranscode},
		SubActions:   map[int]plan.StreamAction{},
	}
	args := BuildArgs(p)
	// no video stream -> no video mapping
	if containsArg(args, "-tag:v", "hvc1") {
		t.Errorf("no video stream -> no hvc1 tag: %v", args)
	}
	if !containsArg(args, "-map", "0:0") {
		t.Errorf("audio must still be mapped: %v", args)
	}
}

func TestBuildArgs_StrictUnofficialAlwaysSet(t *testing.T) {
	p := &plan.ConversionPlan{
		Input:        "in.mkv",
		Output:       "out.mp4",
		VideoStream:  0,
		VideoCodec:   "h264",
		VideoAction:  plan.ActionCopy,
		AudioActions: map[int]plan.StreamAction{1: plan.ActionCopy},
		SubActions:   map[int]plan.StreamAction{},
	}
	args := BuildArgs(p)
	if !containsArg(args, "-strict", "unofficial") {
		t.Errorf("expected -strict unofficial: %v", strings.Join(args, " "))
	}
}

func TestBuildArgs_InputOrderPreserved(t *testing.T) {
	// Audio at stream 5 must produce a -map 0:5 even though it's "later" than sub at 3.
	p := &plan.ConversionPlan{
		Input:       "in.mkv",
		Output:      "out.mp4",
		VideoStream: 0,
		VideoCodec:  "hevc",
		VideoAction: plan.ActionCopy,
		AudioActions: map[int]plan.StreamAction{
			5: plan.ActionTranscode,
			1: plan.ActionCopy,
		},
		SubActions: map[int]plan.StreamAction{3: plan.ActionTranscode},
	}
	args := BuildArgs(p)

	// audio output positions: stream 1 -> output 0 (copy), stream 5 -> output 1 (transcode)
	if !containsArg(args, "-c:a:1", "eac3") {
		t.Errorf("stream 5 (output pos 1) must be transcoded: %v", args)
	}
	if containsArg(args, "-c:a:0", "eac3") {
		t.Errorf("stream 1 (output pos 0, ac3-equivalent copy) should not have eac3 override: %v", args)
	}
}
