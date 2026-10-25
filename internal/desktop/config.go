// Package desktop implements the optional Fyne desktop interface.
package desktop

import (
	"strconv"
	"strings"

	"github.com/ninghaofeng/pgs_converter/internal/colorx"
	"github.com/ninghaofeng/pgs_converter/internal/convert"
	"github.com/ninghaofeng/pgs_converter/internal/subtitle"
)

// Config stores editable field text, including blank auto-size values, so saving
// preferences does not replace the user's input with derived engine defaults.
type Config struct {
	Size, FPS, Profile, White, Encoding string
	Font, FontSize, Margin, Fonts       string
	Output, Jobs                        string
	Strict, Overwrite, Recursive        bool
}

// Defaults supplies first-launch values; language and UI scale have separate preferences.
func Defaults() Config {
	return Config{Size: "1920x1080", FPS: "24000/1001", Profile: "sdr", White: "203",
		Encoding: "auto", Font: "sans-serif", Jobs: "1", Recursive: true}
}

// Options validates a value snapshot and resolves automatic SRT sizes against the
// output height. The returned worker count belongs to the batch scheduler.
func (c Config) Options() (convert.Options, int, error) {
	var opt convert.Options
	dimensions := strings.Split(strings.TrimSpace(c.Size), "x")
	if len(dimensions) != 2 {
		return opt, 0, uiError("resolution must be WIDTHxHEIGHT")
	}
	var err error
	opt.Width, err = strconv.Atoi(dimensions[0])
	if err != nil {
		return opt, 0, uiError("invalid width: %v", err)
	}
	opt.Height, err = strconv.Atoi(dimensions[1])
	if err != nil {
		return opt, 0, uiError("invalid height: %v", err)
	}
	opt.Rate, err = subtitle.ParseRate(strings.TrimSpace(c.FPS))
	if err != nil {
		return opt, 0, uiError("Invalid frame rate; use a supported fraction such as 24000/1001.\n%s", err)
	}
	white := 203.0
	if c.Profile == "hdr10" {
		white, err = strconv.ParseFloat(strings.TrimSpace(c.White), 64)
		if err != nil {
			return opt, 0, uiError("invalid HDR white: %v", err)
		}
	}
	opt.Profile, err = colorx.New(c.Profile, white)
	if err != nil {
		return opt, 0, uiError("Invalid color profile or HDR reference white.\n%s", err)
	}
	opt.Encoding = c.Encoding
	opt.Style = subtitle.Style{Font: strings.TrimSpace(c.Font), Size: max(1, opt.Height/24), Margin: max(1, opt.Height/24)}
	if strings.TrimSpace(c.FontSize) != "" {
		opt.Style.Size, err = strconv.Atoi(strings.TrimSpace(c.FontSize))
		if err != nil {
			return opt, 0, uiError("invalid SRT font size: %v", err)
		}
	}
	if strings.TrimSpace(c.Margin) != "" {
		opt.Style.Margin, err = strconv.Atoi(strings.TrimSpace(c.Margin))
		if err != nil {
			return opt, 0, uiError("invalid SRT margin: %v", err)
		}
	}
	if opt.Style.Font == "" || strings.ContainsAny(opt.Style.Font, ",\r\n") || opt.Style.Size <= 0 || opt.Style.Margin < 0 {
		return opt, 0, uiError("invalid SRT font, size or margin")
	}
	for _, dir := range strings.Split(c.Fonts, "\n") {
		if dir = strings.TrimSpace(dir); dir != "" {
			opt.Fonts = append(opt.Fonts, dir)
		}
	}
	opt.Strict, opt.Overwrite = c.Strict, c.Overwrite
	workers, err := strconv.Atoi(strings.TrimSpace(c.Jobs))
	if err != nil || workers < 1 || workers > 32 {
		return opt, 0, uiError("concurrency must be in 1..32")
	}
	if err := opt.Validate(); err != nil {
		return opt, 0, uiError("Invalid canvas, encoding or font folders.\n%s", err)
	}
	return opt, workers, nil
}
