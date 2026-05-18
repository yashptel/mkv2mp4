package convert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/yashptel/mkv2mp4/internal/plan"
	"github.com/yashptel/mkv2mp4/internal/progress"
)

// BuildDVExtractArgs returns ffmpeg args that dump the input's video stream
// to a raw HEVC bitstream (Annex-B format) suitable for dovi_tool.
func BuildDVExtractArgs(input, hevcOutput string) []string {
	return []string{
		"-hide_banner", "-y", "-nostdin",
		"-i", input,
		"-map", "0:v:0",
		"-c", "copy",
		"-bsf:v", "hevc_mp4toannexb",
		"-f", "hevc",
		hevcOutput,
	}
}

// BuildDoviConvertArgs returns dovi_tool args that convert a Profile 7
// HEVC bitstream into a single-layer Profile 8.1 stream (discarding the EL).
func BuildDoviConvertArgs(hevcIn, hevcOut string) []string {
	return []string{
		"-m", "2",
		"convert",
		"--discard",
		"-o", hevcOut,
		hevcIn,
	}
}

// BuildDVMuxArgs returns ffmpeg args for the final mux step of the DV
// pipeline: video from the converted HEVC (input 0), audio+embedded subs from
// the original MKV (input 1), sidecar subs from inputs 2..N.
func BuildDVMuxArgs(p *plan.ConversionPlan, hevcInput string) []string {
	args := []string{"-hide_banner", "-y", "-nostdin"}
	args = append(args, "-i", hevcInput)
	args = append(args, "-i", p.Input)
	for _, s := range p.SidecarSubs {
		args = append(args, "-i", s.Path)
	}

	args = append(args, "-map", "0:v:0")
	audios, args := mapAudio(args, p, 1)
	embeddedSubsOut, args := mapEmbeddedSubs(args, p, 1)
	for i := range p.SidecarSubs {
		args = append(args, "-map", fmt.Sprintf("%d:0", i+2))
	}

	args = append(args, "-c", "copy", "-tag:v", HEVCTag)
	args = appendCodecOverrides(args, audios, embeddedSubsOut, p.SidecarSubs)

	args = append(args, p.Output)
	return args
}

// DVPipeline orchestrates the three-step DV Profile 7 -> 8.1 conversion.
//
// Run will:
//  1. Extract the HEVC stream to a temp file.
//  2. Convert the RPU profile via dovi_tool.
//  3. Mux the converted stream + original audio/subs into the final MP4.
//
// Temp files are cleaned up on success and on context cancellation.
type DVPipeline struct {
	FFmpegPath      string
	DoviToolPath    string
	Plan            *plan.ConversionPlan
	Logger          io.Writer                    // status messages from the pipeline itself
	OnProgress      func(update progress.Update) // optional; called during step 3 (mux)
	ExtraGlobalArgs []string                     // prepended to each ffmpeg call (e.g. "-loglevel warning")

	// CaptureStderr buffers ffmpeg/dovi_tool stderr during each step and only
	// surfaces it (via the returned error) when a step fails. This keeps a
	// progress bar's redraw clean. When false, stderr streams to Logger in
	// real time (use this with --verbose).
	CaptureStderr bool
}

// Run executes the pipeline.
func (d *DVPipeline) Run(ctx context.Context) error {
	if d == nil || d.Plan == nil {
		return errors.New("DVPipeline: nil plan")
	}
	if d.FFmpegPath == "" || d.DoviToolPath == "" {
		return errors.New("DVPipeline: ffmpeg and dovi_tool paths required")
	}

	tmpDir, err := os.MkdirTemp("", "mkv2mp4-dv-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	rawHEVC := filepath.Join(tmpDir, "video.hevc")
	p81HEVC := filepath.Join(tmpDir, "video_p81.hevc")

	d.logf("DV: extracting HEVC stream...\n")
	if err := d.runFFmpeg(ctx, BuildDVExtractArgs(d.Plan.Input, rawHEVC), nil); err != nil {
		return fmt.Errorf("DV extract: %w", err)
	}

	d.logf("DV: converting Profile 7 -> 8.1 with dovi_tool...\n")
	if err := d.runDoviTool(ctx, BuildDoviConvertArgs(rawHEVC, p81HEVC)); err != nil {
		return fmt.Errorf("DV convert: %w", err)
	}

	d.logf("DV: muxing to MP4...\n")
	if err := d.runFFmpeg(ctx, BuildDVMuxArgs(d.Plan, p81HEVC), d.OnProgress); err != nil {
		return fmt.Errorf("DV mux: %w", err)
	}
	return nil
}

func (d *DVPipeline) runFFmpeg(ctx context.Context, args []string, onProgress func(progress.Update)) error {
	if len(d.ExtraGlobalArgs) > 0 {
		args = append(append([]string{}, d.ExtraGlobalArgs...), args...)
	}
	if onProgress != nil {
		out := args[len(args)-1]
		args = append(args[:len(args)-1:len(args)-1], "-progress", "pipe:1", out)
	}
	cmd := exec.CommandContext(ctx, d.FFmpegPath, args...)

	var stderrBuf bytes.Buffer
	if d.CaptureStderr {
		cmd.Stderr = &stderrBuf
	} else if d.Logger != nil {
		cmd.Stderr = d.Logger
	}
	if onProgress == nil {
		if d.CaptureStderr {
			cmd.Stdout = io.Discard
		} else if d.Logger != nil {
			cmd.Stdout = d.Logger
		}
		return wrapWithStderr(cmd.Run(), &stderrBuf, d.CaptureStderr)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = progress.Parse(stdout, onProgress)
	return wrapWithStderr(cmd.Wait(), &stderrBuf, d.CaptureStderr)
}

func (d *DVPipeline) runDoviTool(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, d.DoviToolPath, args...)
	var stderrBuf bytes.Buffer
	if d.CaptureStderr {
		cmd.Stdout = io.Discard
		cmd.Stderr = &stderrBuf
	} else if d.Logger != nil {
		cmd.Stdout = d.Logger
		cmd.Stderr = d.Logger
	}
	return wrapWithStderr(cmd.Run(), &stderrBuf, d.CaptureStderr)
}

// wrapWithStderr appends captured stderr to a non-nil error so the caller can
// surface the diagnostic context without it polluting a clean run.
func wrapWithStderr(err error, buf *bytes.Buffer, captured bool) error {
	if err == nil || !captured || buf.Len() == 0 {
		return err
	}
	return fmt.Errorf("%w\nstderr: %s", err, buf.String())
}

func (d *DVPipeline) logf(format string, args ...any) {
	if d.Logger == nil {
		return
	}
	fmt.Fprintf(d.Logger, format, args...)
}
