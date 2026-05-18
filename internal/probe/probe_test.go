package probe

import (
	"os"
	"path/filepath"
	"testing"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestParse_DVProfile7(t *testing.T) {
	info, err := Parse("movie.mkv", loadFixture(t, "probe_dv_p7.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(info.Streams), 5; got != want {
		t.Fatalf("streams: got %d, want %d", got, want)
	}
	if got, want := info.Streams[0].DVProfile, 7; got != want {
		t.Errorf("DVProfile: got %d, want %d", got, want)
	}
	if got, want := info.Streams[1].CodecName, "truehd"; got != want {
		t.Errorf("audio[0] codec: got %s, want %s", got, want)
	}
	if got, want := info.Streams[1].Channels, 8; got != want {
		t.Errorf("audio[0] channels: got %d, want %d", got, want)
	}
	if got, want := info.Streams[1].Language, "eng"; got != want {
		t.Errorf("audio[0] language: got %s, want %s", got, want)
	}
	if got := info.Duration; got < 8400 || got > 8401 {
		t.Errorf("Duration: got %f, want ~8400", got)
	}
}

func TestParse_DVProfile8(t *testing.T) {
	info, err := Parse("movie.mkv", loadFixture(t, "probe_dv_p8.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Streams[0].DVProfile, 8; got != want {
		t.Errorf("DVProfile: got %d, want %d", got, want)
	}
}

func TestParse_NoDV(t *testing.T) {
	info, err := Parse("simple.mkv", loadFixture(t, "probe_simple.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Streams[0].DVProfile, 0; got != want {
		t.Errorf("DVProfile: got %d, want %d (no DV)", got, want)
	}
	if got, want := info.Streams[0].CodecName, "h264"; got != want {
		t.Errorf("video codec: got %s, want %s", got, want)
	}
}

func TestParse_ComplexAudio(t *testing.T) {
	info, err := Parse("complex.mkv", loadFixture(t, "probe_complex_audio.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(info.Streams), 6; got != want {
		t.Fatalf("streams: got %d, want %d", got, want)
	}
	wantCodecs := []string{"hevc", "dts", "flac", "opus", "ass", "dvd_subtitle"}
	for i, want := range wantCodecs {
		if got := info.Streams[i].CodecName; got != want {
			t.Errorf("stream[%d] codec: got %s, want %s", i, got, want)
		}
	}
}

func TestParse_InvalidJSON(t *testing.T) {
	_, err := Parse("x.mkv", []byte("{not json"))
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}
