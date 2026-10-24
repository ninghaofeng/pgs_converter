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

Detect the native architecture, check dependencies, run tests and vet, build,
and create a portable archive under dist/. No paths need to be supplied.

  --install-dependencies  Allow Homebrew to install missing Go/pkgconf/libass.
  -h, --help             Show this help without changing the environment.

Homebrew must already be installed (https://brew.sh). If Apple command-line
tools are missing, the install option opens Apple's installer; finish it and
rerun this command. Builds may download Go modules and license text.
USAGE
}

fail() {
    printf 'Build failed: %s\n' "$*" >&2
    exit 1
}

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

# Unique package names make repeated invocations safe without deleting a prior
# successful package or asking beginners to choose a new destination directory.
run_id="$(date +%Y%m%d-%H%M%S)-$$"
build_dir="$repo_root/build/macos-$arch"
package_name="pgs-converter-macos-$arch-$run_id"
destination="$repo_root/dist/$package_name"
smoke="$build_dir/smoke-$run_id.sup"
trap 'if [[ -f "$smoke" ]]; then rm -- "$smoke"; fi' EXIT

printf 'Building macOS %s with %s and libass %s\n' "$arch" "$(go version)" "$(pkg-config --modversion libass)"
go test ./...
go vet ./...
mkdir -p "$build_dir" "$repo_root/dist"
go build -trimpath -ldflags "-X github.com/ninghaofeng/pgs_converter/internal/cli.Version=${GITHUB_SHA:-dev}" -o "$build_dir/pgs-converter" ./cmd/pgs-converter
bash "$script_dir/bundle-macos.sh" "$build_dir/pgs-converter" "$destination"
"$destination/pgs-converter" convert testdata/sample.srt --size 1280x720 --fps 24 -o "$smoke"
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
