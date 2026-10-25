package convert

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ninghaofeng/pgs_converter/internal/fontinfo"
	"github.com/ninghaofeng/pgs_converter/internal/render"
)

// FontReport separates absent font names from inventory/load diagnostics so the
// GUI can request fallback consent without treating every scan warning as absence.
// A clean report does not prove glyph coverage or the provider's exact face choice.
type FontReport struct {
	Missing  []string
	Warnings []string
}

// Diagnostics returns a new warning list, including one warning per missing name.
func (report FontReport) Diagnostics() []string {
	warnings := append([]string(nil), report.Warnings...)
	for _, name := range report.Missing {
		warnings = append(warnings, fmt.Sprintf("required font %q was not found in system, extra or embedded fonts; libass may substitute another font", name))
	}
	return warnings
}

func checkTrackFonts(ctx context.Context, text string, r *render.Renderer, extra []string) (FontReport, error) {
	var report FontReport
	required := r.RequiredFonts()
	needsCatalog := false
	for _, name := range required {
		needsCatalog = needsCatalog || !fontinfo.Generic(name)
	}
	if !needsCatalog {
		return report, ctx.Err()
	}
	embedded, warnings := fontinfo.EmbeddedFonts(text)
	// Embedded fonts can satisfy the entire track without a system scan.
	// Match their internal names, not the arbitrary ASS attachment filenames.
	var embeddedCatalog fontinfo.Catalog
	for _, font := range embedded {
		embeddedCatalog.AddEmbedded(font.Name, font.Data)
	}
	report.Warnings = append(embeddedCatalog.Warnings, warnings...)
	var external []string
	for _, name := range required {
		if !embeddedCatalog.Has(name) {
			external = append(external, name)
		}
	}
	if len(external) == 0 {
		return report, ctx.Err()
	}
	catalog, err := fontinfo.System(ctx, extra)
	if err != nil {
		return report, fmt.Errorf("font preflight: %w", err)
	}
	report.Warnings = append(report.Warnings, catalog.Warnings...)
	for _, name := range external {
		if !catalog.Has(name) {
			report.Missing = append(report.Missing, name)
		}
	}
	return report, ctx.Err()
}

// CheckFonts preflights ASS/SSA without rendering frames or creating output.
// Strict policy is applied by the caller so the GUI can explain missing fonts.
// Other extensions are a no-op; callers still observe an already-cancelled context.
func CheckFonts(ctx context.Context, path string, opt Options) (FontReport, error) {
	var report FontReport
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".ass" && ext != ".ssa" {
		return report, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if err := opt.Validate(); err != nil {
		return report, err
	}
	text, _, err := readSource(path, opt)
	if err != nil {
		return report, err
	}
	r, err := render.New(opt.Width, opt.Height, opt.Fonts)
	if err != nil {
		return report, err
	}
	defer r.Close()
	if _, err := r.Load(text); err != nil {
		return report, err
	}
	report, err = checkTrackFonts(ctx, text, r, opt.Fonts)
	report.Warnings = append(report.Warnings, r.Diagnostics()...)
	return report, err
}
