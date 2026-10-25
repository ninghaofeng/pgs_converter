package desktop

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/ninghaofeng/pgs_converter/internal/batch"
	"github.com/ninghaofeng/pgs_converter/internal/convert"
	"github.com/ninghaofeng/pgs_converter/internal/subtitle"
)

func TestSRTFontStylePreviewUsesExportOptions(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string][]byte{"regular.ttf": goregular.TTF, "mono.ttf": gomono.TTF} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	input := filepath.Join(dir, "sample.srt")
	if err := os.WriteFile(input, []byte("1\n00:00:00,503 --> 00:00:01,507\nHello PGS\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config := Defaults()
	config.Size, config.Fonts, config.Font, config.FontSize, config.Margin = "1280x720", dir, "Go", "40", "30"
	opt, _, err := config.Options()
	if err != nil {
		t.Fatal(err)
	}
	base, err := convert.Preview(context.Background(), input, opt, 503)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*Config)
	}{
		{"font family", func(c *Config) { c.Font = "Go Mono" }},
		{"font size", func(c *Config) { c.FontSize = "80" }},
		{"margin", func(c *Config) { c.Margin = "100" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := config
			tc.edit(&c)
			opt, _, err := c.Options()
			if err != nil {
				t.Fatal(err)
			}
			result, err := convert.Preview(context.Background(), input, opt, 503)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(base.Image.Pix, result.Image.Pix) || !reflect.DeepEqual(base.Intervals, result.Intervals) {
				t.Fatal("style change did not alter rendered pixels or changed subtitle timing")
			}
		})
	}
}

func TestFontCatalogAndSearch(t *testing.T) {
	system, extra := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(system, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(extra, "not-loaded-by-libass"), 0700); err != nil {
		t.Fatal(err)
	}
	// A one-face TTC with absolute table offsets exercises collection metadata.
	collection := make([]byte, 16+len(gomono.TTF))
	copy(collection, "ttcf")
	binary.BigEndian.PutUint32(collection[4:], 0x00010000)
	binary.BigEndian.PutUint32(collection[8:], 1)
	binary.BigEndian.PutUint32(collection[12:], 16)
	copy(collection[16:], gomono.TTF)
	for i := 0; i < int(binary.BigEndian.Uint16(gomono.TTF[4:])); i++ {
		offset := 16 + 12 + i*16 + 8
		binary.BigEndian.PutUint32(collection[offset:], binary.BigEndian.Uint32(collection[offset:])+16)
	}
	for path, data := range map[string][]byte{
		filepath.Join(system, "nested", "renamed.TTF"):             goregular.TTF,
		filepath.Join(extra, "duplicate.ttf"):                      goregular.TTF,
		filepath.Join(extra, "collection.TTC"):                     collection,
		filepath.Join(extra, "broken.otf"):                         []byte("not a font"),
		filepath.Join(extra, "ignore.txt"):                         []byte("not a font"),
		filepath.Join(extra, "not-loaded-by-libass", "broken.ttf"): []byte("not a font"),
	} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	families, warnings, err := scanFontFamilies(context.Background(), []string{system}, []string{extra})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(families, []string{"Go", "Go Mono", "sans-serif"}) {
		t.Fatalf("families were not extracted, sorted and deduplicated: %q", families)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "broken.otf") {
		t.Fatalf("invalid fonts were hidden or extra directories scanned recursively: %q", warnings)
	}
	if got := filterFontFamilies(families, "  MONO "); !reflect.DeepEqual(got, []string{"Go Mono"}) {
		t.Fatalf("case-insensitive search returned %q", got)
	}
	if got := filterFontFamilies([]string{"思源黑体", "Arial"}, "黑体"); !slices.Equal(got, []string{"思源黑体"}) {
		t.Fatalf("Unicode font search failed: %q", got)
	}
	if got := filterFontFamilies(families, "no-match"); len(got) != 0 {
		t.Fatalf("unexpected search results: %q", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := scanFontFamilies(ctx, nil, []string{extra}); !errors.Is(err, context.Canceled) {
		t.Fatalf("font scan ignored cancellation: %v", err)
	}
	if _, _, err := scanFontFamilies(context.Background(), nil, []string{filepath.Join(extra, "missing")}); err == nil {
		t.Fatal("missing extra font directory was silently ignored")
	}
}

func TestStyleChangesScheduleSRTPreview(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := &window{app: a, win: a.NewWindow("test"), selected: -1}
	w.build(Defaults())
	w.font.SetText("Go")
	if w.styleTimer != nil {
		t.Fatal("style editing without a selection scheduled a preview")
	}
	w.rows = []row{{job: batch.Job{Input: "original.ass"}, state: msg("Waiting")}}
	w.selected = 0
	w.font.SetText("Go Mono")
	if w.styleTimer != nil {
		t.Fatal("SRT styling scheduled an ASS style override")
	}
	w.rows[0].job.Input = "selected.SRT"
	w.active = true
	w.intervals = []subtitle.Interval{{Start: 503, End: 1507}}
	w.timestamp.SetText("503")
	w.font.SetText("Go")
	first := w.styleTimer
	if first == nil {
		t.Fatal("font change did not schedule SRT preview")
	}
	first.Stop()
	id := w.previewID
	w.fontSize.SetText("60")
	if w.styleTimer == first || w.previewID <= id {
		t.Fatal("font size did not debounce and invalidate earlier previews")
	}
	w.styleTimer.Stop()
	w.margin.SetText("72")
	w.styleTimer.Stop()
	if !w.active || w.timestamp.Text != "503" || len(w.intervals) != 1 {
		t.Fatal("style preview reset active conversion or the selected preview time")
	}
	w.fontSize.SetText("invalid")
	w.previewID++
	if w.previewStatus.Text != "Complete valid settings to refresh the SRT style preview." || len(w.logLines) != 0 {
		t.Fatal("intermediate invalid settings did not show an inline preview explanation")
	}
}
