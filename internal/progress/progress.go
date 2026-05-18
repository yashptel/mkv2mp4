// Package progress parses ffmpeg's `-progress pipe:1` stream and drives a
// schollz/progressbar/v3 bar with current time + ETA.
package progress

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
)

// Update describes one frame of ffmpeg progress.
type Update struct {
	OutTimeUs int64   // microseconds of output produced so far
	FPS       float64 // current encoding fps
	Speed     string  // raw "1.23x" string
	Bitrate   string  // raw "2000kbits/s" string
	Done      bool    // progress=end seen
}

// Parse reads ffmpeg key=value progress blocks from r and invokes onUpdate
// once per block (delimited by progress=continue or progress=end).
func Parse(r io.Reader, onUpdate func(Update)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var current Update
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		switch key {
		case "out_time_us", "out_time_ms":
			// ffmpeg confusingly emits microseconds in BOTH out_time_us and
			// out_time_ms (the "_ms" key is a long-standing misnomer). We
			// take whichever the build emits.
			n, err := strconv.ParseInt(val, 10, 64)
			if err == nil {
				current.OutTimeUs = n
			}
		case "fps":
			f, err := strconv.ParseFloat(val, 64)
			if err == nil {
				current.FPS = f
			}
		case "speed":
			current.Speed = val
		case "bitrate":
			current.Bitrate = val
		case "progress":
			current.Done = val == "end"
			onUpdate(current)
			current = Update{}
		}
	}
	return scanner.Err()
}

// NewBar constructs a progressbar tuned for ffmpeg-driven conversion.
// total is the source media's duration in microseconds (so the bar matches
// out_time_us 1:1).
func NewBar(totalUs int64, description string) *progressbar.ProgressBar {
	return progressbar.NewOptions64(
		totalUs,
		progressbar.OptionSetDescription(description),
		progressbar.OptionShowBytes(false),
		progressbar.OptionShowCount(),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: "-",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
}

// Drive reads progress events from r and updates bar until r EOFs.
// Returns the last Update seen so callers can log final stats.
func Drive(r io.Reader, bar *progressbar.ProgressBar) (Update, error) {
	var last Update
	err := Parse(r, func(u Update) {
		last = u
		_ = bar.Set64(u.OutTimeUs)
	})
	if last.Done {
		_ = bar.Finish()
	}
	return last, err
}
