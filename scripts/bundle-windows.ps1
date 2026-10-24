# Bundle native Go/cgo output, auditing rather than copying DLLs in static mode.
# Run from the repository root with the matching MSYS2 prefix and objdump on PATH.
# This creates a development archive directory, not a signed Windows installer.
param(
    [Parameter(Mandatory=$true)][string]$Binary,
    [Parameter(Mandatory=$true)][string]$Prefix,
    [Parameter(Mandatory=$true)][string]$Destination,
    [string]$Objdump = "objdump",
    [switch]$Static
)
$ErrorActionPreference = "Stop"
. "$PSScriptRoot/windows-static.ps1"
if (Test-Path $Destination) { throw "Output directory already exists: $Destination" }
New-Item -ItemType Directory -Path $Destination | Out-Null
Copy-Item $Binary "$Destination/pgs-converter.exe"
$queue = [System.Collections.Generic.Queue[string]]::new()
$queue.Enqueue("$Destination/pgs-converter.exe")
# Recursively inspect copied imports instead of copying the whole toolchain bin/.
while ($queue.Count -gt 0) {
    $current = $queue.Dequeue()
    $imports = & $Objdump -p $current
    if ($LASTEXITCODE -ne 0) { throw "Cannot inspect imports of $current" }
    if ($Static) {
        Assert-WindowsStaticImports $imports | Set-Content -LiteralPath "$Destination/PE-IMPORTS.txt" -Encoding Ascii
        continue
    }
    foreach ($line in $imports) {
        if ($line -notmatch 'DLL Name:\s*(\S+)') { continue }
        $name = $Matches[1]
        if (Test-Path "$Destination/$name") { continue }
        if (Test-Path "$Prefix/bin/$name") {
            Copy-Item "$Prefix/bin/$name" "$Destination/$name"
            $queue.Enqueue("$Destination/$name")
        } elseif ($name -match '^(api-ms-win-|ext-ms-win-)' -or (Test-Path "$env:SystemRoot/System32/$name")) {
            # API-set contracts and OS-provided DLLs are supplied by Windows,
            # not redistributed from the developer's system directory.
            continue
        } else {
            throw "Unresolved native dependency: $name"
        }
    }
}
Copy-Item README.md, README.zh-CN.md, THIRD_PARTY_NOTICES $Destination
# Keep complete native package notices; license/source obligations still need
# release review, since this directory can include multiple linked components.
if (-not (Test-Path "$Prefix/share/licenses")) { throw "Native dependency licenses not found" }
Copy-Item -Recurse "$Prefix/share/licenses" "$Destination/licenses"
$modules = & go env GOMODCACHE
if ($LASTEXITCODE -ne 0) { throw "Cannot locate Go module cache" }
New-Item -ItemType Directory -Path "$Destination/licenses/pflag", "$Destination/licenses/x-text" | Out-Null
# Keep the versioned module-cache paths aligned with go.mod.
Copy-Item "$modules/github.com/spf13/pflag@v1.0.6/LICENSE" "$Destination/licenses/pflag/"
Copy-Item "$modules/golang.org/x/text@v0.28.0/LICENSE", "$modules/golang.org/x/text@v0.28.0/PATENTS" "$Destination/licenses/x-text/"
$goroot = & go env GOROOT
New-Item -ItemType Directory -Path "$Destination/licenses/go" | Out-Null
Copy-Item "$goroot/LICENSE", "$goroot/PATENTS" "$Destination/licenses/go/"
& go version | Out-File "$Destination/BUILD-INFO.txt"
& pkg-config --modversion libass | Out-File -Append "$Destination/BUILD-INFO.txt"
if ($Static) {
    @(
        "Build tag: pgs_static; external linker: -static.",
        "Linkage: static native libraries; Windows system DLLs remain required.",
        "Fonts: system fonts or user-supplied --font-dir; no fonts bundled.",
        "This is a development package, not a redistribution-compliance bundle.",
        "Before redistribution, provide applicable LGPL corresponding sources",
        "and the materials/instructions needed to relink against modified LGPL",
        "libraries. License notices alone do not satisfy those requirements.",
        "See THIRD_PARTY_NOTICES and the README static-build section."
    ) | Set-Content -LiteralPath "$Destination/STATIC-LINKING.txt" -Encoding Ascii
    & pkg-config --static --libs libass | Out-File -Append "$Destination/BUILD-INFO.txt"
    if ($LASTEXITCODE -ne 0) { throw "Cannot record static native dependency flags" }
}
# Starting the packaged binary catches unresolved DLLs before archive creation.
& "$Destination/pgs-converter.exe" version
if ($LASTEXITCODE -ne 0) { throw "Packaged binary failed to start" }
