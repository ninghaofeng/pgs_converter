package subtitle

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

func TestDecode(t *testing.T) {
	const want = "中文\nHello"
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"auto", []byte("\xef\xbb\xbf中文\r\nHello")},
		{"auto", mustEncode(t, unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().Bytes, want)},
		{"gb18030", mustEncode(t, simplifiedchinese.GB18030.NewEncoder().Bytes, want)},
	} {
		got, err := Decode(tc.data, tc.name)
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", tc.name, got, err)
		}
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{{"auto", []byte{0xff}}, {"auto", []byte{0xff, 0xfe, 0x00}}, {"big5", []byte{0xff}}, {"unknown", nil}, {"utf8", []byte{0}}} {
		if _, err := Decode(tc.data, tc.name); err == nil {
			t.Errorf("accepted malformed %s data %x", tc.name, tc.data)
		}
	}
}

func mustEncode(t *testing.T, encode func([]byte) ([]byte, error), s string) []byte {
	t.Helper()
	b, err := encode([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSRT(t *testing.T) {
	// Combine markup, entities, literal ASS syntax and sub-centisecond timing
	// so translation cannot accidentally reinterpret or round away the source.
	doc, err := SRT("1\n00:00:00,001 --> 00:00:01,123\n<b>Hello</b>\n{literal} \\N &amp; <font color=\"#ff0000\">red</font>", 1920, 1080, Style{"Arial", 45, 45})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Times) != 1 || doc.Times[0] != (Interval{1, 1123}) {
		t.Fatalf("lost millisecond timing: %v", doc.Times)
	}
	for _, fragment := range []string{`{\b1}Hello{\b0}`, `\{literal\}`, `\{}N`, `{\c&H0000ff&}`} {
		if !strings.Contains(doc.Text, fragment) {
			t.Errorf("missing %q in script", fragment)
		}
	}
	for _, text := range []string{"", "broken", "1\n00:00:02,000 --> 00:00:01,000\nx", "1\n00:00:00,000 --> 00:00:01,000\n<foo>x</foo>", "1\n00:00:00,000 --> 00:00:01,000\n<b>unclosed", "1\n00:00:00,000 --> 00:00:01,000\n<i>x</b>"} {
		if _, err := SRT(text, 1920, 1080, Style{"Arial", 45, 45}); err == nil {
			t.Errorf("accepted invalid SRT %q", text)
		}
		nested, err := srtText(`<b>a<b>b</b>c</b><font color="#ff0000">a<font color="#00ff00">b</font>c</font>`)
		if err != nil || !strings.Contains(nested, `{\b1}a{\b1}b{\b1}c{\b0}`) || !strings.Contains(nested, `b{\c&H0000ff&}c{\c}`) {
			t.Fatalf("nested style restoration: %s %v", nested, err)
		}
	}
}

func TestRate(t *testing.T) {
	r, err := ParseRate("24000/1001")
	if err != nil {
		t.Fatal(err)
	}
	if r.CeilFrame(1000) != 24 || r.Ticks(24000) != 90090000 {
		t.Fatal("inexact frame arithmetic")
	}
	if _, err := ParseRate("23.976"); err == nil {
		t.Fatal("accepted approximate fps")
	}
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte("hello"))
	f.Add([]byte{0xff, 0xfe})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Decode(data, "auto")
	})
}
