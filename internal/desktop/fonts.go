package desktop

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/go-text/typesetting/fontscan"

	"github.com/ninghaofeng/pgs_converter/internal/fontinfo"
)

type fontWarnings struct{ lines []string }

func (w *fontWarnings) Printf(format string, args ...interface{}) {
	w.lines = append(w.lines, fmt.Sprintf(format, args...))
}

func scanFontFamilies(ctx context.Context, system, extra []string) ([]string, []string, error) {
	catalog, err := fontinfo.Scan(ctx, system, extra)
	return catalog.Families, catalog.Warnings, err
}

func filterFontFamilies(families []string, query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	var matches []string
	for _, family := range families {
		if strings.Contains(strings.ToLower(family), query) {
			matches = append(matches, family)
		}
	}
	return matches
}

func (w *window) chooseFont() {
	if w.fontCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.fontCancel = cancel
	search := widget.NewEntry()
	search.SetPlaceHolder(w.tr("Search font families..."))
	status := widget.NewLabel(w.tr("Scanning system and extra font folders..."))
	status.Wrapping = fyne.TextWrapWord
	var families, matches []string
	selected := ""
	var d *dialog.CustomDialog
	choose := widget.NewButton(w.tr("Use font"), func() {
		w.font.SetText(selected)
		d.Hide()
	})
	choose.Disable()
	list := widget.NewList(func() int { return len(matches) }, func() fyne.CanvasObject {
		label := widget.NewLabel("")
		label.Truncation = fyne.TextTruncateEllipsis
		return label
	}, func(id widget.ListItemID, object fyne.CanvasObject) {
		object.(*widget.Label).SetText(matches[id])
	})
	list.OnSelected = func(id widget.ListItemID) {
		selected = matches[id]
		choose.Enable()
	}
	count := 0
	search.OnChanged = func(query string) {
		list.UnselectAll()
		selected = ""
		choose.Disable()
		matches = filterFontFamilies(families, query)
		list.Refresh()
		status.SetText(w.tr("%d matching font families; %d scan warnings (see log).", len(matches), count))
	}
	d = dialog.NewCustom(w.tr("Choose SRT font"), w.tr("Cancel"),
		container.NewBorder(container.NewVBox(search, status), choose, nil, nil, list), w.win)
	d.SetOnClosed(func() {
		cancel()
		w.fontCancel = nil
	})
	d.Resize(fyne.NewSize(560, 480))
	d.Show()
	extra := strings.FieldsFunc(w.fonts.Text, func(r rune) bool { return r == '\n' })
	for i := range extra {
		extra[i] = strings.TrimSpace(extra[i])
	}
	extra = slices.DeleteFunc(extra, func(s string) bool { return s == "" })
	go func() {
		logger := &fontWarnings{}
		dirs, err := fontscan.DefaultFontDirectories(logger)
		var found, warnings []string
		if err == nil {
			found, warnings, err = scanFontFamilies(ctx, dirs, extra)
		}
		fyne.Do(func() {
			// The dialog may have closed while metadata was being parsed.
			if ctx.Err() != nil {
				return
			}
			for _, warning := range append(logger.lines, warnings...) {
				w.appendLog("FONT SCAN WARNING: " + warning)
			}
			if err != nil {
				status.SetText(w.tr("Font scan failed; see log."))
				w.showError(uiError("Could not scan fonts: %v", err))
				return
			}
			count = len(logger.lines) + len(warnings)
			families = found
			search.OnChanged(search.Text)
		})
	}()
}

// scheduleStylePreview debounces only SRT typography edits. Generation checks
// suppress timers already firing when Stop or native-render cancellation loses a race.
func (w *window) scheduleStylePreview() {
	if w.styleTimer != nil {
		w.styleTimer.Stop()
	}
	if w.selected < 0 || w.selected >= len(w.rows) ||
		!strings.EqualFold(filepath.Ext(w.rows[w.selected].job.Input), ".srt") {
		return
	}
	if w.previewCancel != nil {
		w.previewCancel()
	}
	w.previewID++
	id := w.previewID
	if _, _, err := w.config().Options(); err != nil {
		w.say(w.previewStatus, "Complete valid settings to refresh the SRT style preview.")
		return
	}
	w.say(w.previewStatus, "Waiting to refresh SRT style preview...")
	w.styleTimer = time.AfterFunc(350*time.Millisecond, func() {
		fyne.Do(func() {
			if id == w.previewID {
				w.preview(len(w.intervals) == 0)
			}
		})
	})
}
