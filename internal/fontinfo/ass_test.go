package fontinfo

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

func TestInlineFonts(t *testing.T) {
	styles := map[string]string{"Signs": "Sign Font", "Default": "Base Font"}
	for _, tc := range []struct {
		text string
		want []string
	}{
		{`plain \fnNotATag`, nil},
		{`{\fnMissing Font}Hello{\fnOther}world`, []string{"Missing Font", "Other"}},
		{`{\rSigns}Sign{\r}reset{\fn0}reset{\fn}reset`, []string{"Sign Font"}},
		{`{\t(0,100,\fnAnimated Font)}text`, []string{"Animated Font"}},
		{`{\fnFont (Test)}text`, []string{"Font (Test)"}},
		{`{\t(0,100,\fnFont (Test))}text`, []string{"Font (Test)"}},
		{`{\fn@Vertical Family}text`, []string{"@Vertical Family"}},
		{`{\fnBroken`, nil},
	} {
		if got := InlineFonts(tc.text, styles); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: got %q, want %q", tc.text, got, tc.want)
		}
	}
}

func TestEmbeddedFontDecoding(t *testing.T) {
	alphabet := make([]byte, 64)
	for i := range alphabet {
		alphabet[i] = byte(33 + i)
	}
	encoding := base64.NewEncoding(string(alphabet)).WithPadding(base64.NoPadding)
	for _, data := range [][]byte{{1}, {1, 2}, {1, 2, 3}, {232, 0, 0}, []byte("embedded font with multiple lines")} {
		encoded := encoding.EncodeToString(data)
		var lines []string
		for len(encoded) > 0 {
			n := min(8, len(encoded))
			lines = append(lines, encoded[:n])
			encoded = encoded[n:]
		}
		text := "[Fonts]\r\nfontname: misleading-file-name.ttf\r\n" + strings.Join(lines, "\r\n") + "\r\n[Events]\r\n"
		fonts, warnings := EmbeddedFonts(text)
		if len(warnings) != 0 || len(fonts) != 1 || fonts[0].Name != "misleading-file-name.ttf" || !reflect.DeepEqual(fonts[0].Data, data) {
			t.Fatalf("embedded data %v: fonts=%v, warnings=%v", data, fonts, warnings)
		}
	}
	fonts, warnings := EmbeddedFonts("[Fonts]\nfontname: broken.ttf\n!\n")
	if len(fonts) != 0 || len(warnings) != 1 {
		t.Fatal("malformed embedded data was not reported")
	}
}
