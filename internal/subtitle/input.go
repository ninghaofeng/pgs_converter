// Package subtitle normalizes text encodings, parses supported frame rates,
// and translates SRT into an in-memory ASS script for libass.
// Event times remain integer milliseconds until sampling on the video grid.
package subtitle

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

// Decode returns UTF-8 text with LF line endings and no leading Unicode BOM.
// Auto mode recognizes only UTF-8 and BOM-marked UTF-16; legacy encodings
// require an explicit name. Embedded NULs are rejected before the cgo boundary.
func Decode(data []byte, name string) (string, error) {
	var enc encoding.Encoding
	switch strings.ToLower(name) {
	case "", "auto":
		switch {
		case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
			enc = unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM)
		case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
			enc = unicode.UTF16(unicode.BigEndian, unicode.ExpectBOM)
		default:
			data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
		}
	case "utf-8", "utf8":
		data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	case "utf-16le":
		enc = unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
		data = bytes.TrimPrefix(data, []byte{0xff, 0xfe})
	case "utf-16be":
		enc = unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)
		data = bytes.TrimPrefix(data, []byte{0xfe, 0xff})
	case "gb18030":
		enc = simplifiedchinese.GB18030
	case "big5":
		enc = traditionalchinese.Big5
	default:
		return "", fmt.Errorf("unsupported text encoding %q", name)
	}
	if enc != nil {
		decoded, err := enc.NewDecoder().Bytes(data)
		if err != nil {
			return "", fmt.Errorf("decode %s: %w", name, err)
		}
		// x/text may replace malformed input with U+FFFD without an error.
		// Reject replacement output rather than silently damaging subtitles;
		// this conservative rule also rejects a literal U+FFFD in these encodings.
		if bytes.Contains(decoded, []byte("\ufffd")) {
			return "", fmt.Errorf("invalid or unrepresentable sequence in %s input", name)
		}
		data = decoded
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return "", fmt.Errorf("invalid UTF-8 or embedded NUL; specify --encoding for legacy text")
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n"), nil
}

// Rate stores frames per second as Num/Den and its PGS frame-rate code.
// Construct it with ParseRate so the rational value and wire code agree.
type Rate struct {
	Num, Den int64
	Code     byte
}

// ParseRate accepts exact supported rates rather than rounded decimal aliases.
// Keeping fractional rates rational prevents cumulative drift on long tracks.
func ParseRate(s string) (Rate, error) {
	rates := map[string]Rate{
		"24000/1001": {24000, 1001, 0x10}, "24": {24, 1, 0x20},
		"25": {25, 1, 0x30}, "30000/1001": {30000, 1001, 0x40},
		"30": {30, 1, 0x50}, "50": {50, 1, 0x60},
		"60000/1001": {60000, 1001, 0x70}, "60": {60, 1, 0x80},
	}
	r, ok := rates[s]
	if !ok {
		return Rate{}, fmt.Errorf("unsupported fps %q; use 24000/1001, 24, 25, 30000/1001, 30, 50, 60000/1001 or 60", s)
	}
	return r, nil
}

// CeilFrame returns the first frame at or after a nonnegative millisecond time.
// Applying it to both ends maps [start, end) onto an exclusive-end frame span.
func (r Rate) CeilFrame(ms int64) int64 {
	return (ms*r.Num + 1000*r.Den - 1) / (1000 * r.Den)
}

// Milliseconds truncates a nonnegative frame time to whole milliseconds.
// Rendering uses an explicit ceiling instead when testing event boundaries.
func (r Rate) Milliseconds(frame int64) int64 { return frame * 1000 * r.Den / r.Num }

// Ticks rounds a nonnegative frame time to the nearest 90 kHz SUP clock tick.
// Each timestamp is derived from its frame number, not the previous timestamp.
func (r Rate) Ticks(frame int64) int64 { return (frame*90000*r.Den + r.Num/2) / r.Num }

// Style supplies SRT-only typography in output-canvas pixel coordinates.
// ASS and SSA inputs keep their own script styles and coordinate systems.
type Style struct {
	Font         string
	Size, Margin int
}

// Interval is an event's half-open [Start, End) interval in milliseconds.
type Interval struct{ Start, End int64 }

// Document pairs generated ASS with the original, millisecond-precision times.
// Times must be applied to libass events in script order before any rendering;
// ASS dialogue timestamps alone have only centisecond precision.
type Document struct {
	Text  string
	Times []Interval
}

var timing = regexp.MustCompile(`^(\d{2,}):([0-5]\d):([0-5]\d)[,.](\d{3})\s+-->\s+(\d{2,}):([0-5]\d):([0-5]\d)[,.](\d{3})$`)
var tag = regexp.MustCompile(`(?i)<[^>]*>`)
var fontColor = regexp.MustCompile(`(?i)^<font\s+color\s*=\s*["']?(#[0-9a-f]{6})["']?\s*>$`)

// srtText translates the supported HTML-like markup into ASS override blocks.
// A stack enforces balanced nesting and restores enclosing styles on closing.
func srtText(s string) (string, error) {
	var out strings.Builder
	type opened struct{ name, color string }
	var stack []opened
	pos := 0
	writeText := func(s string) error {
		s = html.UnescapeString(s)
		// libass supports escaped braces. An empty override block separates
		// a literal backslash from following N/n/h escape characters.
		// Decode entities only within text so &lt;b&gt; cannot become markup.
		s = strings.NewReplacer(`\`, `\{}`, `{`, `\{`, `}`, `\}`).Replace(s)
		out.WriteString(strings.ReplaceAll(s, "\n", `\N`))
		return nil
	}
	for _, loc := range tag.FindAllStringIndex(s, -1) {
		if err := writeText(s[pos:loc[0]]); err != nil {
			return "", err
		}
		t := s[loc[0]:loc[1]]
		lower := strings.ToLower(t)
		switch lower {
		case "<b>", "<i>", "<u>":
			name := lower[1:2]
			stack = append(stack, opened{name: name})
			fmt.Fprintf(&out, `{\%s1}`, name)
		case "</b>", "</i>", "</u>", "</font>":
			name := lower[2 : len(lower)-1]
			if len(stack) == 0 || stack[len(stack)-1].name != name {
				return "", fmt.Errorf("unbalanced SRT markup %q", t)
			}
			stack = stack[:len(stack)-1]
			restore := ""
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].name == name {
					restore = stack[i].color
					if name != "font" {
						restore = "1"
					}
					break
				}
			}
			if name == "font" {
				fmt.Fprintf(&out, `{\c%s}`, restore)
			} else {
				if restore == "" {
					restore = "0"
				}
				fmt.Fprintf(&out, `{\%s%s}`, name, restore)
			}
		case "<br>", "<br/>", "<br />":
			out.WriteString(`\N`)
		default:
			m := fontColor.FindStringSubmatch(t)
			if m == nil {
				return "", fmt.Errorf("unsupported SRT markup %q", t)
			}
			c := m[1][1:]
			// ASS color literals store BGR, unlike HTML's RGB notation.
			value := fmt.Sprintf("&H%s%s%s&", c[4:6], c[2:4], c[0:2])
			stack = append(stack, opened{"font", value})
			fmt.Fprintf(&out, `{\c%s}`, value)
		}
		pos = loc[1]
	}
	if err := writeText(s[pos:]); err != nil {
		return "", err
	}
	if len(stack) != 0 {
		return "", fmt.Errorf("unclosed SRT markup <%s>", stack[len(stack)-1].name)
	}
	return out.String(), nil
}

// SRT parses LF-normalized SRT and generates an ASS document for the canvas.
// Decode should normalize external input first. Unsupported markup and invalid
// event intervals are errors; callers must discard the document on failure.
func SRT(text string, width, height int, style Style) (Document, error) {
	var times []Interval
	script, err := srt(text, width, height, style, &times)
	return Document{Text: script, Times: times}, err
}

func srt(text string, width, height int, style Style, times *[]Interval) (string, error) {
	if style.Font == "" || strings.ContainsAny(style.Font, ",\r\n") || style.Size < 1 || style.Margin < 0 {
		return "", fmt.Errorf("invalid SRT font, size or margin")
	}
	var out strings.Builder
	// Generated colors are already intended as RGB graphics; Matrix: None
	// prevents the renderer from applying legacy ASS color mangling again.
	fmt.Fprintf(&out, "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\nYCbCr Matrix: None\nScaledBorderAndShadow: yes\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\nStyle: Default,%s,%d,&H00FFFFFF,&H00FFFFFF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,1,2,1,2,%d,%d,%d,1\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n",
		width, height, style.Font, style.Size, style.Margin, style.Margin, style.Margin)
	blocks := regexp.MustCompile(`\n[ \t]*\n`).Split(strings.TrimSpace(text), -1)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("empty SRT")
	}
	for index, block := range blocks {
		lines := strings.Split(block, "\n")
		if len(lines) < 3 {
			return "", fmt.Errorf("SRT block %d: expected index, timestamp and text", index+1)
		}
		if _, err := strconv.ParseUint(strings.TrimSpace(lines[0]), 10, 32); err != nil {
			return "", fmt.Errorf("SRT block %d: invalid index", index+1)
		}
		m := timing.FindStringSubmatch(strings.TrimSpace(lines[1]))
		if m == nil {
			return "", fmt.Errorf("SRT block %d: invalid timestamp", index+1)
		}
		parse := func(parts []string) (int64, error) {
			h, err := strconv.ParseInt(parts[0], 10, 32)
			if err != nil {
				return 0, err
			}
			// The timing regexp has already bounded these two-digit fields
			// and the three-digit millisecond field; only hours can overflow.
			minute, _ := strconv.ParseInt(parts[1], 10, 64)
			second, _ := strconv.ParseInt(parts[2], 10, 64)
			ms, _ := strconv.ParseInt(parts[3], 10, 64)
			return ((h*60+minute)*60+second)*1000 + ms, nil
		}
		start, err := parse(m[1:5])
		if err != nil {
			return "", fmt.Errorf("SRT block %d: %w", index+1, err)
		}
		end, err := parse(m[5:9])
		if err != nil || start >= end || end > 47721858 {
			return "", fmt.Errorf("SRT block %d: invalid or out-of-range interval", index+1)
		}
		*times = append(*times, Interval{start, end})
		content, err := srtText(strings.Join(lines[2:], "\n"))
		if err != nil {
			return "", fmt.Errorf("SRT block %d: %w", index+1, err)
		}
		stamp := func(ms int64) string {
			return fmt.Sprintf("%d:%02d:%02d.%02d", ms/3600000, ms/60000%60, ms/1000%60, ms/10%100)
		}
		// Temporary ASS times expand outward to preserve positive duration.
		// The caller restores Times on libass events before sampling frames.
		fmt.Fprintf(&out, "Dialogue: 0,%s,%s,Default,,0,0,0,,%s\n", stamp(start), stamp((end+9)/10*10), content)
	}
	return out.String(), nil
}
