package convert

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

func fontFixture(t *testing.T, extension, family, text string) string {
	t.Helper()
	source := "[Script Info]\nScriptType: v4.00+\nPlayResX: 640\nPlayResY: 360\n" +
		"[V4+ Styles]\nFormat: Name,Fontname,Fontsize,PrimaryColour,SecondaryColour,OutlineColour,BackColour,Bold,Italic,Underline,StrikeOut,ScaleX,ScaleY,Spacing,Angle,BorderStyle,Outline,Shadow,Alignment,MarginL,MarginR,MarginV,Encoding\n" +
		"Style: Default," + family + ",28,&H00FFFFFF,&H00000000,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1,0,2,20,20,20,1\n" +
		"Style: Unused,PGS-Unused-Missing,28,&H00FFFFFF,&H00000000,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1,0,2,20,20,20,1\n" +
		"[Events]\nFormat: Layer,Start,End,Style,Name,MarginL,MarginR,MarginV,Effect,Text\n" +
		"Dialogue: 0,0:00:00.50,0:00:01.00,Default,,0,0,0,," + text + "\n"
	path := filepath.Join(t.TempDir(), "subtitle."+extension)
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRequiredFontPreflightAndStrictOutput(t *testing.T) {
	for _, ext := range []string{"ass", "ssa"} {
		t.Run(ext, func(t *testing.T) {
			path := fontFixture(t, ext, "PGS-Missing-Base-987", `{\fnPGS-Missing-Inline-987}Hello{\rUnused}sign`)
			report, err := CheckFonts(context.Background(), path, options())
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"PGS-Missing-Base-987", "PGS-Missing-Inline-987", "PGS-Unused-Missing"} {
				if !slices.Contains(report.Missing, name) {
					t.Fatalf("required style, inline or reset font was not detected: %v", report.Missing)
				}
			}
			out := filepath.Join(t.TempDir(), "strict.sup")
			opt := options()
			opt.Strict = true
			opt.Progress = func(Progress) { t.Error("strict font preflight started rendering frames") }
			result, err := File(context.Background(), path, out, opt)
			if err == nil || len(result.Warnings) == 0 || !strings.Contains(err.Error(), "strict mode") {
				t.Fatalf("strict mode accepted missing fonts: %+v, %v", result, err)
			}
			if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("strict font preflight created a final output")
			}
			original := []byte("existing output")
			if err := os.WriteFile(out, original, 0600); err != nil {
				t.Fatal(err)
			}
			opt.Overwrite = true
			if _, err := File(context.Background(), path, out, opt); err == nil {
				t.Fatal("strict overwrite accepted missing fonts")
			}
			preserved, err := os.ReadFile(out)
			if err != nil || !bytes.Equal(preserved, original) {
				t.Fatal("strict failure changed an existing output")
			}
			entries, err := os.ReadDir(filepath.Dir(out))
			if err != nil || len(entries) != 1 {
				t.Fatal("strict font failure leaked temporary outputs")
			}
			var data bytes.Buffer
			result, err = Stream(context.Background(), path, &data, options())
			if err != nil || data.Len() == 0 || len(result.Warnings) == 0 {
				t.Fatalf("normal mode did not warn and allow font fallback: %+v, %v", result, err)
			}
		})
	}
}

func TestAvailableExtraAndEmbeddedFonts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "different-file-name.ttf"), goregular.TTF, 0600); err != nil {
		t.Fatal(err)
	}
	path := fontFixture(t, "ass", "Go", `{\fn@GoRegular}Hello`)
	opt := options()
	opt.Fonts = []string{dir}
	report, err := CheckFonts(context.Background(), path, opt)
	if err != nil || len(report.Missing) != 0 {
		t.Fatalf("extra directory's family/full/PostScript alias was not resolved: %+v %v", report, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	alphabet := make([]byte, 64)
	for i := range alphabet {
		alphabet[i] = byte(33 + i)
	}
	encoded := base64.NewEncoding(string(alphabet)).WithPadding(base64.NoPadding).EncodeToString(goregular.TTF)
	var embedded strings.Builder
	embedded.WriteString("\n[Fonts]\nfontname: unrelated-name.ttf\n")
	for len(encoded) > 0 {
		n := min(80, len(encoded))
		embedded.WriteString(encoded[:n] + "\n")
		encoded = encoded[n:]
	}
	if err := os.WriteFile(path, append(data, []byte(embedded.String())...), 0600); err != nil {
		t.Fatal(err)
	}
	report, err = CheckFonts(context.Background(), path, options())
	if err != nil || len(report.Missing) != 0 {
		t.Fatalf("embedded font metadata was not recognized: %+v %v", report, err)
	}
}

func TestFontPreflightCancellationAndUnusedStyles(t *testing.T) {
	if report, err := CheckFonts(context.Background(), "../../testdata/sample.srt", options()); err != nil || len(report.Diagnostics()) != 0 {
		t.Fatalf("ASS/SSA preflight changed SRT behavior: %+v %v", report, err)
	}
	path := fontFixture(t, "ass", "sans-serif", "Hello")
	report, err := CheckFonts(context.Background(), path, options())
	if err != nil || len(report.Missing) != 0 {
		t.Fatalf("unused missing style triggered preflight: %+v %v", report, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CheckFonts(ctx, path, options()); !errors.Is(err, context.Canceled) {
		t.Fatalf("preflight ignored cancellation: %v", err)
	}
}
