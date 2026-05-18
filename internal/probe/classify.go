package probe

// CompatibilityKind describes how a stream should be handled when producing an
// MP4 destined for an LG OLED.
type CompatibilityKind int

const (
	CompatCopy      CompatibilityKind = iota // Codec is MP4/LG-compatible; remux as-is.
	CompatTranscode                          // Codec is incompatible but can be converted.
	CompatDrop                               // Codec cannot ride in MP4; the stream must be dropped.
	CompatBlock                              // Codec requires expensive transcoding; require explicit user OK.
)

// Compatibility returns the recommended action for a stream.
func Compatibility(s StreamInfo) CompatibilityKind {
	switch s.CodecType {
	case "video":
		return videoCompat(s.CodecName)
	case "audio":
		return audioCompat(s.CodecName)
	case "subtitle":
		return subtitleCompat(s.CodecName)
	}
	return CompatDrop
}

func videoCompat(codec string) CompatibilityKind {
	switch codec {
	case "h264", "hevc":
		return CompatCopy
	default:
		return CompatBlock
	}
}

func audioCompat(codec string) CompatibilityKind {
	switch codec {
	case "aac", "ac3", "eac3", "mp3":
		return CompatCopy
	default:
		return CompatTranscode
	}
}

func subtitleCompat(codec string) CompatibilityKind {
	switch codec {
	case "subrip", "srt", "ass", "ssa", "mov_text", "webvtt":
		return CompatTranscode
	default:
		return CompatDrop
	}
}
