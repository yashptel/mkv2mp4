// Package plan turns a probed media file into a ConversionPlan that tells the
// convert package exactly what to copy, transcode, or drop.
package plan

import (
	"github.com/yashptel/mkv2mp4/internal/probe"
)

type StreamAction int

const (
	ActionCopy StreamAction = iota
	ActionTranscode
	ActionDrop
)

func (a StreamAction) String() string {
	switch a {
	case ActionCopy:
		return "copy"
	case ActionTranscode:
		return "transcode"
	case ActionDrop:
		return "drop"
	}
	return "unknown"
}

type SidecarSub struct {
	Path     string
	Language string
}

type ConversionPlan struct {
	Input          string
	Output         string
	VideoStream    int
	VideoCodec     string
	VideoAction    StreamAction
	AudioActions   map[int]StreamAction
	SubActions     map[int]StreamAction
	NeedsDVConvert bool
	SidecarSubs    []SidecarSub
	Duration       float64
}

// Build produces a ConversionPlan from probed file info and any sidecar subs
// supplied on the CLI. It is a pure function: no I/O, no side effects.
func Build(info *probe.FileInfo, sidecarSubs []SidecarSub) *ConversionPlan {
	p := &ConversionPlan{
		Input:        info.Path,
		AudioActions: make(map[int]StreamAction),
		SubActions:   make(map[int]StreamAction),
		SidecarSubs:  sidecarSubs,
		Duration:     info.Duration,
		VideoStream:  -1,
	}
	for _, s := range info.Streams {
		switch s.CodecType {
		case "video":
			if p.VideoStream == -1 {
				p.VideoStream = s.Index
				p.VideoCodec = s.CodecName
				p.VideoAction = mapVideo(probe.Compatibility(s))
				if s.DVProfile == 7 {
					p.NeedsDVConvert = true
				}
			}
		case "audio":
			p.AudioActions[s.Index] = mapAudio(probe.Compatibility(s))
		case "subtitle":
			p.SubActions[s.Index] = mapSubtitle(probe.Compatibility(s))
		}
	}
	return p
}

// HasIncompatibleVideo reports whether the plan requires re-encoding the video
// stream — the CLI uses this to gate the interactive confirmation prompt.
func (p *ConversionPlan) HasIncompatibleVideo() bool {
	return p.VideoAction == ActionTranscode
}

func mapVideo(c probe.CompatibilityKind) StreamAction {
	if c == probe.CompatCopy {
		return ActionCopy
	}
	return ActionTranscode
}

func mapAudio(c probe.CompatibilityKind) StreamAction {
	if c == probe.CompatCopy {
		return ActionCopy
	}
	return ActionTranscode
}

func mapSubtitle(c probe.CompatibilityKind) StreamAction {
	if c == probe.CompatTranscode {
		return ActionTranscode
	}
	return ActionDrop
}
