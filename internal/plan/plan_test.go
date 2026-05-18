package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yashptel/mkv2mp4/internal/probe"
)

func loadFile(t *testing.T, name string) *probe.FileInfo {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	info, err := probe.Parse(name, data)
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return info
}

func TestBuild_DVProfile7(t *testing.T) {
	info := loadFile(t, "probe_dv_p7.json")
	p := Build(info, nil)

	if !p.NeedsDVConvert {
		t.Error("expected NeedsDVConvert for Profile 7 input")
	}
	if p.VideoAction != ActionCopy {
		t.Errorf("VideoAction: got %s, want copy", p.VideoAction)
	}
	if got, want := p.VideoStream, 0; got != want {
		t.Errorf("VideoStream: got %d, want %d", got, want)
	}
	// stream 1 = truehd (transcode), 2 = ac3 (copy)
	if p.AudioActions[1] != ActionTranscode {
		t.Errorf("audio[1] truehd: got %s, want transcode", p.AudioActions[1])
	}
	if p.AudioActions[2] != ActionCopy {
		t.Errorf("audio[2] ac3: got %s, want copy", p.AudioActions[2])
	}
	// stream 3 = pgs (drop), 4 = srt (transcode)
	if p.SubActions[3] != ActionDrop {
		t.Errorf("sub[3] pgs: got %s, want drop", p.SubActions[3])
	}
	if p.SubActions[4] != ActionTranscode {
		t.Errorf("sub[4] srt: got %s, want transcode", p.SubActions[4])
	}
}

func TestBuild_DVProfile8_NoConvert(t *testing.T) {
	info := loadFile(t, "probe_dv_p8.json")
	p := Build(info, nil)
	if p.NeedsDVConvert {
		t.Error("expected NeedsDVConvert=false for Profile 8 input")
	}
	if p.VideoAction != ActionCopy {
		t.Errorf("VideoAction: got %s, want copy", p.VideoAction)
	}
	if p.AudioActions[1] != ActionCopy {
		t.Errorf("audio[1] eac3: got %s, want copy", p.AudioActions[1])
	}
}

func TestBuild_Simple(t *testing.T) {
	info := loadFile(t, "probe_simple.json")
	p := Build(info, nil)
	if p.NeedsDVConvert {
		t.Error("expected NeedsDVConvert=false")
	}
	if p.VideoAction != ActionCopy {
		t.Errorf("VideoAction: got %s, want copy", p.VideoAction)
	}
	if p.AudioActions[1] != ActionCopy {
		t.Errorf("audio aac: got %s, want copy", p.AudioActions[1])
	}
}

func TestBuild_ComplexAudio(t *testing.T) {
	info := loadFile(t, "probe_complex_audio.json")
	p := Build(info, nil)

	// stream 1 = dts, 2 = flac, 3 = opus → all transcode
	for _, idx := range []int{1, 2, 3} {
		if p.AudioActions[idx] != ActionTranscode {
			t.Errorf("audio[%d]: got %s, want transcode", idx, p.AudioActions[idx])
		}
	}
	// stream 4 = ass (transcode), 5 = dvd_subtitle (drop)
	if p.SubActions[4] != ActionTranscode {
		t.Errorf("sub[4] ass: got %s, want transcode", p.SubActions[4])
	}
	if p.SubActions[5] != ActionDrop {
		t.Errorf("sub[5] dvd: got %s, want drop", p.SubActions[5])
	}
}

func TestBuild_IncompatibleVideo(t *testing.T) {
	info := &probe.FileInfo{
		Path:    "av1.mkv",
		Streams: []probe.StreamInfo{{Index: 0, CodecType: "video", CodecName: "av1"}},
	}
	p := Build(info, nil)
	if !p.HasIncompatibleVideo() {
		t.Error("expected HasIncompatibleVideo=true for AV1")
	}
	if p.VideoAction != ActionTranscode {
		t.Errorf("VideoAction: got %s, want transcode", p.VideoAction)
	}
}

func TestBuild_SidecarSubsPassThrough(t *testing.T) {
	info := loadFile(t, "probe_simple.json")
	subs := []SidecarSub{
		{Path: "english.srt", Language: "eng"},
		{Path: "spanish.srt", Language: "spa"},
	}
	p := Build(info, subs)
	if len(p.SidecarSubs) != 2 {
		t.Fatalf("SidecarSubs len: got %d, want 2", len(p.SidecarSubs))
	}
	if p.SidecarSubs[0].Path != "english.srt" || p.SidecarSubs[0].Language != "eng" {
		t.Errorf("SidecarSubs[0]: %+v", p.SidecarSubs[0])
	}
}

func TestBuild_NoVideo(t *testing.T) {
	info := &probe.FileInfo{
		Path: "audio_only.mkv",
		Streams: []probe.StreamInfo{
			{Index: 0, CodecType: "audio", CodecName: "flac"},
		},
	}
	p := Build(info, nil)
	if p.VideoStream != -1 {
		t.Errorf("VideoStream: got %d, want -1 (no video)", p.VideoStream)
	}
	if p.HasIncompatibleVideo() {
		t.Error("HasIncompatibleVideo should be false when there's no video stream")
	}
}

// Ensure the plan struct is JSON-encodable for `--dry-run` or logging.
func TestPlanIsJSONEncodable(t *testing.T) {
	info := loadFile(t, "probe_dv_p7.json")
	p := Build(info, nil)
	if _, err := json.Marshal(p); err != nil {
		t.Errorf("json.Marshal: %v", err)
	}
}
