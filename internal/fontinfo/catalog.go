// Package fontinfo reads font metadata shared by selection and preflight.
package fontinfo

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
	"github.com/go-text/typesetting/fontscan"
)

// Catalog keeps display families separately from normalized lookup aliases.
// Its zero value accepts embedded fonts. Mutation is not concurrency-safe.
type Catalog struct {
	Families []string
	Warnings []string
	names    map[string]bool
}

// normalize treats ASS's leading @ as a vertical-font marker, not part of a name.
func normalize(name string) string {
	return font.NormalizeFamily(strings.TrimPrefix(strings.TrimSpace(name), "@"))
}

// Generic recognizes provider-selected families that need no named-font match.
// Accepting one does not establish that the provider covers every subtitle glyph.
func Generic(name string) bool {
	switch normalize(name) {
	case "sans-serif", "serif", "monospace":
		return true
	}
	return false
}

// Has accepts generic families or any indexed family, full-name or PostScript alias.
func (c *Catalog) Has(name string) bool { return Generic(name) || c.names[normalize(name)] }

func (c *Catalog) add(resource opentype.Resource, system bool) error {
	loaders, err := opentype.NewLoaders(resource)
	if err != nil {
		return err
	}
	for _, loader := range loaders {
		description, _ := font.Describe(loader, nil)
		family := strings.TrimSpace(description.Family)
		if family == "" {
			return fmt.Errorf("font has no family name")
		}
		if system && strings.HasPrefix(family, ".") {
			// CoreText's private dot-prefixed families are not reliable names
			// for selecting subtitle fonts through libass.
			continue
		}
		raw, err := loader.RawTable(opentype.MustNewTag("name"))
		if err != nil {
			return err
		}
		// Publish a face only after its name records pass bounds validation.
		aliases := Catalog{names: make(map[string]bool)}
		if err := aliases.addNames(raw); err != nil {
			return err
		}
		if c.names == nil {
			c.names = make(map[string]bool)
		}
		c.names[normalize(family)] = true
		for name := range aliases.names {
			c.names[name] = true
		}
		if !strings.ContainsAny(family, ",\r\n") {
			c.Families = append(c.Families, family)
		}
	}
	return nil
}

// addNames includes localized family, full and PostScript aliases, not just English names.
func (c *Catalog) addNames(raw []byte) error {
	if len(raw) < 6 {
		return fmt.Errorf("truncated font name table")
	}
	count, start := int(binary.BigEndian.Uint16(raw[2:])), int(binary.BigEndian.Uint16(raw[4:]))
	if len(raw) < 6+count*12 || start > len(raw) {
		return fmt.Errorf("invalid font name table")
	}
	for i := 0; i < count; i++ {
		record := raw[6+i*12 : 18+i*12]
		id := binary.BigEndian.Uint16(record[6:])
		// Legacy family, full, PostScript, typographic family and WWS family.
		// Keep all language records rather than only the preferred English name.
		if id != 1 && id != 4 && id != 6 && id != 16 && id != 21 {
			continue
		}
		length := int(binary.BigEndian.Uint16(record[8:]))
		offset := start + int(binary.BigEndian.Uint16(record[10:]))
		if offset > len(raw) || length > len(raw)-offset {
			return fmt.Errorf("invalid font name record")
		}
		data := raw[offset : offset+length]
		platform, encoding := binary.BigEndian.Uint16(record), binary.BigEndian.Uint16(record[2:])
		var name string
		switch {
		case platform == 0 || platform == 3 && (encoding == 0 || encoding == 1 || encoding == 10):
			if len(data)%2 != 0 {
				return fmt.Errorf("invalid UTF-16 font name")
			}
			units := make([]uint16, len(data)/2)
			for i := range units {
				units[i] = binary.BigEndian.Uint16(data[i*2:])
			}
			name = string(utf16.Decode(units))
		case platform == 1 && encoding == 0:
			name = tables.DecodeMacintosh(data)
		default:
			continue
		}
		if name = normalize(name); name != "" {
			c.names[name] = true
		}
	}
	return nil
}

// AddEmbedded indexes font bytes, including collection faces; name labels errors
// only. Invalid font metadata is retained as a warning instead of guessed by name.
func (c *Catalog) AddEmbedded(name string, data []byte) {
	if err := c.add(bytes.NewReader(data), false); err != nil {
		c.Warnings = append(c.Warnings, fmt.Sprintf("embedded font %q: %v", name, err))
	}
}

// Scan recursively inventories system roots and scans extra roots non-recursively,
// matching renderer font loading. Individual font/system-path failures are warnings;
// inaccessible extra roots and cancellation return an error and a partial catalog.
// Cancellation is checked between entries, not inside a font parser call.
func Scan(ctx context.Context, system, extra []string) (Catalog, error) {
	c := Catalog{Families: []string{"sans-serif"}, names: make(map[string]bool)}
	consume := func(path string, system bool) {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".ttf", ".otf", ".ttc":
		default:
			return
		}
		file, err := os.Open(path)
		if err == nil {
			err = c.add(file, system)
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
		if err != nil {
			c.Warnings = append(c.Warnings, fmt.Sprintf("%s: %v", path, err))
		}
	}
	for _, dir := range system {
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				c.Warnings = append(c.Warnings, fmt.Sprintf("%s: %v", path, err))
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !entry.IsDir() {
				consume(path, true)
			}
			return nil
		})
		if err != nil {
			return c, err
		}
	}
	for _, dir := range extra {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return c, fmt.Errorf("font directory %q: %w", dir, err)
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return c, err
			}
			if !entry.IsDir() {
				consume(filepath.Join(dir, entry.Name()), false)
			}
		}
	}
	slices.SortFunc(c.Families, func(a, b string) int { return strings.Compare(normalize(a), normalize(b)) })
	c.Families = slices.CompactFunc(c.Families, func(a, b string) bool { return normalize(a) == normalize(b) })
	return c, ctx.Err()
}

// Printf collects fontscan diagnostics without writing from a background scan.
func (c *Catalog) Printf(format string, args ...interface{}) {
	c.Warnings = append(c.Warnings, fmt.Sprintf(format, args...))
}

// System discovers conventional font directories, not the OS provider's full
// registry. Fonts installed elsewhere require an explicit extra directory.
func System(ctx context.Context, extra []string) (Catalog, error) {
	var log Catalog
	dirs, err := fontscan.DefaultFontDirectories(&log)
	if err != nil {
		return log, err
	}
	c, err := Scan(ctx, dirs, extra)
	c.Warnings = append(log.Warnings, c.Warnings...)
	return c, err
}
