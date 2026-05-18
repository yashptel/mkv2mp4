package convert

import (
	"testing"

	"github.com/yashptel/mkv2mp4/internal/plan"
)

func TestBuildDVExtractArgs(t *testing.T) {
	args := BuildDVExtractArgs("movie.mkv", "/tmp/x.hevc")
	if !containsArg(args, "-i", "movie.mkv") {
		t.Errorf("missing input: %v", args)
	}
	if !containsArg(args, "-map", "0:v:0") {
		t.Errorf("missing video map: %v", args)
	}
	if !containsArg(args, "-bsf:v", "hevc_mp4toannexb") {
		t.Errorf("missing annexb bsf: %v", args)
	}
	if !containsArg(args, "-f", "hevc") {
		t.Errorf("missing hevc format: %v", args)
	}
	if args[len(args)-1] != "/tmp/x.hevc" {
		t.Errorf("output should be last: %v", args)
	}
}

func TestBuildDoviConvertArgs(t *testing.T) {
	args := BuildDoviConvertArgs("/tmp/in.hevc", "/tmp/out.hevc")
	if !containsArg(args, "-m", "2") {
		t.Errorf("missing mode 2: %v", args)
	}
	if !containsArg(args, "convert") {
		t.Errorf("missing convert subcommand: %v", args)
	}
	if !containsArg(args, "--discard") {
		t.Errorf("missing --discard: %v", args)
	}
	if !containsArg(args, "-o", "/tmp/out.hevc") {
		t.Errorf("missing -o output: %v", args)
	}
}

func TestBuildDVMuxArgs_StreamSourcing(t *testing.T) {
	// Video must come from input 0 (converted HEVC). Audio/subs must come
	// from input 1 (original MKV).
	p := &plan.ConversionPlan{
		Input:       "original.mkv",
		Output:      "out.mp4",
		VideoStream: 0,
		VideoCodec:  "hevc",
		VideoAction: plan.ActionCopy,
		AudioActions: map[int]plan.StreamAction{
			1: plan.ActionTranscode,
			2: plan.ActionCopy,
		},
		SubActions: map[int]plan.StreamAction{
			3: plan.ActionTranscode,
			4: plan.ActionDrop,
		},
	}
	args := BuildDVMuxArgs(p, "/tmp/p81.hevc")

	if !containsArg(args, "-i", "/tmp/p81.hevc") {
		t.Errorf("missing HEVC input: %v", args)
	}
	if !containsArg(args, "-i", "original.mkv") {
		t.Errorf("missing original MKV input: %v", args)
	}
	if !containsArg(args, "-map", "0:v:0") {
		t.Errorf("video must come from input 0 (HEVC): %v", args)
	}
	if !containsArg(args, "-map", "1:1") {
		t.Errorf("audio[1] must come from input 1: %v", args)
	}
	if !containsArg(args, "-map", "1:2") {
		t.Errorf("audio[2] must come from input 1: %v", args)
	}
	if !containsArg(args, "-map", "1:3") {
		t.Errorf("sub[3] must come from input 1: %v", args)
	}
	if containsArg(args, "-map", "1:4") {
		t.Errorf("sub[4] (drop) should not be mapped: %v", args)
	}
	if !containsArg(args, "-tag:v", "hvc1") {
		t.Errorf("DV mux always needs hvc1 tag: %v", args)
	}
	if !containsArg(args, "-c:a:0", "eac3") {
		t.Errorf("first kept audio (transcoded) should be eac3: %v", args)
	}
	if !containsArg(args, "-c:s", "mov_text") {
		t.Errorf("kept subs should be mov_text: %v", args)
	}
	if args[len(args)-1] != "out.mp4" {
		t.Errorf("output should be last: %v", args)
	}
}

func TestBuildDVMuxArgs_SidecarSubs(t *testing.T) {
	p := &plan.ConversionPlan{
		Input:       "original.mkv",
		Output:      "out.mp4",
		VideoStream: 0,
		VideoCodec:  "hevc",
		VideoAction: plan.ActionCopy,
		AudioActions: map[int]plan.StreamAction{1: plan.ActionCopy},
		SubActions:   map[int]plan.StreamAction{},
		SidecarSubs: []plan.SidecarSub{
			{Path: "english.srt", Language: "eng"},
			{Path: "spanish.srt", Language: "spa"},
		},
	}
	args := BuildDVMuxArgs(p, "/tmp/p81.hevc")
	// Inputs: 0=hevc, 1=mkv, 2=english.srt, 3=spanish.srt
	if !containsArg(args, "-map", "2:0") {
		t.Errorf("first sidecar should be input 2: %v", args)
	}
	if !containsArg(args, "-map", "3:0") {
		t.Errorf("second sidecar should be input 3: %v", args)
	}
	if !containsArg(args, "-metadata:s:s:0", "language=eng") {
		t.Errorf("missing eng metadata: %v", args)
	}
}
