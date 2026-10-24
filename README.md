# pgs-converter

English | [简体中文](./README.zh-CN.md)

Convert SRT, ASS and SSA text subtitles into PGS (`.sup`) bitmap subtitles.
The CLI and PGS encoder are written in Go; libass renders fonts, positioning,
layers, vector drawings, fades, movement, transforms and karaoke.

This is an initial implementation, not a Blu-ray authoring/compliance tool.
It does not encode video, perform OCR, or mux MKV files.

## Build

### Recommended: one-command build and package

Run these commands from the project directory. No `Binary`, `Prefix` or
`Destination` arguments are needed: the entry points detect the native
architecture, locate dependencies, run tests and vet, build the CLI, collect
statically link third-party libraries, collect licenses, and test the standalone
executable. **Windows and macOS only provide static builds.**

**Windows (PowerShell or Command Prompt):**

```powershell
.\scripts\build-windows.cmd
```

If dependencies are missing, the default is to stop with guidance, **not**
install software. To explicitly allow dependency installation:

```powershell
.\scripts\build-windows.cmd -InstallDependencies
```

This permits winget to install missing Go/MSYS2, and MSYS2's pacman to install
the matching compiler, pkgconf and libass. Microsoft's **App Installer**
(`winget`) must already be available if Go/MSYS2 need automatic installation.
Installers may request administrator approval. After installation or an MSYS2
core update, follow any restart/retry instructions and rerun the command.
For an existing nonstandard MSYS2 installation, add:

```powershell
.\scripts\build-windows.cmd -Msys2Root D:\tools\msys64
```

Supply the MSYS2 root, not its `ucrt64` or `clangarm64` subdirectory.
The launcher uses a process-scoped PowerShell execution-policy bypass; it does
not permanently change the machine's policy.
It uses built-in Windows PowerShell 5.1; installing PowerShell 7 is not required.
When multiple Go installations are present, the first executable on `PATH` is
used and checked for the required version and native architecture.

**macOS (Terminal):**

```sh
bash scripts/build-macos.sh
```

To explicitly allow installing missing dependencies:

```sh
bash scripts/build-macos.sh --install-dependencies
```

[Homebrew](https://brew.sh) must already be installed. The opt-in flag permits
installing/upgrading Go and installing pkgconf/libass. If Apple command-line
tools are missing, it opens Apple's installer; complete installation and rerun.
If Homebrew lacks Graphite2's static archive, the opt-in flag also permits
installing CMake if needed and building Graphite2 1.3.15 privately under
`build/native/` from pinned, SHA-256-verified source. Later builds reuse it.
Other missing static libraries fail explicitly rather than falling back to
dylibs. No Homebrew libraries are replaced or modified.

Both entry points build **only for the current native architecture**, not by
cross-compiling. Go, the C toolchain and native libraries must match.
FFmpeg is optional: decoder tests and the packaged-output probe run when the
required FFmpeg tools are available and otherwise report a skip.
Even without the installation flag, builds may download Go modules and
packaging downloads license text; this is not an offline build mode.

Results are written to `build/` and `dist/`. Each run creates a uniquely named
package directory under `dist/`, plus a `.zip` (Windows) or `.tar.gz` (macOS)
and a matching `.sha256` file. Existing successful packages are not deleted.
Only the executable is needed at runtime; keep licenses and documentation when
redistributing. OS-provided libraries/frameworks remain required.
Run `.\scripts\build-windows.cmd -?` or
`bash scripts/build-macos.sh --help` for help.

### Build script layout

[`scripts/`](./scripts/) contains only `build-macos.sh`, `build-windows.cmd`
and `build-windows.ps1`. Packaging and native dependency audits are integrated
into the platform scripts; the CMD file launches the PowerShell script.
[`cmd/macos-static/`](./cmd/macos-static/) is a build-time Go helper for resolving
static archives and generating pkg-config metadata, not a runtime dependency.
Build-script tests and PowerShell test fixtures live in
[`internal/buildscripts/`](./internal/buildscripts/).

### Static-only linking and verification

No build-mode flag is necessary. Windows still accepts the old `-Static`
switch for compatibility, but it no longer selects a separate mode.
This is real static linking, not an executable that extracts DLLs at startup.
It uses the same system-font lookup and optional `--font-dir`; no fonts are
bundled. Windows-provided DLLs (including the OS UCRT on Windows 10/11) remain
required, but third-party renderer/compiler-runtime DLLs must not be imported.

All builds request `pkg-config --static` private dependencies.
On Windows the linker is forced into static external linking.
C compilation still uses GCC/Clang, but the final linker uses the matching
`g++` (UCRT64) or `clang++` (CLANGARM64) driver so C++ dependencies such as
Graphite2 receive their runtime libraries after the static dependency archives.
The C++ driver is checked alongside the C compiler and is supplied by the same
MSYS2 compiler package; the static DLL-import audit remains mandatory.
Matching static `.a` libraries for the entire dependency chain are required;
an import library ending in `.dll.a` is not a substitute. Missing libraries or
unresolved symbols fail the build, with no fallback to the DLL version.
Dynamic-library packaging has been removed.

Outputs use `build/windows-<arch>-static/` or `build/macos-<arch>-static/`,
and uniquely named `dist/pgs-converter-<os>-<arch>-static-.../` packages. Only
`pgs-converter.exe` (Windows) or `pgs-converter` (macOS) is needed at runtime;
keep the accompanying notices and
documentation when distributing the package. Before declaring success the
script audits PE imports against a Windows system-DLL allowlist, then copies
only the EXE to a temporary directory and converts SRT and animated ASS with
MSYS2 removed from PATH. `PE-IMPORTS.txt` records the audit result and
`MSYS2-PACKAGES.txt` records installed package versions. An unknown
import fails explicitly even if that DLL happens to exist in System32.

On macOS, the build generates a self-contained `libass.pc` that replaces each
third-party `-l...` with its explicit static `.a` path, retaining only SDK system
libraries/frameworks dynamically. It uses Apple's C++ linker driver; macOS does
not support Windows-style global `-static`. The package's `MACHO-IMPORTS.txt`
must contain only `/usr/lib/` and `/System/Library/` imports. No dylibs or loader
path rewrites are included. `static-archives.txt` records the exact native
archives; the binary is ad-hoc signed, not notarized. SRT and animated ASS are
tested at 3840x2160, 24000/1001 from a temporary directory containing only the
executable, without Homebrew paths or DYLD environment variables.

**Redistribution:** this remains a development package. Static linking may
include LGPL components such as FriBidi and GLib. License text alone is not
sufficient: before redistribution, review all linked licenses and provide
applicable corresponding sources and object/relinking materials and
instructions allowing users to relink with modified LGPL libraries. The
scripts do not automatically assemble those compliance materials; see
`THIRD_PARTY_NOTICES` and the generated `STATIC-LINKING.txt`.

### Build prerequisites and platform status

Requirements: Go 1.24+, a cgo-compatible C compiler, pkg-config and libass
development/static libraries. macOS may additionally require CMake for the
private Graphite2 build. macOS ARM64 static builds have passed local tests,
import audits, standalone 4K conversion and packaging using libass 0.17.5.
FFmpeg is optional at runtime and is used for independent tests.

Use a Go installation and Homebrew dependencies matching the target
architecture. Setting `GOARCH` alone does not cross-compile C dependencies.

#### Windows

Use native Go plus MSYS2's **UCRT64** toolchain for x64, or **CLANGARM64** for
ARM64. The build entry point configures the native compiler and C++ linker,
pkg-config paths and static linkage, then audits the executable. Use these
entry points rather than bypassing the static configuration with plain
`go build`.

The four-target GitHub Actions workflow runs these build-and-package entry
points on macOS
ARM64/x64 and Windows ARM64/x64, and prepares static executable packages.
**Only macOS ARM64 has been run locally; the other targets and their
packaging are not certified until their CI jobs and clean-machine checks pass.**
CI uses package-manager native dependencies and records installed versions;
native dependencies are not yet fully reproducibly source-pinned (the private
Graphite2 source is pinned).

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

Conversions print a `START` line, then per-file encoding progress to stderr
approximately every 500 ms (between frames), plus initial and final updates.
Percentages count sampled frames in merged active subtitle intervals, excluding
blank gaps; they are not percentages of the video duration or output size.
The final `100%` means encoding finished; only `OK` confirms the output was
flushed and committed. Loading fonts and a single expensive frame can delay an
update. Use `--no-progress` with either command to disable live updates while
keeping warnings, results and the summary. Output uses separate lines, including
when redirected to a log.

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
  Set `--jobs 2` to convert two files concurrently; without it, files run
  sequentially. For 4K, start with two workers and monitor memory usage.
  Concurrent progress lines include the input path and are serialized.
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

The build entry points run tests and vet with the correct static configuration.
For extra macOS tests after preparing a build (use `amd64` on Intel):

```sh
export PKG_CONFIG_PATH="$PWD/build/native/macos-arm64/pkgconfig"
export PKG_CONFIG_LIBDIR="$PKG_CONFIG_PATH"
go test -ldflags "-linkmode=external -extld=/usr/bin/clang++" ./...
go vet ./...
go test -race -ldflags "-linkmode=external -extld=/usr/bin/clang++" ./...
go test ./internal/pgs -run '^$' -fuzz FuzzRLE -fuzztime 5s
go test ./internal/subtitle -run '^$' -fuzz FuzzDecode -fuzztime 5s
```

Install FFmpeg to enable the independent PGS decoder integration test; otherwise
that test reports a skip. The test preserves source timestamps with `-copyts`,
compares decoded HD frames to libass source frames on black, checks blank
frames for exact absence of residue, and limits active-pixel mean RGB error
to 2 and peak component error to 16. Font availability can affect renderings.
The decoder fixture uses animated ASS vector drawings and asserts at most 255
visible source colors per sampled frame, isolating decoder accuracy from
system-font differences and lossy palette reduction. Text movement, fades,
karaoke and clearing are covered separately by the animated-text stream test;
lossy quantization quality has its own numeric regression limits below.
Other tests cover millisecond timing, SSA input, literal escaping, overlapping
events, RLE boundaries, ODS fragmentation, deterministic quantization, output
protection, cancellation and mixed-result batches.

HDR tests verify ST 2084 reference values, primary conversion, palette black/
white points, actual 203-nit encoded white, and unchanged geometry/alpha across
profiles. They do not certify player HDR interpretation. Quantization tests
bound mean linear premultiplied component error to 0.025 and peak to 0.15 for
the fixed gradient fixture, not arbitrary inputs.

### Static packages

```sh
bash scripts/build-macos.sh
```

The build calls the static-only bundler automatically. Both bundlers reject
third-party dynamic imports and include notices/build records without copying
DLLs or dylibs. macOS packaging downloads the matching FreeType license text
from the upstream source tag, so packaging requires internet access.
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
