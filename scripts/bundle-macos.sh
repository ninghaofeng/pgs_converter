#!/usr/bin/env bash
# Build a relocatable development bundle from a native, already-linked binary.
# Run from the repository root; Homebrew must contain the linked dependencies.
# This script ad-hoc signs modified Mach-O files but does not notarize them.
set -euo pipefail

if [[ $# -ne 2 ]]; then
    echo "usage: bash scripts/bundle-macos.sh BUILT_BINARY NEW_OUTPUT_DIRECTORY" >&2
    exit 2
fi
binary=$1
destination=$2
brew_prefix=$(brew --prefix)
cellar=$(brew --cellar)
if [[ -e "$destination" ]]; then
    echo "output directory already exists: $destination" >&2
    exit 1
fi
mkdir -p "$destination/lib" "$destination/licenses"
cp "$binary" "$destination/pgs-converter"
queue=("$destination/pgs-converter")
prefixes=()
index=0
# Traverse the actual Mach-O dependency graph, copying each dylib once.
# Formula metadata alone can differ from the libraries an older binary links.
while [[ $index -lt ${#queue[@]} ]]; do
    current=${queue[$index]}
    index=$((index + 1))
    while IFS= read -r dependency; do
        name=$(basename "$dependency")
        # A dylib's own install ID appears in otool output but is not an import.
        if [[ "$dependency" == "@loader_path/$(basename "$current")" ]]; then
            continue
        fi
        resolved=$dependency
        case "$dependency" in
            /usr/lib/*|/System/Library/*) continue ;;
            @loader_path/*|@rpath/*)
                # Homebrew's public lib links resolve rpath-based sibling imports.
                resolved="$brew_prefix/lib/$name"
                if [[ ! -f "$resolved" ]]; then
                    echo "cannot resolve dependency: $dependency" >&2
                    exit 1
                fi
                ;;
            /opt/homebrew/*|/usr/local/*) ;;
            *) echo "unsupported dependency path: $dependency" >&2; exit 1 ;;
        esac
        if [[ ! -f "$destination/lib/$name" ]]; then
            cp -L "$resolved" "$destination/lib/$name"
            source_dir=$(dirname "$(realpath "$resolved")")
            # Record the actual Cellar version, not only the current opt symlink,
            # so notices match the copied library and its installed source.
            relative=${source_dir#"$cellar/"}
            if [[ "$relative" == "$source_dir" ]]; then
                echo "library is not owned by a Homebrew cellar: $resolved" >&2
                exit 1
            fi
            formula=${relative%%/*}
            relative=${relative#*/}
            version=${relative%%/*}
            prefixes+=("$cellar/$formula/$version")
            chmod u+w "$destination/lib/$name"
            install_name_tool -id "@loader_path/$name" "$destination/lib/$name"
            queue+=("$destination/lib/$name")
        fi
        if [[ "$current" == "$destination/pgs-converter" ]]; then
            # The executable locates lib/; dylibs locate neighboring dylibs.
            replacement="@executable_path/lib/$name"
        else
            replacement="@loader_path/$name"
        fi
        install_name_tool -change "$dependency" "$replacement" "$current"
    done < <(otool -L "$current" | tail -n +2 | sed -E 's/^[[:space:]]*//; s/ \(compatibility version.*$//')
done

# Collect native notices from the resolved package versions, not build tools.
for prefix in "${prefixes[@]}"; do
    formula=$(basename "$(dirname "$prefix")")
    mkdir -p "$destination/licenses/$formula"
    for notice in "$prefix"/COPYING* "$prefix"/LICENSE* "$prefix"/OFL* "$prefix"/AUTHORS* "$prefix"/LGPL*; do
        if [[ -f "$notice" ]]; then
            cp "$notice" "$destination/licenses/$formula/"
        fi
    done
    if [[ "$formula" == "freetype" ]]; then
        # Homebrew's summary license points to the full FreeType License, which
        # is fetched from the matching upstream release rather than from HEAD.
        version=$(basename "$prefix")
        version=${version%%_*}
        tag=${version//./-}
        curl --fail --location --silent --show-error "https://raw.githubusercontent.com/freetype/freetype/VER-$tag/docs/FTL.TXT" \
            -o "$destination/licenses/freetype/FTL.TXT"
    fi
done
cp README.md README.zh-CN.md THIRD_PARTY_NOTICES "$destination/"
modules=$(go env GOMODCACHE)
# Keep these module-cache paths in sync with go.mod when dependencies change.
mkdir -p "$destination/licenses/pflag" "$destination/licenses/x-text"
cp "$modules/github.com/spf13/pflag@v1.0.6/LICENSE" "$destination/licenses/pflag/"
cp "$modules/golang.org/x/text@v0.28.0/LICENSE" "$modules/golang.org/x/text@v0.28.0/PATENTS" "$destination/licenses/x-text/"
mkdir -p "$destination/licenses/go"
cp "$(go env GOROOT)/LICENSE" "$(go env GOROOT)/PATENTS" "$destination/licenses/go/"
{
    go version
    pkg-config --modversion libass
    printf '%s\n' "${prefixes[@]}"
} > "$destination/BUILD-INFO.txt"
# Loader-path edits invalidate prior signatures; sign only after all rewrites.
for file in "${queue[@]}"; do
    codesign --force --sign - "$file"
done
"$destination/pgs-converter" version
