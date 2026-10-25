package fontinfo

import (
	"encoding/binary"
	"testing"
	"unicode/utf16"

	"golang.org/x/image/font/gofont/goregular"
)

func TestCatalogEmbeddedAndAliases(t *testing.T) {
	var c Catalog
	c.AddEmbedded("not-the-family.ttf", goregular.TTF)
	for _, name := range []string{"Go", "GO", "@Go", "GoRegular", "sans-serif", "serif", "monospace"} {
		if !c.Has(name) {
			t.Errorf("catalog missed available family or alias %q; indexed names: %v", name, c.names)
		}
	}
	if c.Has("Definitely Missing PGS Font 123456") {
		t.Fatal("catalog accepted an unavailable font")
	}
	before := len(c.Warnings)
	c.AddEmbedded("invalid.ttf", []byte("not a font"))
	if len(c.Warnings) != before+1 {
		t.Fatal("invalid embedded font was silently ignored")
	}
}

func TestLocalizedNameAliases(t *testing.T) {
	name := utf16.Encode([]rune("测试字体"))
	raw := make([]byte, 18+len(name)*2)
	binary.BigEndian.PutUint16(raw[2:], 1)
	binary.BigEndian.PutUint16(raw[4:], 18)
	binary.BigEndian.PutUint16(raw[6:], 3)
	binary.BigEndian.PutUint16(raw[8:], 1)
	binary.BigEndian.PutUint16(raw[10:], 0x0804)
	binary.BigEndian.PutUint16(raw[12:], 1)
	binary.BigEndian.PutUint16(raw[14:], uint16(len(name)*2))
	for i, unit := range name {
		binary.BigEndian.PutUint16(raw[18+i*2:], unit)
	}
	c := Catalog{names: make(map[string]bool)}
	if err := c.addNames(raw); err != nil || !c.Has("测试字体") {
		t.Fatalf("localized family alias was not detected: %v", err)
	}
	if err := c.addNames(raw[:10]); err == nil {
		t.Fatal("invalid font name table was silently accepted")
	}
}
