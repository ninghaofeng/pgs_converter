package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/ninghaofeng/pgs_converter/internal/batch"
	"github.com/ninghaofeng/pgs_converter/internal/convert"
)

type missingFonts struct {
	path  string
	names []string
}

func (w *window) preflightFonts(ctx context.Context, cancel context.CancelFunc, jobs []batch.Job, opt convert.Options, workers int, config Config) {
	go func() {
		var missing []missingFonts
		var warnings []string
		failedIndex := -1
		var failure error
		for i, job := range jobs {
			report, err := convert.CheckFonts(ctx, job.Input, opt)
			for _, warning := range report.Warnings {
				warnings = append(warnings, job.Input+": "+warning)
			}
			if err != nil {
				failedIndex, failure = i, err
				break
			}
			if len(report.Missing) > 0 {
				missing = append(missing, missingFonts{job.Input, report.Missing})
			}
		}
		fyne.Do(func() {
			if ctx.Err() != nil || errors.Is(failure, context.Canceled) {
				cancel()
				w.finish(true)
				return
			}
			for _, warning := range warnings {
				w.appendLog("FONT PREFLIGHT WARNING: " + warning)
			}
			if failure != nil {
				w.apply(batch.Event{Index: failedIndex, Job: batch.Job{Input: jobs[failedIndex].Input, Err: failure}})
				cancel()
				w.showError(uiError("Font preflight failed: %v", failure))
				w.finish(false)
				return
			}
			if len(missing) == 0 {
				w.startConversions(ctx, cancel, jobs, opt, workers, config)
				return
			}
			w.confirmMissingFonts(ctx, cancel, missing, opt.Strict, func() {
				w.startConversions(ctx, cancel, jobs, opt, workers, config)
			})
		})
	}()
}

func (w *window) confirmMissingFonts(ctx context.Context, cancel context.CancelFunc, missing []missingFonts, strict bool, proceed func()) {
	var details strings.Builder
	for _, file := range missing {
		fmt.Fprintf(&details, "%s\n  %s\n\n", file.path, strings.Join(file.names, "\n  "))
		w.appendLog(fmt.Sprintf("FONT PREFLIGHT: %s: missing fonts: %s", file.path, strings.Join(file.names, ", ")))
	}
	if strict {
		for i, row := range w.rows {
			for _, file := range missing {
				if row.job.Input == file.path {
					w.apply(batch.Event{Index: i, Job: batch.Job{Input: file.path, Err: uiError("Missing required fonts: %s", strings.Join(file.names, ", "))}})
				}
			}
		}
		cancel()
		w.showError(uiError("Strict mode: required fonts are missing. Add font folders and retry.\n%s", details.String()))
		w.finish(false)
		return
	}
	w.say(w.status, "Missing fonts found; waiting for confirmation.")
	content := widget.NewLabel(w.tr("These required fonts were not found. Continuing allows substitution and may change the subtitle appearance.\n\n%s", details.String()))
	content.Wrapping = fyne.TextWrapWord
	responded := false
	closed := make(chan struct{})
	d := dialog.NewCustomConfirm(w.tr("Missing subtitle fonts"), w.tr("Continue with font fallback"), w.tr("Cancel conversion"),
		container.NewVScroll(content), func(ok bool) {
			if responded {
				return
			}
			responded = true
			close(closed)
			if ok && ctx.Err() == nil {
				proceed()
			} else {
				cancel()
				w.finish(true)
			}
		}, w.win)
	d.Resize(fyne.NewSize(660, 460))
	d.Show()
	go func() {
		select {
		case <-ctx.Done():
			fyne.Do(func() {
				if !responded {
					d.Hide()
				}
			})
		case <-closed:
		}
	}()
}
