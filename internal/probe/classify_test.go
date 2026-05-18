package probe

import "testing"

func TestCompatibility(t *testing.T) {
	cases := []struct {
		name   string
		stream StreamInfo
		want   CompatibilityKind
	}{
		// video
		{"h264 video", StreamInfo{CodecType: "video", CodecName: "h264"}, CompatCopy},
		{"hevc video", StreamInfo{CodecType: "video", CodecName: "hevc"}, CompatCopy},
		{"av1 video", StreamInfo{CodecType: "video", CodecName: "av1"}, CompatBlock},
		{"vp9 video", StreamInfo{CodecType: "video", CodecName: "vp9"}, CompatBlock},
		{"mpeg2 video", StreamInfo{CodecType: "video", CodecName: "mpeg2video"}, CompatBlock},

		// audio
		{"aac audio", StreamInfo{CodecType: "audio", CodecName: "aac"}, CompatCopy},
		{"ac3 audio", StreamInfo{CodecType: "audio", CodecName: "ac3"}, CompatCopy},
		{"eac3 audio", StreamInfo{CodecType: "audio", CodecName: "eac3"}, CompatCopy},
		{"mp3 audio", StreamInfo{CodecType: "audio", CodecName: "mp3"}, CompatCopy},
		{"truehd audio", StreamInfo{CodecType: "audio", CodecName: "truehd"}, CompatTranscode},
		{"dts audio", StreamInfo{CodecType: "audio", CodecName: "dts"}, CompatTranscode},
		{"flac audio", StreamInfo{CodecType: "audio", CodecName: "flac"}, CompatTranscode},
		{"opus audio", StreamInfo{CodecType: "audio", CodecName: "opus"}, CompatTranscode},
		{"vorbis audio", StreamInfo{CodecType: "audio", CodecName: "vorbis"}, CompatTranscode},

		// subtitles
		{"srt subs", StreamInfo{CodecType: "subtitle", CodecName: "subrip"}, CompatTranscode},
		{"ass subs", StreamInfo{CodecType: "subtitle", CodecName: "ass"}, CompatTranscode},
		{"ssa subs", StreamInfo{CodecType: "subtitle", CodecName: "ssa"}, CompatTranscode},
		{"mov_text subs", StreamInfo{CodecType: "subtitle", CodecName: "mov_text"}, CompatTranscode},
		{"pgs subs", StreamInfo{CodecType: "subtitle", CodecName: "hdmv_pgs_subtitle"}, CompatDrop},
		{"dvd subs", StreamInfo{CodecType: "subtitle", CodecName: "dvd_subtitle"}, CompatDrop},
		{"vobsub subs", StreamInfo{CodecType: "subtitle", CodecName: "dvb_subtitle"}, CompatDrop},

		// unknown stream type
		{"unknown attachment", StreamInfo{CodecType: "attachment", CodecName: "ttf"}, CompatDrop},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Compatibility(c.stream); got != c.want {
				t.Errorf("Compatibility(%+v) = %d, want %d", c.stream, got, c.want)
			}
		})
	}
}
