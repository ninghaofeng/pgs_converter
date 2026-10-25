package render

import (
	"bytes"
	"slices"
	"testing"

	"github.com/ninghaofeng/pgs_converter/internal/subtitle"
)

func TestLiteralTextAndMillisecondEvents(t *testing.T) {
	// An ASS-looking position override must stay visible as ordinary SRT text.
	// Times deliberately do not align to ASS's native centisecond precision.
	doc, err := subtitle.SRT("1\n00:00:00,001 --> 00:00:01,123\n{\\pos(1,1)} \\N", 1280, 720, subtitle.Style{Font: "sans-serif", Size: 36, Margin: 30})
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(1280, 720, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Load(doc.Text); err != nil {
		t.Fatal(err)
	}
	if err := r.SetTimes(doc.Times); err != nil {
		t.Fatal(err)
	}
	before, _, err := r.Frame(0)
	if err != nil || before != nil {
		t.Fatal("event began before its original millisecond timestamp")
	}
	visible, _, err := r.Frame(1)
	if err != nil || visible == nil || visible.Rect.Min.Y < 500 || visible.Rect.Dx() < 100 {
		t.Fatalf("literal text interpreted as ASS commands: %v %v", visible, err)
	}
	end, _, err := r.Frame(1123)
	if err != nil || end != nil {
		t.Fatal("event did not clear at original timestamp")
	}
}

func TestLiteralBackslash(t *testing.T) {
	doc, err := subtitle.SRT("1\n00:00:00,000 --> 00:00:01,000\nHello", 640, 360, subtitle.Style{Font: "sans-serif", Size: 28, Margin: 20})
	if err != nil {
		t.Fatal(err)
	}

	// Verify literal backslash-N is a single line, not an injected line break.
	var pix [][]byte
	for _, content := range []string{`Hello\{}NWorld`, `Hello\NWorld`} {
		r, err := New(640, 360, nil)
		if err != nil {
			t.Fatal(err)
		}
		script := bytes.ReplaceAll([]byte(doc.Text), []byte(",,Hello\n"), []byte(",,"+content+"\n"))
		if _, err := r.Load(string(script)); err != nil {
			r.Close()
			t.Fatal(err)
		}
		im, _, err := r.Frame(0)
		if err != nil || im == nil {
			r.Close()
			t.Fatalf("render: %v", err)
		}
		pix = append(pix, im.Pix)
		r.Close()
	}
	if bytes.Equal(pix[0], pix[1]) {
		t.Fatal("escaped literal backslash-N became a line break")
	}
}

func TestSSALoad(t *testing.T) {
	// SSA uses V4 styles, decimal colors and Marked rather than ASS's Layer.
	// Keep this dialect fixture separate from the generated ASS test scripts.
	const script = `[Script Info]
ScriptType: v4.00
PlayResX: 640
PlayResY: 360

[V4 Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, TertiaryColour, BackColour, Bold, Italic, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, AlphaLevel, Encoding
Style: Default,sans-serif,28,16777215,65535,0,0,0,0,1,2,1,2,20,20,20,0,1

[Events]
Format: Marked, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: Marked=0,0:00:00.50,0:00:01.50,Default,,0,0,0,,SSA subtitle
`
	r, err := New(640, 360, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	events, err := r.Load(script)
	if err != nil || len(events) != 1 || events[0].Start != 500 || events[0].End != 1500 {
		t.Fatalf("SSA events: %v %v", events, err)
	}
	if fonts := r.RequiredFonts(); !slices.Equal(fonts, []string{"sans-serif"}) {
		t.Fatalf("SSA required fonts: %v", fonts)
	}
	im, _, err := r.Frame(1000)
	if err != nil || im == nil {
		t.Fatalf("SSA render failed: %v", err)
	}

}

func TestRequiredFontsUseParsedStyleFormat(t *testing.T) {
	const script = `[Script Info]
ScriptType: v4.00+
PlayResX: 640
PlayResY: 360
[V4+ Styles]
Format: Fontname, Fontsize, Name
Style: Base Font,28,Default
Style: Sign Font,28,Signs
Style: Unused Font,28,Unused
[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Comment: 0,0:00:00.50,0:00:01.50,Unused,,0,0,0,,{\fnComment Font}comment
Dialogue: 0,0:00:00.50,0:00:01.50,Default,,0,0,0,,{\fnInline Font}Hello{\rSigns}Sign
`
	r, err := New(640, 360, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Load(script); err != nil {
		t.Fatal(err)
	}
	want := []string{"Base Font", "Inline Font", "Sign Font"}
	if got := r.RequiredFonts(); !slices.Equal(got, want) {
		t.Fatalf("parsed font references: %v, want %v", got, want)
	}
}
