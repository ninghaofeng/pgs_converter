// Package cli validates command-line configuration, discovers batch inputs and
// schedules isolated conversions. One collector owns progress output and exit
// status so worker completion order cannot interleave individual log messages.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/ninghaofeng/pgs_converter/internal/colorx"
	"github.com/ninghaofeng/pgs_converter/internal/convert"
	"github.com/ninghaofeng/pgs_converter/internal/subtitle"
	"github.com/spf13/pflag"
)

// Version is replaced by release builds using -ldflags "-X .../cli.Version=...".
var Version = "dev"

// job also represents discovery failures, keeping them in normal batch results
// instead of aborting otherwise valid files before workers can run.
type job struct {
	input, output string
	err           error
}

type completed struct {
	job
	result   convert.Result
	progress *convert.Progress
	started  bool
}

func supported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".srt", ".ass", ".ssa":
		return true
	}
	return false
}

// discover preserves paths relative to each directory root and flattens
// individually listed files. WalkDir does not follow directory symlinks.
// Colliding destinations fail every contributing input before writes begin.
func discover(inputs []string, output string, recursive bool) []job {
	var jobs []job
	for _, input := range inputs {
		info, err := os.Stat(input)
		if err != nil {
			jobs = append(jobs, job{input: input, err: err})
			continue
		}
		if !info.IsDir() {
			if !supported(input) {
				jobs = append(jobs, job{input: input, err: fmt.Errorf("unsupported subtitle extension")})
			} else {
				jobs = append(jobs, job{input: input, output: filepath.Join(output, strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))+".sup")})
			}
			continue
		}
		err = filepath.WalkDir(input, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				jobs = append(jobs, job{input: path, err: walkErr})
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if path != input && !recursive {
					return filepath.SkipDir
				}
				return nil
			}
			if !supported(path) {
				return nil
			}
			rel, err := filepath.Rel(input, path)
			if err != nil {
				return err
			}
			jobs = append(jobs, job{input: path, output: filepath.Join(output, strings.TrimSuffix(rel, filepath.Ext(rel))+".sup")})
			return nil
		})
		if err != nil {
			jobs = append(jobs, job{input: input, err: err})
		}
	}
	// Compare absolute, case-folded paths even on case-sensitive hosts so a
	// batch has the same conservative collision policy on Windows and macOS.
	destinations := make(map[string][]int)
	for i, j := range jobs {
		if j.err != nil {
			continue
		}
		abs, err := filepath.Abs(j.output)
		if err != nil {
			jobs[i].err = err
			continue
		}
		destinations[strings.ToLower(abs)] = append(destinations[strings.ToLower(abs)], i)
	}
	for _, indexes := range destinations {
		if len(indexes) > 1 {
			for _, i := range indexes {
				jobs[i].err = fmt.Errorf("multiple inputs map to output %s", jobs[i].output)
			}
		}
	}
	return jobs
}

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
	flags.BoolVar(&opt.Strict, "strict", false, "Fail on libass diagnostics or invisible short events")
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
	var jobs []job
	if command == "convert" {
		if flags.NArg() != 1 || output == "" || outDir != "" || recursive || flags.Changed("jobs") {
			return fail(fmt.Errorf("convert requires one input and -o OUTPUT; batch-only options are not accepted"))
		}
		if !strings.EqualFold(filepath.Ext(output), ".sup") {
			return fail(fmt.Errorf("output must have .sup extension"))
		}
		jobs = []job{{input: flags.Arg(0), output: output}}
	} else {
		if flags.NArg() < 1 || outDir == "" || output != "" {
			return fail(fmt.Errorf("batch requires inputs and --output-dir DIR; -o is not accepted"))
		}
		jobs = discover(flags.Args(), outDir, recursive)
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
	// Unbuffered dispatch limits active work to the worker count. Results are
	// collected below while workers run, so completion sends cannot deadlock.
	tasks := make(chan job)
	results := make(chan completed)
	var wg sync.WaitGroup
	for i := 0; i < min(workers, len(jobs)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range tasks {
				result := convert.Result{}
				if j.err == nil {
					if err := ctx.Err(); err != nil {
						j.err = err
					} else {
						fileOpt := opt
						if !noProgress {
							results <- completed{job: j, started: true}
							fileOpt.Progress = func(progress convert.Progress) {
								results <- completed{job: j, progress: &progress}
							}
						}
						// Each File call owns its libass state and staged output.
						result, j.err = convert.File(ctx, j.input, j.output, fileOpt)
					}
				}
				results <- completed{job: j, result: result}
			}
		}()
	}
	// Only the producer closes tasks; cancellation stops further scheduling
	// once observed, while the collector still drains all active conversions.
	go func() {
		defer close(tasks)
		for _, j := range jobs {
			select {
			case tasks <- j:
			case <-ctx.Done():
				return
			}
		}
	}()
	// Results close only after every worker exits, never from a worker itself.
	go func() {
		wg.Wait()
		close(results)
	}()
	// Serialize diagnostics here rather than writing from conversion workers.
	// Completion order is intentionally not the original input order.
	success, failed, warnings := 0, 0, globalWarnings
	for done := range results {
		if done.started {
			fmt.Fprintf(stderr, "START %s -> %s\n", done.input, done.output)
			continue
		}
		if p := done.progress; p != nil {
			percent := 100 * float64(p.Processed) / float64(p.Total)
			if p.Processed < p.Total {
				percent = min(percent, 99.9)
			}
			fmt.Fprintf(stderr, "progress: %s: %.1f%% (%d/%d sampled frames, %d display sets)\n",
				done.input, percent, p.Processed, p.Total, p.DisplaySets)
			continue
		}
		for _, warning := range done.result.Warnings {
			fmt.Fprintf(stderr, "warning: %s: %s\n", done.input, warning)
			warnings++
		}
		if done.err != nil {
			fmt.Fprintf(stderr, "FAIL %s -> %s: %v\n", done.input, done.output, done.err)
			failed++
		} else {
			fmt.Fprintf(stderr, "OK %s -> %s (%d display sets)\n", done.input, done.output, done.result.Frames)
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
