#!/usr/bin/env bash
# Native build-and-package entry point. Dependency installation is opt-in;
# environment changes apply only to this script and its child processes.
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)
install_dependencies=false

usage() {
    cat <<'USAGE'
Usage: bash scripts/build-macos.sh [--install-dependencies]

Check dependencies, statically link third-party libraries, run tests and vet,
and create a single-executable package under dist/. No paths are required.

  --install-dependencies  Allow missing tools/libraries and private source builds.
  -h, --help             Show this help without changing the environment.

Homebrew must already be installed (https://brew.sh). If Apple command-line
tools are missing, the install option opens Apple's installer; finish it and
rerun this command. Missing Graphite2 archives are built privately from pinned,
checksum-verified source with explicit installation permission. Other missing
static libraries cause an error, never a dylib fallback.
USAGE
}

fail() {
    printf 'Build failed: %s\n' "$*" >&2
    exit 1
}

bundle_macos() {
    local binary=$1 destination=$2 metadata=$3 imports count=0
    local prefix formula found notice version tag modules dependency
    if [[ -e "$destination" ]]; then
        echo "output directory already exists: $destination" >&2
        exit 1
    fi
    imports=$(otool -L "$binary")
    while IFS= read -r dependency; do
        [[ -n "$dependency" ]] || continue
        case "$dependency" in
            /usr/lib/*|/System/Library/*) count=$((count + 1)) ;;
            *) echo "static executable imports a non-system library: $dependency" >&2; exit 1 ;;
        esac
    done < <(printf '%s\n' "$imports" | tail -n +2 | sed -E 's/^[[:space:]]*//; s/ \(compatibility version.*$//')
    [[ $count -gt 0 ]] || { echo "cannot verify Mach-O imports" >&2; exit 1; }
    mkdir -p "$destination/licenses"
    cp "$binary" "$destination/pgs-converter"
    printf '%s\n' "$imports" > "$destination/MACHO-IMPORTS.txt"
    cp "$metadata/static-archives.txt" "$metadata/native-prefixes.txt" "$metadata/libass.pc" "$destination/"
    while IFS= read -r prefix; do
        formula=$(basename "$(dirname "$prefix")")
        if [[ -f "$prefix/SOURCE-INFO.txt" ]]; then formula=graphite2; fi
        mkdir -p "$destination/licenses/$formula"
        found=false
        for notice in "$prefix"/COPYING* "$prefix"/LICENSE* "$prefix"/OFL* "$prefix"/AUTHORS* "$prefix"/LGPL* "$prefix"/SOURCE-INFO*; do
            if [[ -f "$notice" ]]; then
                cp "$notice" "$destination/licenses/$formula/"
                found=true
            fi
        done
        "$found" || { echo "native license notices missing: $prefix" >&2; exit 1; }
        if [[ "$formula" == freetype ]]; then
            version=$(basename "$prefix")
            version=${version%%_*}
            tag=${version//./-}
            curl --fail --location --silent --show-error \
                "https://raw.githubusercontent.com/freetype/freetype/VER-$tag/docs/FTL.TXT" \
                -o "$destination/licenses/freetype/FTL.TXT"
        fi
    done < "$metadata/native-prefixes.txt"
    cp README.md README.zh-CN.md THIRD_PARTY_NOTICES "$destination/"
    modules=$(go env GOMODCACHE)
    mkdir -p "$destination/licenses/pflag" "$destination/licenses/x-text" "$destination/licenses/go"
    cp "$modules/github.com/spf13/pflag@v1.0.6/LICENSE" "$destination/licenses/pflag/"
    cp "$modules/golang.org/x/text@v0.28.0/LICENSE" "$modules/golang.org/x/text@v0.28.0/PATENTS" "$destination/licenses/x-text/"
    cp "$(go env GOROOT)/LICENSE" "$(go env GOROOT)/PATENTS" "$destination/licenses/go/"
    {
        go version
        pkg-config --modversion libass
        printf 'Linkage: static third-party archives; system libraries/frameworks remain dynamic.\n'
    } > "$destination/BUILD-INFO.txt"
    cat > "$destination/STATIC-LINKING.txt" <<'NOTICE'
Fonts: system fonts or user-supplied --font-dir; no fonts bundled.
This is a development package, not a redistribution-compliance bundle.
Before redistribution, audit all licenses and provide applicable corresponding
sources and materials/instructions to relink with modified LGPL libraries.
License notices alone do not satisfy those requirements.
See THIRD_PARTY_NOTICES and the README.
NOTICE
    codesign --force --sign - "$destination/pgs-converter"
    codesign --verify --strict "$destination/pgs-converter"
    "$destination/pgs-converter" version
}

# Tests can source packaging functions without starting a build.
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
    return 0
fi

for argument in "$@"; do
    case "$argument" in
        --install-dependencies) install_dependencies=true ;;
        -h|--help) usage; exit 0 ;;
        *) printf 'Unknown option: %s\n' "$argument" >&2; usage >&2; exit 2 ;;
    esac
done

[[ "$(uname -s)" == Darwin ]] || fail "This script requires macOS; use build-windows.cmd on Windows."
case "$(uname -m)" in
    arm64) arch=arm64; default_brew=/opt/homebrew/bin/brew ;;
    x86_64) arch=amd64; default_brew=/usr/local/bin/brew ;;
    *) fail "Only macOS ARM64 and x64 are supported." ;;
esac
cd "$repo_root"

if ! xcrun --find clang >/dev/null 2>&1; then
    if "$install_dependencies"; then
        xcode-select --install
        fail "Finish installing Apple's command-line tools, then rerun this command."
    fi
    fail "Apple command-line tools are missing. Run xcode-select --install, or rerun with --install-dependencies."
fi

if command -v brew >/dev/null 2>&1; then
    brew_command=$(command -v brew)
elif [[ -x "$default_brew" ]]; then
    brew_command=$default_brew
else
    fail "Homebrew is missing. Install it from https://brew.sh, then rerun this command."
fi
brew_prefix=$("$brew_command" --prefix)
export PATH="$brew_prefix/bin:$brew_prefix/sbin:$PATH"
export GOTOOLCHAIN=local
export GOOS=darwin GOARCH="$arch" CGO_ENABLED=1
export CC="$(xcrun --find clang)"
sdk_root=$(xcrun --show-sdk-path)
export SDKROOT="$sdk_root"
export PKG_CONFIG_PATH="$brew_prefix/lib/pkgconfig:$brew_prefix/share/pkgconfig"
# Keep pkgconf's default paths: Homebrew also supplies SDK zlib/bzip2 metadata.
unset PKG_CONFIG_LIBDIR PKG_CONFIG_SYSROOT_DIR

minimum_go=$(awk '$1 == "go" { print $2; exit }' go.mod)
[[ -n "$minimum_go" ]] || fail "Cannot read the minimum Go version from go.mod."
go_is_supported() {
    command -v go >/dev/null 2>&1 || return 1
    local version
    version=$(go version) || return 1
    [[ "$version" =~ go([0-9]+)\.([0-9]+)\.([0-9]+)[[:space:]] ]] || return 1
    version="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.${BASH_REMATCH[3]}"
    awk -v actual="$version" -v minimum="$minimum_go" 'BEGIN {
        split(actual, a, "."); split(minimum, m, ".");
        for (i = 1; i <= 3; i++) {
            if (a[i]+0 > m[i]+0) exit 0;
            if (a[i]+0 < m[i]+0) exit 1;
        }
        exit 0;
    }'
}

if ! go_is_supported; then
    if ! "$install_dependencies"; then
        fail "Go $minimum_go or newer is required. Rerun with --install-dependencies, or install a native Go release."
    fi
    if "$brew_command" list --versions go >/dev/null 2>&1; then
        "$brew_command" upgrade go
    else
        "$brew_command" install go
    fi
    go_is_supported || fail "A supported Go version is still unavailable; check PATH and the Homebrew installation."
fi
[[ "$(go env GOHOSTARCH)" == "$arch" ]] || fail "Go architecture does not match $arch. Use matching native Go, Homebrew and Terminal architectures."

if ! command -v pkg-config >/dev/null 2>&1 || ! pkg-config --exists libass; then
    if ! "$install_dependencies"; then
        fail "pkgconf/libass development files are missing. Run brew install pkgconf libass, or rerun with --install-dependencies."
    fi
    "$brew_command" install pkgconf libass
fi
command -v pkg-config >/dev/null 2>&1 || fail "pkg-config is still unavailable after dependency installation."
pkg-config --exists libass || fail "libass is still unavailable to pkg-config; check the Homebrew installation."
export PKG_CONFIG="$(command -v pkg-config)"

native_root="$repo_root/build/native/macos-$arch"
graphite_prefix="$native_root/graphite2-1.3.15-static"
if [[ ! -f "$brew_prefix/lib/libgraphite2.a" && ! -f "$graphite_prefix/lib/libgraphite2.a" ]]; then
    "$install_dependencies" || fail "Graphite2's static archive is missing. Rerun with --install-dependencies to build a private, pinned static copy."
    if ! command -v cmake >/dev/null 2>&1; then
        "$brew_command" install cmake
    fi
    mkdir -p "$native_root"
    graphite_archive="$native_root/graphite2-1.3.15.tgz"
    graphite_hash=c6bc8b4252724665297f7cad0c55897285c673f9b8e6db3522ace833593fe0b1
    if [[ ! -f "$graphite_archive" ]]; then
        curl --fail --location --silent --show-error \
            https://github.com/silnrsi/graphite/releases/download/1.3.15/graphite2-1.3.15.tgz \
            -o "$graphite_archive"
    fi
    printf '%s  %s\n' "$graphite_hash" "$graphite_archive" | shasum -a 256 -c -
    tar -xzf "$graphite_archive" -C "$native_root"
    cmake -S "$native_root/graphite2-1.3.15" -B "$native_root/graphite2-build" \
        -DBUILD_SHARED_LIBS=OFF -DBUILD_TESTING=OFF -DCMAKE_BUILD_TYPE=Release \
        -DCMAKE_OSX_SYSROOT="$sdk_root" -DCMAKE_OSX_ARCHITECTURES="$(uname -m)" \
        -DCMAKE_INSTALL_PREFIX="$graphite_prefix"
    cmake --build "$native_root/graphite2-build" --parallel
    cmake --install "$native_root/graphite2-build"
    cp "$native_root/graphite2-1.3.15/COPYING" "$native_root/graphite2-1.3.15/LICENSE" "$graphite_prefix/"
    printf 'graphite2 1.3.15\nsha256 %s\n' "$graphite_hash" > "$graphite_prefix/SOURCE-INFO.txt"
fi
metadata="$native_root/pkgconfig"
extra_dir=
if [[ -f "$graphite_prefix/lib/libgraphite2.a" ]]; then
    extra_dir="$graphite_prefix/lib"
fi
go run ./cmd/macos-static -pkg-config "$PKG_CONFIG" -sdk "$sdk_root" \
    -cellar "$("$brew_command" --cellar)" -output "$metadata" -extra-lib-dir "$extra_dir"
export PKG_CONFIG_PATH="$metadata"
export PKG_CONFIG_LIBDIR="$metadata"

# Unique package names make repeated invocations safe without deleting a prior
# successful package or asking beginners to choose a new destination directory.
run_id="$(date +%Y%m%d-%H%M%S)-$$"
build_dir="$repo_root/build/macos-$arch-static"
package_name="pgs-converter-macos-$arch-static-$run_id"
destination="$repo_root/dist/$package_name"
smoke="$build_dir/smoke-$run_id.sup"
isolated=
cleanup() {
    if [[ -f "$smoke" ]]; then rm -- "$smoke"; fi
    if [[ -n "$isolated" ]]; then
        rm -- "$isolated/pgs-converter"
        rmdir -- "$isolated"
    fi
}
trap cleanup EXIT

link_flags="-linkmode=external -extld=/usr/bin/clang++"
printf 'Building macOS %s (static) with %s and libass %s\n' "$arch" "$(go version)" "$(pkg-config --modversion libass)"
go test -ldflags "$link_flags" ./...
go vet ./...
mkdir -p "$build_dir" "$repo_root/dist"
go build -trimpath -ldflags "$link_flags -X github.com/ninghaofeng/pgs_converter/internal/cli.Version=${GITHUB_SHA:-dev}" -o "$build_dir/pgs-converter" ./cmd/pgs-converter
bundle_macos "$build_dir/pgs-converter" "$destination" "$metadata"
isolated=$(mktemp -d "${TMPDIR:-/tmp}/pgs-static-smoke.XXXXXX")
cp "$destination/pgs-converter" "$isolated/"
(
    cd "$isolated"
    for fixture in sample.srt animated.ass; do
        env -i PATH=/usr/bin:/bin HOME="$HOME" ./pgs-converter convert \
            "$repo_root/testdata/$fixture" --size 3840x2160 --fps 24000/1001 --overwrite -o "$smoke"
    done
)
if command -v ffprobe >/dev/null 2>&1; then
    ffprobe -v error -show_streams "$smoke"
else
    printf 'Skipping independent packaged-output probe: ffprobe is not installed.\n'
fi
tar -czf "$destination.tar.gz" -C "$repo_root/dist" "$package_name"
(
    cd "$repo_root/dist"
    shasum -a 256 "$package_name.tar.gz" > "$package_name.tar.gz.sha256"
)
printf '\nBuild succeeded.\nExecutable: %s/pgs-converter\nArchive: %s.tar.gz\nChecksum: %s.tar.gz.sha256\n' "$destination" "$destination" "$destination"
