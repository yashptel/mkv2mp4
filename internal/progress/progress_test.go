package progress

import (
	"io"
	"strings"
	"testing"
)

// sampleStream mimics what `ffmpeg -progress pipe:1` emits — two intermediate
// frames ending in progress=continue, then a final frame with progress=end.
const sampleStream = `frame=120
fps=24.50
stream_0_0_q=-1.0
bitrate=2000.0kbits/s
total_size=10240
out_time_us=4000000
out_time_ms=4000000
out_time=00:00:04.000000
dup_frames=0
drop_frames=0
speed=1.0x
progress=continue
frame=240
fps=24.10
stream_0_0_q=-1.0
bitrate=2050.0kbits/s
total_size=20480
out_time_us=8000000
out_time_ms=8000000
out_time=00:00:08.000000
dup_frames=0
drop_frames=0
speed=1.05x
progress=continue
frame=480
fps=23.90
stream_0_0_q=-1.0
bitrate=2030.0kbits/s
total_size=40960
out_time_us=16000000
out_time_ms=16000000
out_time=00:00:16.000000
dup_frames=0
drop_frames=0
speed=1.10x
progress=end
`

func TestParse_CallsCallbackPerBlock(t *testing.T) {
	var updates []Update
	err := Parse(strings.NewReader(sampleStream), func(u Update) {
		updates = append(updates, u)
	})
	if err != nil && err != io.EOF {
		t.Fatalf("Parse: %v", err)
	}
	if got, want := len(updates), 3; got != want {
		t.Fatalf("blocks: got %d, want %d", got, want)
	}
	if got, want := updates[0].OutTimeUs, int64(4000000); got != want {
		t.Errorf("block 0 OutTimeUs: got %d, want %d", got, want)
	}
	if got, want := updates[2].OutTimeUs, int64(16000000); got != want {
		t.Errorf("block 2 OutTimeUs: got %d, want %d", got, want)
	}
	if !updates[2].Done {
		t.Errorf("last block should be Done=true")
	}
	if updates[0].Done {
		t.Errorf("first block should be Done=false")
	}
	if got, want := updates[2].Speed, "1.10x"; got != want {
		t.Errorf("last Speed: got %q, want %q", got, want)
	}
	if updates[1].FPS == 0 {
		t.Errorf("expected non-zero fps in update 1")
	}
}

func TestParse_IgnoresUnknownKeys(t *testing.T) {
	input := `random_key=ignored
out_time_us=1000000
progress=continue
`
	var updates []Update
	err := Parse(strings.NewReader(input), func(u Update) { updates = append(updates, u) })
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 {
		t.Fatalf("blocks: got %d, want 1", len(updates))
	}
	if updates[0].OutTimeUs != 1000000 {
		t.Errorf("OutTimeUs: got %d", updates[0].OutTimeUs)
	}
}

func TestParse_HandlesBlankAndMalformedLines(t *testing.T) {
	input := `

malformed line with no equals
out_time_us=2000000

progress=continue
`
	var got Update
	err := Parse(strings.NewReader(input), func(u Update) { got = u })
	if err != nil {
		t.Fatal(err)
	}
	if got.OutTimeUs != 2000000 {
		t.Errorf("OutTimeUs: got %d, want 2000000", got.OutTimeUs)
	}
}

func TestNewBar_ReturnsBar(t *testing.T) {
	bar := NewBar(60_000_000, "test")
	if bar == nil {
		t.Fatal("expected non-nil bar")
	}
	_ = bar.Set64(30_000_000)
}
