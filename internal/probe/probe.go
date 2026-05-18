// Package probe wraps ffprobe and exposes a typed view of a media file's streams.
package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
)

type StreamInfo struct {
	Index     int
	CodecType string
	CodecName string
	Channels  int
	Language  string
	DVProfile int
}

type FileInfo struct {
	Path     string
	Duration float64
	Streams  []StreamInfo
}

// Probe invokes ffprobe and returns a parsed FileInfo.
func Probe(ctx context.Context, ffprobePath, file string) (*FileInfo, error) {
	args := []string{
		"-v", "error",
		"-show_format",
		"-show_streams",
		"-of", "json",
		file,
	}
	out, err := exec.CommandContext(ctx, ffprobePath, args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("ffprobe %s: %w (stderr: %s)", file, err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("ffprobe %s: %w", file, err)
	}
	return Parse(file, out)
}

// Parse parses raw ffprobe JSON output. Exposed so tests can use captured fixtures.
func Parse(path string, raw []byte) (*FileInfo, error) {
	var r rawFFprobe
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parse ffprobe json: %w", err)
	}
	streams := make([]StreamInfo, 0, len(r.Streams))
	for _, s := range r.Streams {
		streams = append(streams, StreamInfo{
			Index:     s.Index,
			CodecType: s.CodecType,
			CodecName: s.CodecName,
			Channels:  s.Channels,
			Language:  s.Tags["language"],
			DVProfile: extractDVProfile(s.SideDataList),
		})
	}
	dur, _ := strconv.ParseFloat(r.Format.Duration, 64)
	return &FileInfo{Path: path, Duration: dur, Streams: streams}, nil
}

type rawFFprobe struct {
	Streams []rawStream `json:"streams"`
	Format  rawFormat   `json:"format"`
}

type rawStream struct {
	Index        int               `json:"index"`
	CodecName    string            `json:"codec_name"`
	CodecType    string            `json:"codec_type"`
	Channels     int               `json:"channels"`
	Tags         map[string]string `json:"tags"`
	SideDataList []map[string]any  `json:"side_data_list"`
}

type rawFormat struct {
	Duration string `json:"duration"`
}

func extractDVProfile(sideData []map[string]any) int {
	for _, sd := range sideData {
		t, _ := sd["side_data_type"].(string)
		if t != "DOVI configuration record" {
			continue
		}
		switch v := sd["dv_profile"].(type) {
		case float64:
			return int(v)
		case int:
			return v
		case string:
			n, _ := strconv.Atoi(v)
			return n
		}
	}
	return 0
}
