# pgs-converter

English | [简体中文](./README.zh-CN.md)

Convert SRT, ASS and SSA text subtitles into PGS (`.sup`) bitmap subtitles.
The CLI and PGS encoder are written in Go; libass renders fonts, positioning,
layers, vector drawings, fades, movement, transforms and karaoke.

This is an initial implementation, not a Blu-ray authoring/compliance tool.
It does not encode video, perform OCR, or mux MKV files.

## Build

Requirements: Go 1.24+, a cgo-compatible C compiler, pkg-config and libass
development files. Development on macOS ARM64 has been verified against
libass 0.17.5. FFmpeg is optional at runtime and is used for independent tests.

### macOS

```sh
brew install libass pkgconf
go build -trimpath -o pgs-converter ./cmd/pgs-converter
./pgs-converter version
```

Use a Go installation and Homebrew dependencies matching the target
architecture. Setting `GOARCH` alone does not cross-compile C dependencies.

### Windows

Use native Go plus MSYS2's **UCRT64** toolchain for x64, or **CLANGARM64** for
ARM64. Install the matching compiler, libass and pkgconf packages. Add that
environment's `bin` directory to PATH, set `CGO_ENABLED=1`, select `CC=gcc`
(UCRT64) or `CC=clang` (CLANGARM64), and point `PKG_CONFIG_PATH` at its
`lib/pkgconfig` directory before running:

```powershell
go build -trimpath -o pgs-converter.exe ./cmd/pgs-converter
```

The four-target GitHub Actions workflow is configured to build and run tests on macOS
ARM64/x64 and Windows ARM64/x64, and prepares archives with shared native
libraries. **Only macOS ARM64 has been run locally; the other targets and their
packaging are not certified until their CI jobs and clean-machine checks pass.**
CI uses package-manager native dependencies and records installed versions;
native dependencies are not yet reproducibly source-pinned.

## Convert one file

Resolution and frame rate are mandatory. ASS `PlayRes` is a script coordinate
system, not a reliable video-resolution declaration.

```sh
pgs-converter convert movie.ass \
  --size 1920x1080 --fps 24000/1001 -o movie.sup

pgs-converter convert movie.srt \
  --size 1920x1080 --fps 24 \
  --font-dir ./fonts --font "Noto Sans CJK SC" \
  --font-size 45 --margin 45 -o movie.sup

pgs-converter convert legacy.ssa \
  --encoding gb18030 --size 1920x1080 --fps 25 -o legacy.sup
```

Supported frame rates: `24000/1001`, `24`, `25`, `30000/1001`, `30`, `50`,
`60000/1001`, `60`. Decimal approximations such as `23.976` are rejected.
Canvas dimensions must fit PGS's 16-bit fields and total at most 8,294,400
pixels (3840 x 2160). Acceptance of a size does not imply hardware support.

SRT defaults to the system `sans-serif` family, with font size and margins
equal to canvas height / 24 (minimum 1 pixel). `--font`, `--font-size` and
`--margin` affect SRT only; ASS/SSA retain their own styling.
SRT supports multiline text, overlapping events, HTML entities, nested
`b`/`i`/`u`, `font color="#RRGGBB"` and `br`. Unknown, unbalanced or unsupported
markup is an explicit conversion error, not silently stripped.
Literal braces and backslash escape sequences are protected during conversion
to libass's in-memory ASS script. Original SRT millisecond times are restored
on parsed events; they are not truncated to ASS centiseconds.

## Batch conversion

```sh
pgs-converter batch ./subtitles \
  --recursive --size 1920x1080 --fps 24000/1001 \
  --output-dir ./output --jobs 2

pgs-converter batch one.srt two.ass \
  --size 1920x1080 --fps 24 --output-dir ./output
```

- Directories are non-recursive unless `--recursive` is specified.
- Relative subdirectories are retained. Multiple roots use paths relative to
  each input root. Inputs mapping to the same output are all rejected before
  conversion, including case-only differences.
- A failed file does not stop the other files. Progress, warnings, mappings
  and a final summary are written to stderr.
- `--jobs` defaults to 1 and accepts 1..32. Each job owns its libass renderer.
  Higher concurrency increases native font-cache and image memory use.
- Existing files are protected unless `--overwrite` is given. Conversion writes
  to a temporary file beside the destination and commits only after success.
  No-overwrite commits use hard links: filesystems without hard-link support
  fail explicitly instead of risking replacement. Replacement uses the OS rename
  operation; atomic replacement is not guaranteed on every Windows filesystem.
- Ctrl+C stops scheduling, cancels active conversions between rendering/encoding
  operations, and cleans up temporary files. A single expensive native operation
  is not forcibly interrupted.

Exit codes: `0` success/help/version, `1` one or more conversion failures,
`2` invalid arguments/global configuration, `130` cancellation.

## Fonts and text encodings

System fonts are enabled. Repeat `--font-dir` to load additional TTF, OTF or
TTC files from directories (not recursively). No fonts are bundled; supply
appropriately licensed font files for consistent output on different systems.

libass warnings, unavailable glyph diagnostics and detectable fallback failures
are reported. `--strict` turns these diagnostics and invisible short events
into file failures. Normal system font selection may substitute a family
without emitting a warning; strict mode is **not a guarantee** of exact font
identity or a complete font-coverage preflight.

`--encoding auto` accepts UTF-8 (with or without BOM) and BOM-marked UTF-16.
Explicit choices: `utf-8`, `utf-16le`, `utf-16be`, `gb18030`, `big5`.
Malformed input is rejected. Legacy decoders also reject replacement-character
output rather than silently converting damaged bytes. No statistical encoding
guessing is performed.

## Experimental HDR10/PQ palette

SDR is the default. To encode palette colors with BT.2020 primaries and
ST 2084 PQ:

```sh
pgs-converter convert movie.ass \
  --size 3840x2160 --fps 24000/1001 \
  --color-profile hdr10 --subtitle-white-nits 203 \
  -o movie.hdr.sup
```

`--subtitle-white-nits` defaults to **203 nits** in HDR mode and accepts finite
values greater than zero up to 10000. It is invalid with the SDR profile.
Values above 300 produce a comfort warning. A white-level setting expresses
**encoding intent**, not a guarantee of measured on-screen brightness.
HDR is independent of canvas resolution.

The HDR path interprets source subtitle RGB as SDR sRGB/BT.709 graphics,
linearizes it, converts primaries into BT.2020, scales by reference-white
luminance, applies PQ, and writes limited-range 8-bit YCbCr palette entries.
It does not simply change the YCbCr matrix or scale transparency.
Palette selection uses deterministic, linear-light premultiplied RGBA features;
the PQ transform is evaluated in floating point before final palette encoding.
The source renderer still uses libass's 8-bit coverage and compositing.

Important limitations:

- Standalone SUP has no universally usable signal for primaries, transfer
  function or reference-white luminance. Players may assume BT.709, guess from
  video, or composite subtitles as SDR regardless of palette intent.
- **The HDR option is experimental.** No target HDR player/output chain has yet
  been selected and verified. A decoder accepting the file does not establish
  correct HDR colors. Brightness can be remapped by player and display settings.
- No HDR10 video metadata, MaxCLL/MaxFALL, HLG, Dolby Vision-specific processing,
  video tone mapping or automatic video probing is implemented.
- Generate separate SDR and HDR SUP files when both deliveries are needed.
  Neither 4K resolution nor a `.hdr.sup` filename supplies HDR signaling.

## Rendering and format limits

Frames are sampled on the specified global frame grid, with integer/rational
timing and at most one frame of subtitle-boundary rounding. Short events that
fall entirely between sampled frames are reported; strict mode rejects them.
Unchanged pixels and positions are deduplicated, blank intervals are skipped,
and explicit empty compositions clear subtitles at gaps and at the end.

PGS allows at most 256 palette entries. The encoder reserves transparency,
preserves source colors exactly in the intermediate RGB palette when they fit,
and otherwise uses deterministic weighted median-cut quantization. Final
YCbCr encoding remains lossy even without palette reduction. Animated or
large subtitles may produce large files; every changed image is a complete
self-contained composition, without advanced palette/object-cache reuse.
Frames are streamed rather than buffered for the entire movie.

SDR output uses BT.709 limited-range palette conversion. Some decoders assume
BT.601 for canvases at or below 576 lines; these sizes produce a warning.
ASS `YCbCr Matrix` compatibility mangling is applied to a BT.709 SDR rendering
target before optional HDR conversion. `None` opts out; a missing header
follows the conventional TV.601 interpretation. This does not infer video
color properties or promise exact color matching to an unknown video.

Negative times, non-positive durations and out-of-range PGS fields are rejected.
32-bit 90 kHz timestamp wrapping is not supported (roughly 13.25 hours).
Animation fidelity is limited by frame rate, palette quantization, libass
compatibility and player behavior. Strict Blu-ray buffering/bandwidth
compliance and physical-player certification are out of scope.

## Development and validation

```sh
go test ./...
go vet ./...
go test -race ./...       # on supported host/toolchains
go test ./internal/pgs -run '^$' -fuzz FuzzRLE -fuzztime 5s
go test ./internal/subtitle -run '^$' -fuzz FuzzDecode -fuzztime 5s
```

Install FFmpeg to enable the independent PGS decoder integration test; otherwise
that test reports a skip. The test preserves source timestamps with `-copyts`,
compares decoded HD frames to libass source frames on black, checks blank
frames for exact absence of residue, and limits active-pixel mean RGB error
to 2 and peak component error to 16. Font availability can affect renderings.
Other tests cover millisecond timing, SSA input, literal escaping, overlapping
events, RLE boundaries, ODS fragmentation, deterministic quantization, output
protection, cancellation and mixed-result batches.

HDR tests verify ST 2084 reference values, primary conversion, palette black/
white points, actual 203-nit encoded white, and unchanged geometry/alpha across
profiles. They do not certify player HDR interpretation. Quantization tests
bound mean linear premultiplied component error to 0.025 and peak to 0.15 for
the fixed gradient fixture, not arbitrary inputs.

### Portable macOS archive

```sh
go build -trimpath -o build/pgs-converter ./cmd/pgs-converter
bash scripts/bundle-macos.sh build/pgs-converter dist/pgs-converter-macos
```

The script copies transitive Homebrew dylibs, rewrites loader paths, applies
ad-hoc signatures, and includes dependency notices and build-version records.
It downloads the matching FreeType license text from the upstream source tag,
so packaging requires internet access.
This is not Apple notarization. The Windows companion script follows DLL
imports and collects the selected MSYS2 environment's license directory.
See `THIRD_PARTY_NOTICES`; audit native dependency licenses and source/relinking
obligations before publicly redistributing archives.

Remaining release gates: clean-machine tests on all four targets, native
dependency source/version pinning, an actual player smoke test,
long-duration performance characterization, and a chosen HDR playback chain.

Local macOS ARM64 smoke measurements at 1920x1080 / 24 fps: a one-hour static
subtitle produced two display sets, an approximately 11 KiB file and 22 MiB
peak RSS; a one-minute moving HDR subtitle produced 1,441 display sets, an
approximately 11 MiB file and 29 MiB peak RSS. These are small synthetic
fixtures, not general performance guarantees. Independent FFmpeg MKV
mux/demux roundtrip is tested; an actual target player's presentation is still
a release gate.
