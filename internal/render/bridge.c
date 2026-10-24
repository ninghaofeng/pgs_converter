#include "bridge.h"
#include <stdarg.h>
#include <stdio.h>
#include <string.h>

/*
 * libass uses smaller levels for more severe messages. Preserve warnings and
 * selected font messages even when their native severity is only informational;
 * they can explain missing visible text and are relevant to Go's strict mode.
 */
static void message(int level, const char *format, va_list args, void *data) {
    pgs_renderer *r = data;
    char line[4096];
    vsnprintf(line, sizeof(line), format, args);
    if (level > 2 && !strstr(line, "Glyph ") && !strstr(line, "fontselect: failed"))
        return;
    size_t left = sizeof(r->diagnostics) - r->used;
    int n = snprintf(r->diagnostics + r->used, left, "%s\n", line);
    if (n < 0 || (size_t)n >= left) {
        /* snprintf preserves termination; leave used within the buffer bounds. */
        r->truncated = 1;
        return;
    }
    r->used += (size_t)n;
}

pgs_renderer *pgs_new(int width, int height) {
    pgs_renderer *r = calloc(1, sizeof(*r));
    if (!r) return NULL;
    r->library = ass_library_init();
    if (!r->library) { pgs_free(r); return NULL; }
    ass_set_message_cb(r->library, message, r);
    /* Honor legacy ASS embedded fonts in addition to caller-supplied files. */
    ass_set_extract_fonts(r->library, 1);
    r->renderer = ass_renderer_init(r->library);
    if (!r->renderer) { pgs_free(r); return NULL; }
    ass_set_frame_size(r->renderer, width, height);
    /* The CLI supplies a square-pixel canvas, not an inferred source video SAR. */
    ass_set_storage_size(r->renderer, width, height);
    ass_set_pixel_aspect(r->renderer, 1.0);
    return r;
}

void pgs_free(pgs_renderer *r) {
    if (!r) return;
    if (r->track) ass_free_track(r->track);
    if (r->renderer) ass_renderer_done(r->renderer);
    if (r->library) ass_library_done(r->library);
    free(r);
}

int pgs_load(pgs_renderer *r, char *text, size_t length) {
    r->track = ass_read_memory(r->library, text, length, "UTF-8");
    return r->track != NULL;
}

void pgs_clear_diagnostics(pgs_renderer *r) {
    r->used = 0;
    r->diagnostics[0] = 0;
    r->truncated = 0;
}
