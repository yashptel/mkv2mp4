// Command mkv2mp4 converts MKV files to LG-OLED-friendly MP4.
//
// See README.md for usage. The high-level flow per file is:
//
//	probe -> plan -> (optional DV P7->P8.1 pipeline) -> mux/transcode -> verify
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/yashptel/mkv2mp4/internal/convert"
	"github.com/yashptel/mkv2mp4/internal/deps"
	"github.com/yashptel/mkv2mp4/internal/fsutil"
	"github.com/yashptel/mkv2mp4/internal/plan"
	"github.com/yashptel/mkv2mp4/internal/probe"
	"github.com/yashptel/mkv2mp4/internal/progress"
	"github.com/yashptel/mkv2mp4/internal/selfupdate"

	"github.com/schollz/progressbar/v3"
)

// repoSlug points at the GitHub repo used for self-update lookups.
const repoSlug = "yashptel/mkv2mp4"

// version is overridden at link time by goreleaser via -ldflags.
var version = "dev"

type stringSlice []string

func (s *stringSlice) String() string     { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error { *s = append(*s, v); return nil }

type cliOpts struct {
	recursive    bool
	outputDir    string
	force        bool
	deleteSource bool
	sidecars     stringSlice
	verbose      bool
	quiet        bool
	yes          bool
	updateDeps   bool
	selfUpdate   bool
	checkUpdate  bool
	binDir       string
	showVersion  bool
}

func main() {
	opts := &cliOpts{}
	fs := flag.NewFlagSet("mkv2mp4", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { printUsage(os.Stderr) }

	fs.BoolVar(&opts.recursive, "r", false, "Recurse into subdirectories")
	fs.BoolVar(&opts.recursive, "recursive", false, "Recurse into subdirectories (long form)")
	fs.StringVar(&opts.outputDir, "o", "", "Output directory (default: source dir)")
	fs.StringVar(&opts.outputDir, "output", "", "Output directory (long form)")
	fs.BoolVar(&opts.force, "f", false, "Overwrite existing .mp4")
	fs.BoolVar(&opts.force, "force", false, "Overwrite existing .mp4 (long form)")
	fs.BoolVar(&opts.deleteSource, "delete-source", false, "Delete source .mkv after successful convert")
	fs.Var(&opts.sidecars, "srt", "Sidecar subtitle PATH[:LANG] (repeatable)")
	fs.BoolVar(&opts.verbose, "v", false, "Verbose ffmpeg output")
	fs.BoolVar(&opts.verbose, "verbose", false, "Verbose ffmpeg output (long form)")
	fs.BoolVar(&opts.quiet, "q", false, "Errors only")
	fs.BoolVar(&opts.quiet, "quiet", false, "Errors only (long form)")
	fs.BoolVar(&opts.yes, "yes", false, "Auto-confirm interactive prompts")
	fs.BoolVar(&opts.updateDeps, "update-deps", false, "Refetch ffmpeg/dovi_tool")
	fs.BoolVar(&opts.selfUpdate, "update", false, "Update mkv2mp4 itself to the latest release")
	fs.BoolVar(&opts.checkUpdate, "check-update", false, "Print whether a newer release exists")
	fs.StringVar(&opts.binDir, "bin-dir", "", "Override binary cache location")
	fs.BoolVar(&opts.showVersion, "version", false, "Print version and exit")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	// Clean up any leftover .old binary from a prior Windows self-update.
	selfupdate.CleanupOldBinary()

	if opts.showVersion {
		fmt.Println("mkv2mp4", version)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, opts, fs.Args()); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "cancelled")
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `mkv2mp4 - convert MKV files to LG OLED-friendly MP4.

Usage:
  mkv2mp4 [flags] <file|dir|glob>...

Flags:
  -r, --recursive          Recurse into subdirectories
  -o, --output DIR         Output directory (default: source dir)
  -f, --force              Overwrite existing .mp4
      --delete-source      Delete source .mkv after success
      --srt PATH[:LANG]    Add sidecar subtitle (repeatable)
  -v, --verbose            Show ffmpeg log
  -q, --quiet              Errors only
      --yes                Auto-confirm interactive prompts
      --update             Update mkv2mp4 itself to the latest release
      --check-update       Print whether a newer release exists
      --update-deps        Refetch ffmpeg/dovi_tool
      --bin-dir DIR        Override binary cache location
      --version            Print version
  -h, --help               This message

Examples:
  mkv2mp4 movie.mkv
  mkv2mp4 ./Season1/
  mkv2mp4 -r ./Show/
  mkv2mp4 movie.mkv --srt english.srt:eng

`)
}

func run(ctx context.Context, opts *cliOpts, args []string) error {
	if opts.selfUpdate {
		return runSelfUpdate(ctx, opts)
	}
	if opts.checkUpdate {
		return runCheckUpdate(ctx, opts)
	}

	resolver, err := deps.NewResolver(opts.binDir)
	if err != nil {
		return err
	}
	resolver.Logger = stderrFor(opts)

	if opts.updateDeps {
		return runUpdateDeps(ctx, resolver, opts)
	}
	if len(args) == 0 {
		printUsage(os.Stderr)
		return errors.New("at least one input file or directory required")
	}

	ffmpegPath, err := resolver.Resolve(ctx, deps.ToolFFmpeg)
	if err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	ffprobePath, err := resolver.Resolve(ctx, deps.ToolFFprobe)
	if err != nil {
		return fmt.Errorf("ffprobe: %w", err)
	}

	files, err := fsutil.Expand(args, opts.recursive)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("no .mkv files found")
	}

	sidecars, err := parseSidecars(opts.sidecars)
	if err != nil {
		return err
	}

	out := &fsutil.OutputResolver{OutputDir: opts.outputDir, Force: opts.force}

	if !opts.quiet {
		fmt.Fprintf(os.Stderr, "Found %d file(s).\n", len(files))
	}

	var successes, failures, skipped int
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		status, err := processFile(ctx, opts, file, sidecars, out, ffmpegPath, ffprobePath, resolver)
		switch {
		case err != nil:
			failures++
			fmt.Fprintf(os.Stderr, "  %s: %v\n", filepath.Base(file), err)
		case status == statusSkipped:
			skipped++
		default:
			successes++
		}
	}

	if !opts.quiet {
		fmt.Fprintf(os.Stderr, "\nDone: %d ok, %d skipped, %d failed.\n", successes, skipped, failures)
	}
	if failures > 0 {
		return fmt.Errorf("%d file(s) failed", failures)
	}
	return nil
}

type processStatus int

const (
	statusOK processStatus = iota
	statusSkipped
)

func processFile(
	ctx context.Context,
	opts *cliOpts,
	file string,
	sidecars []plan.SidecarSub,
	out *fsutil.OutputResolver,
	ffmpegPath, ffprobePath string,
	resolver *deps.Resolver,
) (processStatus, error) {
	if !opts.quiet {
		fmt.Fprintf(os.Stderr, "\n-> %s\n", file)
	}

	output := out.Output(file)
	if out.ShouldSkip(output) {
		if !opts.quiet {
			fmt.Fprintf(os.Stderr, "  skipping (already exists; use --force to overwrite)\n")
		}
		return statusSkipped, nil
	}
	if err := fsutil.EnsureParentDir(output); err != nil {
		return statusOK, fmt.Errorf("create output dir: %w", err)
	}

	info, err := probe.Probe(ctx, ffprobePath, file)
	if err != nil {
		return statusOK, fmt.Errorf("probe: %w", err)
	}

	p := plan.Build(info, sidecars)
	p.Output = output

	if p.HasIncompatibleVideo() && !opts.yes {
		if !confirmVideoTranscode(info, p) {
			if !opts.quiet {
				fmt.Fprintf(os.Stderr, "  skipping (video transcode declined)\n")
			}
			return statusSkipped, nil
		}
	}

	if p.NeedsDVConvert {
		doviPath, err := resolver.Resolve(ctx, deps.ToolDoviTool)
		if err != nil {
			return statusOK, fmt.Errorf("dovi_tool: %w", err)
		}
		if err := runDVPipeline(ctx, opts, p, ffmpegPath, doviPath); err != nil {
			return statusOK, err
		}
	} else {
		if err := runFFmpeg(ctx, opts, p, ffmpegPath); err != nil {
			return statusOK, err
		}
	}

	if opts.deleteSource {
		if err := os.Remove(file); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: could not delete source: %v\n", err)
		}
	}

	return statusOK, nil
}

func runFFmpeg(ctx context.Context, opts *cliOpts, p *plan.ConversionPlan, ffmpegPath string) error {
	args := convert.BuildArgs(p)
	return execFFmpeg(ctx, opts, ffmpegPath, args, p, "remuxing")
}

func runDVPipeline(ctx context.Context, opts *cliOpts, p *plan.ConversionPlan, ffmpegPath, doviPath string) error {
	level := "warning"
	if opts.verbose {
		level = "info"
	}
	if opts.quiet {
		level = "error"
	}
	bar := makeProgressBar(opts, p, "DV converting")
	pipeline := &convert.DVPipeline{
		FFmpegPath:      ffmpegPath,
		DoviToolPath:    doviPath,
		Plan:            p,
		Logger:          stderrFor(opts),
		ExtraGlobalArgs: []string{"-loglevel", level},
		CaptureStderr:   bar != nil && !opts.verbose,
	}
	if bar != nil {
		pipeline.OnProgress = func(u progress.Update) {
			_ = bar.Set64(u.OutTimeUs)
			if u.Done {
				_ = bar.Finish()
			}
		}
	}
	return pipeline.Run(ctx)
}

func execFFmpeg(ctx context.Context, opts *cliOpts, ffmpegPath string, args []string, p *plan.ConversionPlan, label string) error {
	bar := makeProgressBar(opts, p, label)
	args = withLogLevel(args, opts)
	if bar != nil {
		// inject -progress pipe:1 just before the output arg
		out := args[len(args)-1]
		args = append(args[:len(args)-1:len(args)-1], "-progress", "pipe:1", out)
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)

	// Buffer stderr when a progress bar is active so ffmpeg warnings (e.g. the
	// benign "Multiple -codec ... will be used" lines) don't break the bar's
	// in-place redraw. Dump the buffer if the run fails.
	var stderrBuf bytes.Buffer
	captureStderr := bar != nil && !opts.verbose
	if captureStderr {
		cmd.Stderr = &stderrBuf
	} else {
		cmd.Stderr = stderrFor(opts)
	}

	runErr := runWithBar(cmd, bar, opts)
	if runErr != nil && captureStderr && stderrBuf.Len() > 0 {
		fmt.Fprintln(os.Stderr, "--- ffmpeg stderr ---")
		os.Stderr.Write(stderrBuf.Bytes())
	}
	return runErr
}

func runWithBar(cmd *exec.Cmd, bar *progressbar.ProgressBar, opts *cliOpts) error {
	if bar == nil {
		if opts.verbose {
			cmd.Stdout = os.Stderr
		}
		return cmd.Run()
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	_, _ = progress.Drive(stdout, bar)
	return cmd.Wait()
}

func withLogLevel(args []string, opts *cliOpts) []string {
	level := "warning"
	if opts.verbose {
		level = "info"
	}
	if opts.quiet {
		level = "error"
	}
	return append([]string{"-loglevel", level}, args...)
}

func makeProgressBar(opts *cliOpts, p *plan.ConversionPlan, label string) *progressbar.ProgressBar {
	if opts.quiet || opts.verbose {
		// verbose passes ffmpeg log through, which would garble a progress bar.
		return nil
	}
	if p.Duration <= 0 {
		return nil
	}
	totalUs := int64(p.Duration * 1_000_000)
	return progress.NewBar(totalUs, fmt.Sprintf("  %s %s", label, filepath.Base(p.Input)))
}

func runSelfUpdate(ctx context.Context, opts *cliOpts) error {
	res, err := selfupdate.Run(ctx, selfupdate.Options{
		Repo:           repoSlug,
		CurrentVersion: version,
		Logger:         stderrFor(opts),
	})
	if err != nil {
		return err
	}
	if !res.Updated {
		fmt.Fprintf(os.Stderr, "Already up to date (%s).\n", res.CurrentVersion)
		return nil
	}
	fmt.Fprintf(os.Stderr, "Updated %s -> %s (%s)\n", res.CurrentVersion, res.LatestVersion, res.BinaryPath)
	return nil
}

func runCheckUpdate(ctx context.Context, opts *cliOpts) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	res, err := selfupdate.Check(ctx, selfupdate.Options{
		Repo:           repoSlug,
		CurrentVersion: version,
	})
	if err != nil {
		return err
	}
	if res.HasUpdate {
		fmt.Fprintf(os.Stderr, "A newer release is available: %s (current: %s)\n", res.LatestVersion, res.CurrentVersion)
		fmt.Fprintln(os.Stderr, "  run `mkv2mp4 --update` to upgrade.")
		return nil
	}
	fmt.Fprintf(os.Stderr, "Up to date (%s).\n", res.CurrentVersion)
	return nil
}

func runUpdateDeps(ctx context.Context, resolver *deps.Resolver, opts *cliOpts) error {
	resolver.Update = true
	for _, tool := range deps.AllTools {
		path, err := resolver.Resolve(ctx, tool)
		if err != nil {
			return fmt.Errorf("update %s: %w", tool, err)
		}
		if !opts.quiet {
			fmt.Fprintf(os.Stderr, "%s -> %s\n", tool, path)
		}
	}
	return nil
}

func parseSidecars(raw []string) ([]plan.SidecarSub, error) {
	out := make([]plan.SidecarSub, 0, len(raw))
	for _, r := range raw {
		path, lang, _ := strings.Cut(r, ":")
		if path == "" {
			return nil, fmt.Errorf("invalid --srt %q (expected PATH[:LANG])", r)
		}
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("sidecar %s: %w", path, err)
		}
		out = append(out, plan.SidecarSub{Path: path, Language: lang})
	}
	return out, nil
}

func confirmVideoTranscode(info *probe.FileInfo, p *plan.ConversionPlan) bool {
	codec := ""
	for _, s := range info.Streams {
		if s.Index == p.VideoStream {
			codec = s.CodecName
			break
		}
	}
	fmt.Fprintf(os.Stderr,
		"  video codec %q is not directly playable; transcode to HEVC?\n  (this can take hours per file) [y/N]: ",
		codec)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func stderrFor(opts *cliOpts) io.Writer {
	if opts.quiet {
		return io.Discard
	}
	return os.Stderr
}
