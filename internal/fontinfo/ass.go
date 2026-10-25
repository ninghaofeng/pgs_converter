package fontinfo

import (
	"fmt"
	"strings"
)

// InlineFonts finds font changes and named style resets inside override blocks.
func InlineFonts(text string, styles map[string]string) []string {
	var names []string
	for {
		start := strings.IndexByte(text, '{')
		if start < 0 {
			break
		}
		text = text[start+1:]
		end := strings.IndexByte(text, '}')
		if end < 0 {
			break
		}
		block := text[:end]
		text = text[end+1:]
		depth := 0
		for _, tag := range strings.Split(block, `\`)[1:] {
			var name string
			argument := tag
			for i := 0; i < depth && strings.HasSuffix(argument, ")"); i++ {
				argument = strings.TrimSuffix(argument, ")")
			}
			if strings.HasPrefix(tag, "fn") {
				name = strings.TrimSpace(argument[2:])
				if name == "0" {
					name = ""
				}
			} else if strings.HasPrefix(tag, "r") {
				style := strings.TrimSpace(argument[1:])
				name = styles[style]
			}
			if name != "" {
				names = append(names, name)
			}
			if depth > 0 || strings.HasPrefix(tag, "t(") {
				depth = max(0, depth+strings.Count(tag, "(")-strings.Count(tag, ")"))
			}
		}
	}
	return names
}

type EmbeddedFont struct {
	Name string
	Data []byte
}

func EmbeddedFonts(text string) ([]EmbeddedFont, []string) {
	var fonts []EmbeddedFont
	var warnings []string
	section, name := false, ""
	var encoded strings.Builder
	flush := func() {
		if name == "" {
			return
		}
		text := encoded.String()
		if len(text) == 0 || len(text)%4 == 1 {
			warnings = append(warnings, fmt.Sprintf("embedded font %q: invalid encoded data length", name))
		} else {
			data := make([]byte, 0, len(text)*3/4)
			for len(text) > 0 {
				n := min(4, len(text))
				var value uint32
				for i := 0; i < n; i++ {
					value |= uint32((text[i]-33)&63) << (6 * (3 - i))
				}
				data = append(data, byte(value>>16))
				if n >= 3 {
					data = append(data, byte(value>>8))
				}
				if n == 4 {
					data = append(data, byte(value))
				}
				text = text[n:]
			}
			fonts = append(fonts, EmbeddedFont{Name: name, Data: data})
		}
		name = ""
		encoded.Reset()
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimLeft(line, " \t")
		header := false
		for _, prefix := range []string{"[script info]", "[v4 styles]", "[v4+ styles]", "[events]", "[fonts]"} {
			header = header || strings.HasPrefix(strings.ToLower(line), prefix)
		}
		if header {
			flush()
			section = strings.HasPrefix(strings.ToLower(line), "[fonts]")
			continue
		}
		if !section {
			continue
		}
		if strings.HasPrefix(line, "fontname:") {
			flush()
			name = strings.TrimSpace(strings.TrimPrefix(line, "fontname:"))
		} else if name != "" {
			encoded.WriteString(line)
		}
	}
	flush()
	return fonts, warnings
}
