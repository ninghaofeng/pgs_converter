// Package cli validates command-line configuration, discovers batch inputs and
// schedules isolated conversions. One collector owns progress output and exit
// status so worker completion order cannot interleave individual log messages.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ninghaofeng/pgs_converter/internal/batch"
	"github.com/ninghaofeng/pgs_converter/internal/colorx"
	"github.com/ninghaofeng/pgs_converter/internal/convert"
	"github.com/ninghaofeng/pgs_converter/internal/subtitle"
	"github.com/spf13/pflag"
)

// Version is replaced by release builds using -ldflags "-X .../cli.Version=...".
var Version = "dev"

// Run executes CLI arguments without the executable name and never calls os.Exit.
// It returns 0 for success/help/version, 1 for conversion failures, 2 for invalid
// configuration and 130 for cancellation. Conversion progress goes to stderr;
// stdout is reserved for top-level help and version output.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, "pgs-converter convert INPUT --size WxH --fps RATE -o OUTPUT\npgs-converter batch INPUT... --size WxH --fps RATE --output-dir DIR [--recursive]\npgs-converter version\nUse COMMAND --help for options.")
		return 0
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Fprintln(stdout, Version)
		return 0
	}
	command := args[0]
	if command != "convert" && command != "batch" {
		fmt.Fprintf(stderr, "error: unknown command %q\n", command)
		return 2
	}
	flags := pflag.NewFlagSet(command, pflag.ContinueOnError)
	flags.SetOutput(stderr)
	var opt convert.Options
	var size, fps, output, outDir, profile string
	var white float64
	var recursive, noProgress bool
	var workers int
	flags.StringVar(&size, "size", "", "Required output canvas, e.g. 1920x1080")
	flags.StringVar(&fps, "fps", "", "Required frame rate, e.g. 24 or 24000/1001")
	flags.StringVarP(&output, "output", "o", "", "Single-file output .sup path")
	flags.StringVar(&outDir, "output-dir", "", "Batch output directory")
	flags.StringVar(&opt.Encoding, "encoding", "auto", "auto, utf-8, utf-16le, utf-16be, gb18030, big5")
	flags.StringArrayVar(&opt.Fonts, "font-dir", nil, "Additional font directory (repeatable, non-recursive)")
	flags.StringVar(&opt.Style.Font, "font", "sans-serif", "SRT font family (ASS/SSA retain script styles)")
	flags.IntVar(&opt.Style.Size, "font-size", 0, "SRT font size (default canvas height / 24)")
	flags.IntVar(&opt.Style.Margin, "margin", -1, "SRT margin in pixels (default canvas height / 24)")
	flags.BoolVar(&opt.Strict, "strict", false, "Fail on missing ASS/SSA fonts, libass diagnostics or invisible short events")
	flags.BoolVar(&opt.Overwrite, "overwrite", false, "Replace existing outputs only after successful conversion")
	flags.BoolVar(&recursive, "recursive", false, "Recursively discover batch subtitles")
	flags.IntVar(&workers, "jobs", 1, "Concurrent batch conversions (1..32)")
	flags.BoolVar(&noProgress, "no-progress", false, "Disable live per-file progress (keep results and diagnostics)")
	flags.StringVar(&profile, "color-profile", "sdr", "sdr or experimental hdr10 (BT.2020/PQ)")
	flags.Float64Var(&white, "subtitle-white-nits", 203, "HDR reference white (0..10000], default 203 nits")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	dimensions := strings.Split(size, "x")
	if len(dimensions) != 2 {
		return fail(fmt.Errorf("--size WIDTHxHEIGHT is required"))
	}
	var err error
	opt.Width, err = strconv.Atoi(dimensions[0])
	if err != nil {
		return fail(fmt.Errorf("invalid width"))
	}
	opt.Height, err = strconv.Atoi(dimensions[1])
	if err != nil {
		return fail(fmt.Errorf("invalid height"))
	}
	opt.Rate, err = subtitle.ParseRate(fps)
	if err != nil {
		return fail(err)
	}
	// Distinguish an explicitly supplied flag from its default: reference white
	// must not be silently accepted and ignored by the SDR profile.
	if flags.Changed("subtitle-white-nits") && profile != "hdr10" {
		return fail(fmt.Errorf("--subtitle-white-nits requires --color-profile hdr10"))
	}
	opt.Profile, err = colorx.New(profile, white)
	if err != nil {
		return fail(err)
	}
	if opt.Style.Size == 0 && !flags.Changed("font-size") {
		opt.Style.Size = max(1, opt.Height/24)
	}
	if opt.Style.Margin == -1 && !flags.Changed("margin") {
		opt.Style.Margin = max(1, opt.Height/24)
	}
	if opt.Style.Size <= 0 || opt.Style.Margin < 0 || opt.Style.Font == "" || strings.ContainsAny(opt.Style.Font, ",\r\n") {
		return fail(fmt.Errorf("invalid SRT font, size or margin"))
	}
	if err := opt.Validate(); err != nil {
		return fail(err)
	}
	if workers < 1 || workers > 32 {
		return fail(fmt.Errorf("--jobs must be in 1..32"))
	}
	var jobs []batch.Job
	if command == "convert" {
		if flags.NArg() != 1 || output == "" || outDir != "" || recursive || flags.Changed("jobs") {
			return fail(fmt.Errorf("convert requires one input and -o OUTPUT; batch-only options are not accepted"))
		}
		if !strings.EqualFold(filepath.Ext(output), ".sup") {
			return fail(fmt.Errorf("output must have .sup extension"))
		}
		jobs = []batch.Job{{Input: flags.Arg(0), Output: output}}
	} else {
		if flags.NArg() < 1 || outDir == "" || output != "" {
			return fail(fmt.Errorf("batch requires inputs and --output-dir DIR; -o is not accepted"))
		}
		jobs = batch.Discover(flags.Args(), outDir, recursive)
		if len(jobs) == 0 {
			return fail(fmt.Errorf("no SRT, ASS or SSA files found"))
		}
	}
	// Compatibility notices describe the whole invocation, not a single file.
	// Include them once in the summary without making strict rendering fail.
	globalWarnings := 0
	fmt.Fprintf(stderr, "profile=%s", profile)
	if opt.Profile.HDR {
		globalWarnings++
		fmt.Fprintf(stderr, " reference-white=%g-nits\nwarning: experimental BT.2020/PQ palette; SUP has no portable HDR signaling; player interpretation must be verified\n", white)
		if white > 300 {
			globalWarnings++
			fmt.Fprintln(stderr, "warning: high subtitle reference white may be uncomfortable on HDR displays")
		}
	} else {
		fmt.Fprintln(stderr)
		if opt.Height <= 576 {
			globalWarnings++
			fmt.Fprintln(stderr, "warning: SDR palette uses BT.709; some players assume BT.601 for canvases at or below 576 lines")
		}
	}
	results, err := batch.Run(ctx, jobs, opt, workers, !noProgress)
	if err != nil {
		return fail(err)
	}
	// Serialize diagnostics here rather than writing from conversion workers.
	// Completion order is intentionally not the original input order.
	success, failed, warnings := 0, 0, globalWarnings
	for done := range results {
		if done.Started {
			fmt.Fprintf(stderr, "START %s -> %s\n", done.Input, done.Output)
			continue
		}
		if p := done.Progress; p != nil {
			percent := 100 * float64(p.Processed) / float64(p.Total)
			if p.Processed < p.Total {
				percent = min(percent, 99.9)
			}
			fmt.Fprintf(stderr, "progress: %s: %.1f%% (%d/%d sampled frames, %d display sets)\n",
				done.Input, percent, p.Processed, p.Total, p.DisplaySets)
			continue
		}
		for _, warning := range done.Result.Warnings {
			fmt.Fprintf(stderr, "warning: %s: %s\n", done.Input, warning)
			warnings++
		}
		if done.Err != nil {
			fmt.Fprintf(stderr, "FAIL %s -> %s: %v\n", done.Input, done.Output, done.Err)
			failed++
		} else {
			fmt.Fprintf(stderr, "OK %s -> %s (%d display sets)\n", done.Input, done.Output, done.Result.Frames)
			success++
		}
	}
	fmt.Fprintf(stderr, "summary: %d succeeded, %d failed, %d warnings, %d not processed\n", success, failed, warnings, len(jobs)-success-failed)
	if ctx.Err() != nil {
		return 130
	}
	if failed > 0 {
		return 1
	}
	return 0
}
