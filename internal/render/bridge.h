#ifndef PGS_BRIDGE_H
#define PGS_BRIDGE_H
#include <ass/ass.h>
#include <stdlib.h>

/*
 * Native state is owned by one Go Renderer and accessed serially. Keeping
 * diagnostics in C avoids a variadic callback into Go and lets Go drain them
 * immediately after each native operation.
 */
typedef struct {
    ASS_Library *library;
    ASS_Renderer *renderer;
    ASS_Track *track;
    char diagnostics[65536]; /* NUL-terminated; used excludes the terminator. */
    size_t used;
    int truncated; /* Signals loss of messages rather than hiding buffer overflow. */
} pgs_renderer;

/* Returns fully initialized state or NULL; partial initialization is cleaned up. */
pgs_renderer *pgs_new(int width, int height);
/* Accepts NULL and releases track, renderer and library in dependency order. */
void pgs_free(pgs_renderer *r);
/* Parses a borrowed UTF-8 buffer and retains the resulting native track. */
int pgs_load(pgs_renderer *r, char *text, size_t length);
/* Starts a new diagnostic batch after Go has copied the previous messages. */
void pgs_clear_diagnostics(pgs_renderer *r);
#endif
