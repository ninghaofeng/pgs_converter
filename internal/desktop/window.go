package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/lang"
	"fyne.io/fyne/v2/widget"

	"github.com/ninghaofeng/pgs_converter/internal/batch"
	"github.com/ninghaofeng/pgs_converter/internal/convert"
	"github.com/ninghaofeng/pgs_converter/internal/nativepicker"
	"github.com/ninghaofeng/pgs_converter/internal/subtitle"
)

type row struct {
	job       batch.Job
	state     message
	progress  float64
	done      bool
	cancelled bool
}

type window struct {
	app            fyne.App
	version        string
	win            fyne.Window
	files          []string
	rows           []row
	selected       int
	active         bool
	scanning       bool
	closing        bool
	cancel         context.CancelFunc
	previewCancel  context.CancelFunc
	fontCancel     context.CancelFunc
	styleTimer     *time.Timer
	pickerCancel   context.CancelFunc
	openPicker     func(context.Context, fyne.Window, nativepicker.Request, func([]string, error))
	previewID      int
	previewGate    chan struct{}
	intervals      []subtitle.Interval
	logLines       []string
	language       string
	languagePicker *widget.Select
	uiScale        int
	scalePicker    *widget.Select
	timestampBox   *fyne.Container
	messages       map[*widget.Label]message
	translators    []func()

	size, fps                          *widget.SelectEntry
	profile, encoding                  *widget.Select
	white, font, fontSize, margin      *widget.Entry
	fonts, output, jobs, timestamp     *widget.Entry
	strict, overwrite, recursive       *widget.Check
	list                               *widget.List
	log                                *widget.Entry
	detail, status, previewStatus      *widget.Label
	progress                           *widget.ProgressBar
	previewImage                       *canvas.Image
	start, cancelButton, previewButton *widget.Button
}

func New(a fyne.App, version string, inputs ...string) fyne.Window {
	a.SetIcon(applicationIcon)
	w := &window{app: a, version: version, win: a.NewWindow("PGS Converter"), selected: -1, previewGate: make(chan struct{}, 1)}
	w.win.SetIcon(applicationIcon)
	var languageErr error
	w.language, languageErr = initialLanguage(a.Preferences().String("desktop-language"), lang.SystemLocale().String())
	w.uiScale = a.Preferences().IntWithFallback("desktop-ui-scale", 100)
	var scaleErr error
	if !slices.Contains(uiScalePercentages, w.uiScale) {
		scaleErr = uiError("Unsupported saved UI scale %d%%; using 100%%.", w.uiScale)
		w.uiScale = 100
	}
	config := Defaults()
	var settingsErr error
	if saved := a.Preferences().String("desktop-settings"); saved != "" {
		settingsErr = json.Unmarshal([]byte(saved), &config)
		if settingsErr != nil {
			config = Defaults()
		}
	}
	w.build(config)
	w.win.Resize(fyne.NewSize(1180, 820))
	w.win.SetOnDropped(func(_ fyne.Position, uris []fyne.URI) {
		var paths []string
		for _, uri := range uris {
			if uri.Scheme() != "file" {
				w.showError(uiError("only local files and folders are supported: %s", uri))
				return
			}
			paths = append(paths, uri.Path())
		}
		w.add(paths)
	})
	w.win.SetCloseIntercept(func() {
		if w.active {
			content := widget.NewLabel(w.tr("Cancel remaining work and close after temporary outputs are cleaned up?"))
			content.Wrapping = fyne.TextWrapWord
			d := dialog.NewCustomConfirm(w.tr("Conversion in progress"), w.tr("Cancel and close"), w.tr("Keep converting"), content, func(ok bool) {
				if ok {
					w.closing = true
					w.cancel()
					w.cancelButton.Disable()
					w.say(w.status, "Cancelling; waiting for output cleanup...")
				}
			}, w.win)
			d.Show()
			return
		}
		if w.previewCancel != nil {
			w.previewCancel()
		}
		if w.pickerCancel != nil {
			w.pickerCancel()
		}
		if w.fontCancel != nil {
			w.fontCancel()
		}
		if w.styleTimer != nil {
			w.styleTimer.Stop()
		}
		w.previewID++
		w.win.SetCloseIntercept(nil)
		w.win.Close()
	})
	if settingsErr != nil {
		w.showError(uiError("saved settings are invalid; defaults restored: %v", settingsErr))
	}
	if languageErr != nil {
		w.showError(languageErr)
	}
	if scaleErr != nil {
		w.showError(scaleErr)
	}
	if len(inputs) > 0 {
		w.add(inputs)
	}
	return w.win
}

func entry(value string) *widget.Entry {
	e := widget.NewEntry()
	e.SetText(value)
	return e
}

func (w *window) selector(options []string, value string, changed func(string)) *widget.Select {
	s := &widget.Select{Options: options, PlaceHolder: w.tr("Choose..."), OnChanged: changed}
	s.ExtendBaseWidget(s)
	s.SetSelected(value)
	w.translators = append(w.translators, func() {
		s.PlaceHolder = w.tr("Choose...")
		s.Refresh()
	})
	return s
}

func (w *window) build(c Config) {
	if w.language == "" {
		w.language = english
	}
	w.buildScalePicker()
	selectedLanguage := "English"
	if w.language == chinese {
		selectedLanguage = "简体中文"
	}
	w.languagePicker = w.selector([]string{"English", "简体中文"}, selectedLanguage, nil)
	w.languagePicker.OnChanged = func(value string) {
		code := english
		if value == "简体中文" {
			code = chinese
		}
		w.setLanguage(code)
	}
	w.size = widget.NewSelectEntry([]string{"1920x1080", "3840x2160", "1280x720"})
	w.size.SetText(c.Size)
	w.fps = widget.NewSelectEntry([]string{"24000/1001", "24", "25", "30000/1001", "30", "50", "60000/1001", "60"})
	w.fps.SetText(c.FPS)
	w.white, w.font, w.fontSize, w.margin = entry(c.White), entry(c.Font), entry(c.FontSize), entry(c.Margin)
	updatePlaceholders := func() {
		w.fontSize.SetPlaceHolder(w.tr("Auto: canvas height / 24"))
		w.margin.SetPlaceHolder(w.tr("Auto: canvas height / 24"))
	}
	updatePlaceholders()
	w.translators = append(w.translators, updatePlaceholders)
	w.profile = w.selector([]string{"sdr", "hdr10"}, c.Profile, func(value string) {
		if value == "hdr10" {
			w.white.Enable()
		} else {
			w.white.Disable()
		}
	})
	w.encoding = w.selector([]string{"auto", "utf-8", "utf-16le", "utf-16be", "gb18030", "big5"}, c.Encoding, nil)
	w.fonts = widget.NewMultiLineEntry()
	w.fonts.SetMinRowsVisible(2)
	w.fonts.SetText(c.Fonts)
	w.output, w.jobs, w.timestamp = entry(c.Output), entry(c.Jobs), entry("0")
	w.strict = w.check("Strict rendering diagnostics")
	w.strict.SetChecked(c.Strict)
	w.overwrite = w.check("Replace existing outputs after success")
	w.overwrite.SetChecked(c.Overwrite)
	w.recursive = w.check("Include subfolders when adding folders")
	w.recursive.SetChecked(c.Recursive)
	w.detail = w.label("Drop SRT / ASS / SSA files or folders into this window.")
	w.detail.Wrapping = fyne.TextWrapWord
	w.status = w.label("Ready. Settings are snapshotted when conversion starts.")
	w.previewStatus = w.label("Style preview only: SDR display; no SUP quantization or HDR luminance simulation.")
	w.previewStatus.Wrapping = fyne.TextWrapWord
	w.progress = widget.NewProgressBar()
	w.log = widget.NewMultiLineEntry()
	w.log.SetMinRowsVisible(5)
	w.log.Disable()
	w.previewImage = canvas.NewImageFromImage(image.NewNRGBA(image.Rect(0, 0, 16, 9)))
	w.previewImage.FillMode = canvas.ImageFillContain
	w.previewImage.SetMinSize(fyne.NewSize(480, 220))
	w.list = widget.NewList(func() int { return len(w.rows) }, func() fyne.CanvasObject {
		label := widget.NewLabel("filename.ass | " + w.tr("Waiting"))
		label.Truncation = fyne.TextTruncateEllipsis
		return label
	}, func(id widget.ListItemID, object fyne.CanvasObject) {
		r := w.rows[id]
		object.(*widget.Label).SetText(filepath.Base(r.job.Input) + " | " + w.tr(r.state.key, r.state.args...))
	})
	w.list.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(w.rows) {
			return
		}
		w.selected = id
		w.intervals = nil
		w.previewID++
		if w.previewCancel != nil {
			w.previewCancel()
		}
		w.say(w.detail, "%s\nOutput: %s", w.rows[id].job.Input, w.rows[id].job.Output)
		w.previewImage.Image = image.NewNRGBA(image.Rect(0, 0, 16, 9))
		w.previewImage.Refresh()
		w.say(w.previewStatus, "Use First subtitle or enter an exact time. Preview is SDR style only.")
	}
	addFile := w.button("Add file", func() {
		if !w.busy() {
			w.selectPaths(false, w.add)
		}
	})
	addFolder := w.button("Add folder", func() {
		if !w.busy() {
			w.chooseFolder(func(path string) { w.add([]string{path}) })
		}
	})
	remove := w.button("Remove selected", func() {
		if w.busy() || w.selected < 0 {
			return
		}
		index := slices.Index(w.files, w.rows[w.selected].job.Input)
		if index < 0 {
			w.showError(uiError("queued input changed on disk; clear the queue and add it again"))
			return
		}
		w.files = slices.Delete(w.files, index, index+1)
		w.plan()
		if len(w.rows) > 0 && w.rows[0].job.Err == nil {
			w.list.Select(0)
			w.preview(true)
		}
	})
	clear := w.button("Clear queue", func() {
		if !w.busy() {
			w.files = nil
			w.plan()
		}
	})
	queue := container.NewBorder(container.NewVBox(container.NewHBox(addFile, addFolder, remove, clear), w.recursive),
		w.detail, nil, nil, w.list)
	form := widget.NewForm(
		w.formItem("Language", w.languagePicker),
		w.formItem("UI scale", w.scalePicker),
		w.formItem("Resolution", w.size),
		w.formItem("Exact FPS", w.fps),
		w.formItem("Color profile", w.profile),
		w.formItem("HDR white (nits)", w.white),
		w.formItem("Input encoding", w.encoding),
		w.formItem("SRT font family", container.NewBorder(nil, nil, nil, w.button("Choose font", w.chooseFont), w.font)),
		w.formItem("SRT font size", w.fontSize),
		w.formItem("SRT margin", w.margin),
		w.formItem("Font folders (one per line)", w.fonts),
		w.formItem("Concurrency (1..32)", w.jobs),
	)
	addFont := w.button("Add font folder", func() {
		w.chooseFolder(func(path string) {
			if strings.TrimSpace(w.fonts.Text) == "" {
				w.fonts.SetText(path)
			} else {
				w.fonts.SetText(w.fonts.Text + "\n" + path)
			}
		})
	})
	settings := container.NewVScroll(container.NewVBox(form, addFont, w.strict, w.overwrite,
		w.label("SRT style settings do not override ASS/SSA styles.")))
	top := container.NewHSplit(queue, settings)
	top.Offset = 0.52
	w.previewButton = w.button("Render at time", func() { w.preview(false) })
	first := w.button("First subtitle", func() { w.preview(true) })
	previous := w.button("Previous", func() { w.navigate(-1) })
	next := w.button("Next", func() { w.navigate(1) })
	w.timestampBox = container.NewGridWrap(w.timestampSize(), w.timestamp)
	preview := container.NewBorder(
		container.NewVBox(w.previewStatus, container.NewHBox(w.label("Time (ms)"),
			w.timestampBox, w.previewButton, first, previous, next)),
		nil, nil, nil, container.NewStack(canvas.NewRectangle(color.Black), w.previewImage))
	logPanel := container.NewBorder(container.NewHBox(w.button("Copy log", func() {
		w.app.Clipboard().SetContent(strings.Join(w.logLines, "\n"))
	})), nil, nil, nil, container.NewThemeOverride(w.log, logTheme{settings: w.app.Settings()}))
	tabs := container.NewAppTabs(container.NewTabItem(w.tr("Subtitle preview"), preview), container.NewTabItem(w.tr("Log / error details"), logPanel))
	w.translators = append(w.translators, func() {
		tabs.Items[0].Text = w.tr("Subtitle preview")
		tabs.Items[1].Text = w.tr("Log / error details")
		tabs.Refresh()
	})
	main := container.NewVSplit(top, tabs)
	main.Offset = 0.6
	w.start = w.button("Start conversion", w.run)
	w.cancelButton = w.button("Cancel remaining", func() {
		if w.cancel != nil {
			w.cancel()
			w.cancelButton.Disable()
			w.say(w.status, "Cancelling; waiting for active native calls and output cleanup...")
		}
	})
	w.cancelButton.Disable()
	output := container.NewBorder(nil, nil, w.label("Output folder"), w.button("Browse", func() {
		w.chooseFolder(func(path string) { w.output.SetText(path) })
	}), w.output)
	footer := container.NewVBox(output, w.label("Flat output folder; duplicate basenames are rejected. 4K: start with concurrency 1 or 2."),
		w.progress, w.status, container.NewBorder(nil, nil, container.NewHBox(w.start, w.cancelButton),
			w.button("About PGS Converter", w.showAbout)))
	w.win.SetContent(container.NewBorder(nil, footer, nil, nil, main))
	for _, field := range []*widget.Entry{w.font, w.fontSize, w.margin, w.fonts} {
		field.OnChanged = func(string) { w.scheduleStylePreview() }
	}
}

func (w *window) config() Config {
	return Config{Size: w.size.Text, FPS: w.fps.Text, Profile: w.profile.Selected, White: w.white.Text,
		Encoding: w.encoding.Selected, Font: w.font.Text, FontSize: w.fontSize.Text, Margin: w.margin.Text,
		Fonts: w.fonts.Text, Output: w.output.Text, Jobs: w.jobs.Text,
		Strict: w.strict.Checked, Overwrite: w.overwrite.Checked, Recursive: w.recursive.Checked}
}

func (w *window) chooseFolder(accept func(string)) {
	w.selectPaths(true, func(paths []string) { accept(paths[0]) })
}

func (w *window) showError(err error) {
	w.appendLog("ERROR: " + err.Error())
	text := w.tr("Details (original diagnostic):\n%s", err.Error())
	var localized localizedError
	if errors.As(err, &localized) {
		text = w.tr(localized.key, localized.args...)
	}
	content := widget.NewLabel(text)
	content.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustom(w.tr("Error"), w.tr("OK"), container.NewVScroll(content), w.win)
	d.Resize(fyne.NewSize(560, 260))
	d.Show()
}

func (w *window) appendLog(line string) {
	w.logLines = append(w.logLines, line)
	if len(w.logLines) > 500 {
		w.logLines = w.logLines[len(w.logLines)-500:]
	}
	w.log.SetText(strings.Join(w.logLines, "\n"))
}

func (w *window) busy() bool {
	if w.active || w.scanning {
		w.showError(uiError("wait for discovery or conversion to finish before changing the queue"))
		return true
	}
	return false
}

func (w *window) add(paths []string) {
	if w.busy() || len(paths) == 0 {
		return
	}
	w.scanning = true
	w.start.Disable()
	w.say(w.status, "Discovering subtitle files...")
	recursive := w.recursive.Checked
	go func() {
		discovered := batch.Discover(paths, "", recursive)
		fyne.Do(func() {
			w.scanning = false
			w.start.Enable()
			for _, job := range discovered {
				path, err := filepath.Abs(job.Input)
				if err != nil {
					w.showError(err)
					continue
				}
				if !slices.Contains(w.files, path) {
					w.files = append(w.files, path)
				}
			}
			w.plan()
			if len(discovered) == 0 {
				w.showError(uiError("no SRT, ASS or SSA files found"))
			} else if len(w.rows) > 0 && w.rows[0].job.Err == nil {
				w.list.Select(0)
				w.preview(true)
			}
		})
	}()
}

func (w *window) plan() {
	w.previewID++
	if w.previewCancel != nil {
		w.previewCancel()
	}
	w.selected = -1
	w.intervals = nil
	w.previewImage.Image = image.NewNRGBA(image.Rect(0, 0, 16, 9))
	w.previewImage.Refresh()
	w.say(w.previewStatus, "Select a file for SDR style preview; no SUP quantization or HDR luminance simulation.")
	w.rows = nil
	for _, job := range batch.Discover(w.files, strings.TrimSpace(w.output.Text), false) {
		state := msg("Waiting")
		if job.Err != nil {
			state = msg("Blocked: %s", job.Err.Error())
		}
		w.rows = append(w.rows, row{job: job, state: state})
	}
	w.list.UnselectAll()
	w.list.Refresh()
	w.progress.SetValue(0)
	w.say(w.status, "%d queued; inspect settings and output folder before starting.", len(w.rows))
	w.say(w.detail, "Select a file to preview or remove it.")
}

func (w *window) run() {
	if w.busy() {
		return
	}
	c := w.config()
	if strings.TrimSpace(c.Output) == "" {
		w.showError(uiError("choose an output folder"))
		return
	}
	opt, workers, err := c.Options()
	if err != nil {
		w.showError(err)
		return
	}
	w.plan()
	if len(w.rows) == 0 {
		w.showError(uiError("add subtitle files first"))
		return
	}
	jobs := make([]batch.Job, len(w.rows))
	for i, r := range w.rows {
		if r.job.Err != nil {
			w.showError(uiError("%s: %v; remove blocked inputs before starting", r.job.Input, r.job.Err))
			return
		}
		jobs[i] = r.job
	}
	saved, err := json.Marshal(c)
	if err != nil {
		w.showError(uiError("save settings: %v", err))
		return
	}
	w.app.Preferences().SetString("desktop-settings", string(saved))
	ctx, cancel := context.WithCancel(context.Background())
	w.active, w.cancel = true, cancel
	w.start.Disable()
	w.cancelButton.Enable()
	w.say(w.status, "Checking required subtitle fonts...")
	w.preflightFonts(ctx, cancel, jobs, opt, workers, c)
}

func (w *window) startConversions(ctx context.Context, cancel context.CancelFunc, jobs []batch.Job, opt convert.Options, workers int, c Config) {
	if ctx.Err() != nil {
		cancel()
		w.finish(true)
		return
	}
	events, err := batch.Run(ctx, jobs, opt, workers, true)
	if err != nil {
		cancel()
		w.showError(err)
		w.finish(false)
		return
	}
	w.say(w.status, "Converting with snapshotted settings...")
	w.appendLog(fmt.Sprintf("START: %d files; %s; %s fps; %s; concurrency %d", len(jobs), c.Size, c.FPS, c.Profile, workers))
	if opt.Profile.HDR {
		w.appendLog("WARNING: experimental BT.2020/PQ; SUP has no portable HDR signaling. Preview is SDR style only.")
		if opt.Profile.White > 300 {
			w.appendLog("WARNING: high subtitle reference white may be uncomfortable on HDR displays.")
		}
	} else if opt.Height <= 576 {
		w.appendLog("WARNING: SDR uses BT.709; some players assume BT.601 at or below 576 lines.")
	}
	go func() {
		for event := range events {
			fyne.Do(func() { w.apply(event) })
		}
		cancelled := ctx.Err() != nil
		cancel()
		fyne.Do(func() { w.finish(cancelled) })
	}()
}

func (w *window) apply(event batch.Event) {
	r := &w.rows[event.Index]
	switch {
	case event.Started:
		r.state = msg("Converting")
	case event.Progress != nil:
		p := event.Progress
		if p.Total > 0 {
			r.progress = min(0.999, float64(p.Processed)/float64(p.Total))
		}
		r.state = msg("%.1f%% (%d display sets)", r.progress*100, p.DisplaySets)
		if p.Processed == p.Total {
			r.state = msg("Finalizing output...")
		}
	default:
		r.done = true
		for _, warning := range event.Result.Warnings {
			w.appendLog("WARNING: " + event.Input + ": " + warning)
		}
		if event.Err != nil {
			r.state = msg("Failed: %s", event.Err.Error())
			if errors.Is(event.Err, context.Canceled) {
				r.state = msg("Cancelled")
				r.cancelled = true
			}
			w.appendLog(r.state.String() + ": " + event.Input)
		} else {
			r.progress = 1
			r.state = msg("Saved (%d display sets)", event.Result.Frames)
			w.appendLog("OK: " + event.Input + " -> " + event.Output)
		}
	}
	var total float64
	for _, r := range w.rows {
		total += r.progress
	}
	w.progress.SetValue(total / float64(len(w.rows)))
	w.list.Refresh()
}

func (w *window) finish(cancelled bool) {
	w.active, w.cancel = false, nil
	w.start.Enable()
	w.cancelButton.Disable()
	saved, failed, stopped := 0, 0, 0
	for i := range w.rows {
		r := &w.rows[i]
		if !r.done {
			r.state = msg("Not processed")
			if cancelled {
				r.state = msg("Cancelled before start")
			}
			stopped++
		} else if r.progress == 1 {
			saved++
		} else if r.cancelled {
			stopped++
		} else {
			failed++
		}
	}
	w.list.Refresh()
	summary := msg("Finished: %d saved, %d failed, %d cancelled/not processed.", saved, failed, stopped)
	w.say(w.status, summary.key, summary.args...)
	w.appendLog(summary.String())
	if w.closing {
		if w.previewCancel != nil {
			w.previewCancel()
		}
		if w.pickerCancel != nil {
			w.pickerCancel()
		}
		if w.fontCancel != nil {
			w.fontCancel()
		}
		if w.styleTimer != nil {
			w.styleTimer.Stop()
		}
		w.previewID++
		w.win.SetCloseIntercept(nil)
		w.win.Close()
	}
}

func (w *window) preview(first bool) {
	if w.selected < 0 || w.selected >= len(w.rows) {
		w.showError(uiError("select a subtitle file first"))
		return
	}
	opt, _, err := w.config().Options()
	if err != nil {
		w.showError(err)
		return
	}
	ms, err := strconv.ParseInt(strings.TrimSpace(w.timestamp.Text), 10, 64)
	if err != nil && !first {
		w.showError(uiError("preview time must be integer milliseconds"))
		return
	}
	if first {
		ms = 0
	}
	path := w.rows[w.selected].job.Input
	if w.previewCancel != nil {
		w.previewCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.previewCancel = cancel
	w.previewID++
	id := w.previewID
	w.say(w.previewStatus, "Rendering SDR style preview...")
	go func() {
		defer cancel()
		select {
		case w.previewGate <- struct{}{}:
			defer func() { <-w.previewGate }()
		case <-ctx.Done():
			return
		}
		result, err := convert.Preview(ctx, path, opt, ms)
		if first && err == nil && len(result.Intervals) > 0 {
			ms = result.Intervals[0].Start
			for _, interval := range result.Intervals {
				ms = min(ms, interval.Start)
			}
			result, err = convert.Preview(ctx, path, opt, ms)
		}
		fyne.Do(func() {
			if id != w.previewID {
				return
			}
			if err != nil {
				w.say(w.previewStatus, "Preview failed; see log.")
				w.showError(err)
				return
			}
			w.intervals = result.Intervals
			w.timestamp.SetText(strconv.FormatInt(ms, 10))
			w.previewImage.Image = result.Image
			w.previewImage.Refresh()
			w.say(w.previewStatus, "%s at %d ms | SDR style preview; no SUP quantization or HDR luminance simulation.", filepath.Base(path), ms)
			for _, warning := range result.Warnings {
				w.appendLog("PREVIEW WARNING: " + warning)
			}
		})
	}()
}

func (w *window) navigate(direction int) {
	current, err := strconv.ParseInt(strings.TrimSpace(w.timestamp.Text), 10, 64)
	if err != nil {
		w.showError(uiError("preview time must be integer milliseconds"))
		return
	}
	target := int64(-1)
	for _, interval := range w.intervals {
		if direction < 0 && interval.Start < current && interval.Start > target {
			target = interval.Start
		}
		if direction > 0 && interval.Start > current && (target < 0 || interval.Start < target) {
			target = interval.Start
		}
	}
	if target < 0 {
		w.say(w.previewStatus, "No matching subtitle boundary. Use First subtitle to load this file.")
		return
	}
	w.timestamp.SetText(strconv.FormatInt(target, 10))
	w.preview(false)
}
