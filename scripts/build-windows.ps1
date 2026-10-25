<#
.SYNOPSIS
Build, test and package the native Windows CLI and desktop app without mandatory path arguments.
.DESCRIPTION
Both CLI and desktop app are built by default. Dependency installation is opt-in.
-InstallDependencies permits winget
to install Go/MSYS2 and pacman to install the matching native C toolchain/libass.
Installers may request elevation. Existing packages are never deleted.
Only static native linking is supported; non-system DLL imports are rejected.
Static redistribution requires a separate license review.
.EXAMPLE
.\scripts\build-windows.ps1
.EXAMPLE
.\scripts\build-windows.ps1 -InstallDependencies
.EXAMPLE
.\scripts\build-windows.ps1 -Msys2Root D:\tools\msys64
.EXAMPLE
.\scripts\build-windows.ps1 -Static
.EXAMPLE
.\scripts\build-windows.ps1 -Gui
#>
[CmdletBinding()]
param(
    [switch]$InstallDependencies,
    [string]$Msys2Root,
    [switch]$Static, # Compatibility alias: static is now the only build mode.
    [switch]$Gui = $true # Compatibility option: GUI is now built by default.
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Get-WindowsLinkConfiguration {
    param([string]$PkgConfig, [string]$CxxLinker)
    $command = '"' + $PkgConfig.Replace("\", "/") + '"'
    if ($CxxLinker -notin @("g++.exe", "clang++.exe")) {
        throw "Static linking requires the matching C++ driver (g++.exe or clang++.exe)."
    }
    # The C++ driver supplies its runtime after the native dependency archives.
    # CC remains a C compiler, and cgo always requests pkg-config --static.
    $linkerFlags = "-linkmode=external -extld=$CxxLinker -extldflags=-static"
    return [pscustomobject]@{
        PkgConfig = $command
        LinkerFlags = $linkerFlags
    }
}

function Assert-WindowsStaticImports {
    param([string[]]$Lines)
    # Do not trust every DLL in System32: installers can place runtimes there.
    $systemDlls = @(
        "advapi32.dll", "bcrypt.dll", "bcryptprimitives.dll", "cfgmgr32.dll",
        "combase.dll", "comdlg32.dll", "crypt32.dll", "cryptbase.dll",
        "dnsapi.dll", "dwmapi.dll", "dwrite.dll", "gdi32.dll", "imm32.dll",
        "iphlpapi.dll", "kernel32.dll", "kernelbase.dll", "msimg32.dll",
        "msvcrt.dll", "mswsock.dll", "ncrypt.dll", "netapi32.dll",
        "normaliz.dll", "ntdll.dll", "ole32.dll", "oleaut32.dll", "opengl32.dll",
        "powrprof.dll", "profapi.dll", "propsys.dll", "psapi.dll",
        "rpcrt4.dll", "secur32.dll", "setupapi.dll", "shell32.dll", "shcore.dll",
        "shlwapi.dll", "ucrtbase.dll", "user32.dll", "userenv.dll",
        "usp10.dll", "uxtheme.dll", "version.dll", "winhttp.dll",
        "wininet.dll", "winmm.dll", "winspool.drv", "wintrust.dll",
        "ws2_32.dll", "wtsapi32.dll"
    )
    $imports = @(
        foreach ($line in $Lines) {
            # LLVM's "DLL name:" identifies the export module, not an import.
            if ($line -cmatch '^\s*DLL Name:\s*(.*)$') { $Matches[1].Trim() }
        }
    ) | Sort-Object -Unique
    if (-not $imports) {
        throw "Cannot verify static executable: no PE DLL imports found in objdump output."
    }
    foreach ($name in $imports) {
        if ($name -notin $systemDlls -and $name -notmatch '^(api-ms-win-|ext-ms-win-)[a-z0-9-]+\.dll$') {
            throw "Static executable still imports a non-allowlisted DLL: $name. Check static archives and linker flags; no dynamic fallback is permitted."
        }
    }
    return $imports
}

function New-WindowsBundle {
    param(
        [Parameter(Mandatory=$true)][string]$Binary,
        [Parameter(Mandatory=$true)][string]$Prefix,
        [Parameter(Mandatory=$true)][string]$Destination,
        [string]$Objdump = "objdump",
        [string]$GuiBinary
    )
    if (Test-Path $Destination) { throw "Output directory already exists: $Destination" }
    $imports = & $Objdump -p $Binary
    if ($LASTEXITCODE -ne 0) { throw "Cannot inspect imports of $Binary" }
    $verified = Assert-WindowsStaticImports $imports
    $guiImports = @()
    if ($GuiBinary) {
        $imports = & $Objdump -p $GuiBinary
        if ($LASTEXITCODE -ne 0) { throw "Cannot inspect imports of $GuiBinary" }
        $guiImports = @(Assert-WindowsStaticImports $imports)
    }
    New-Item -ItemType Directory -Path $Destination | Out-Null
    $verified | Set-Content -LiteralPath "$Destination/PE-IMPORTS.txt" -Encoding Ascii
    Copy-Item -LiteralPath $Binary -Destination "$Destination/pgs-converter.exe"
    if ($GuiBinary) {
        Copy-Item -LiteralPath $GuiBinary -Destination "$Destination/pgs-converter-gui.exe"
        $guiImports | Set-Content -LiteralPath "$Destination/GUI-PE-IMPORTS.txt" -Encoding Ascii
        Invoke-CheckedNative "go" @("run", "./cmd/package-licenses", "-output", "$Destination/licenses/go-modules")
        Invoke-CheckedNative "$Destination/pgs-converter-gui.exe" @("-version")
    }
    Copy-Item README.md, README.zh-CN.md, THIRD_PARTY_NOTICES $Destination
    if (-not (Test-Path "$Prefix/share/licenses")) { throw "Native dependency licenses not found" }
    New-Item -ItemType Directory -Force -Path "$Destination/licenses" | Out-Null
    Copy-Item -Recurse "$Prefix/share/licenses/*" "$Destination/licenses"
    $modules = & go env GOMODCACHE
    if ($LASTEXITCODE -ne 0) { throw "Cannot locate Go module cache" }
    New-Item -ItemType Directory -Path "$Destination/licenses/pflag", "$Destination/licenses/x-text", "$Destination/licenses/go" | Out-Null
    Copy-Item "$modules/github.com/spf13/pflag@v1.0.6/LICENSE" "$Destination/licenses/pflag/"
    Copy-Item "$modules/golang.org/x/text@v0.28.0/LICENSE", "$modules/golang.org/x/text@v0.28.0/PATENTS" "$Destination/licenses/x-text/"
    $goroot = & go env GOROOT
    if ($LASTEXITCODE -ne 0) { throw "Cannot locate Go runtime licenses" }
    Copy-Item "$goroot/LICENSE", "$goroot/PATENTS" "$Destination/licenses/go/"
    & go version | Out-File "$Destination/BUILD-INFO.txt"
    if ($LASTEXITCODE -ne 0) { throw "Cannot record Go version" }
    & pkg-config --static --modversion libass | Out-File -Append "$Destination/BUILD-INFO.txt"
    if ($LASTEXITCODE -ne 0) { throw "Cannot record libass version" }
    & pkg-config --static --libs libass | Out-File -Append "$Destination/BUILD-INFO.txt"
    if ($LASTEXITCODE -ne 0) { throw "Cannot record native dependency flags" }
    @(
        "Linkage: static third-party libraries; Windows system DLLs remain required.",
        "Subtitle fonts: system fonts or user-supplied --font-dir; none bundled.",
        "The optional GUI embeds toolkit UI fonts; see licenses/go-modules.",
        "This is a development package, not a redistribution-compliance bundle.",
        "Before redistribution, provide applicable LGPL corresponding sources",
        "and materials/instructions to relink with modified LGPL libraries.",
        "License notices alone do not satisfy those requirements.",
        "See THIRD_PARTY_NOTICES and the README."
    ) | Set-Content -LiteralPath "$Destination/STATIC-LINKING.txt" -Encoding Ascii
    & "$Destination/pgs-converter.exe" version
    if ($LASTEXITCODE -ne 0) { throw "Packaged binary failed to start" }
}

function Invoke-CheckedNative {
    param([string]$FilePath, [string[]]$Arguments)
    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$FilePath failed with exit code $LASTEXITCODE. Check its output above before retrying."
    }
}

function Get-WindowsArchitecture {
    # WOW64/emulated shells expose the native architecture separately.
    # Avoid RuntimeInformation properties absent in some Windows .NET runtimes.
    $architecture = $env:PROCESSOR_ARCHITEW6432
    if (-not $architecture) { $architecture = $env:PROCESSOR_ARCHITECTURE }
    if (-not $architecture) { throw "Cannot detect the native Windows architecture from the process environment." }
    switch ($architecture.ToLowerInvariant()) {
        "amd64" { return "x64" }
        "arm64" { return "arm64" }
        default { throw "Unsupported Windows architecture: $architecture. Only x64 and ARM64 are supported." }
    }
}

function Get-TargetConfiguration {
    param([string]$Architecture)
    switch ($Architecture.ToLowerInvariant()) {
        "x64" {
            return [pscustomobject]@{
                Arch = "amd64"; System = "UCRT64"; Prefix = "ucrt64"
                Compiler = "gcc.exe"; CxxLinker = "g++.exe"; Objdump = "objdump.exe"
                Packages = "pacman -S --needed --noconfirm mingw-w64-ucrt-x86_64-gcc mingw-w64-ucrt-x86_64-libass mingw-w64-ucrt-x86_64-pkgconf"
            }
        }
        "arm64" {
            return [pscustomobject]@{
                Arch = "arm64"; System = "CLANGARM64"; Prefix = "clangarm64"
                Compiler = "clang.exe"; CxxLinker = "clang++.exe"; Objdump = "llvm-objdump.exe"
                Packages = "pacman -S --needed --noconfirm mingw-w64-clang-aarch64-clang mingw-w64-clang-aarch64-libass mingw-w64-clang-aarch64-pkgconf"
            }
        }
        default { throw "Unsupported Windows architecture: $Architecture. Only x64 and ARM64 are supported." }
    }
}

function Find-Msys2Root {
    param([string]$RequestedRoot)
    if ($RequestedRoot) {
        $full = [System.IO.Path]::GetFullPath($RequestedRoot)
        if (-not (Test-Path -LiteralPath "$full/usr/bin/bash.exe" -PathType Leaf)) {
            throw "Invalid MSYS2 root: $full. Supply its root (for example C:\msys64), not the ucrt64/clangarm64 subdirectory."
        }
        return $full
    }
    $candidates = @($env:MSYS2_ROOT, "$env:SystemDrive\msys64", "C:\tools\msys64")
    if ($env:LOCALAPPDATA) {
        $candidates += "$env:LOCALAPPDATA\Programs\msys64"
    }
    $shell = Get-Command msys2_shell.cmd -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($shell) { $candidates += Split-Path -Parent $shell.Source }
    foreach ($candidate in $candidates) {
        if ($candidate -and (Test-Path -LiteralPath "$candidate/usr/bin/bash.exe" -PathType Leaf)) {
            return [System.IO.Path]::GetFullPath($candidate)
        }
    }
    return $null
}

function Get-GoStatus {
    param([version]$Minimum, [string]$Architecture)
    $command = Get-Command go -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $command) {
        return [pscustomobject]@{ Ready = $false; Reason = "Go is missing."; Path = $null }
    }
    $text = & $command.Source version
    if ($LASTEXITCODE -ne 0) { throw "Cannot query Go version: $($command.Source)" }
    $match = [regex]::Match(($text -join " "), '\bgo(\d+\.\d+\.\d+)\s')
    if (-not $match.Success -or [version]$match.Groups[1].Value -lt $Minimum) {
        return [pscustomobject]@{ Ready = $false; Reason = "Go $Minimum or newer is required."; Path = $command.Source }
    }
    $hostArch = & $command.Source env GOHOSTARCH
    if ($LASTEXITCODE -ne 0) { throw "Cannot query the Go host architecture." }
    if ($hostArch.Trim() -ne $Architecture) {
        return [pscustomobject]@{ Ready = $false; Reason = "Go must match the native $Architecture architecture."; Path = $command.Source }
    }
    return [pscustomobject]@{ Ready = $true; Reason = ""; Path = $command.Source }
}

function Install-WingetPackage {
    param([string]$Id)
    $winget = Get-Command winget -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $winget) {
        throw "winget is missing. Install/update Microsoft's App Installer, or install $Id manually, then rerun this script."
    }
    Invoke-CheckedNative $winget.Source @("install", "--id", $Id, "--exact", "--source", "winget", "--accept-source-agreements", "--accept-package-agreements")
}

function Invoke-WindowsBuild {
    param([switch]$AllowInstall, [string]$RequestedRoot, [switch]$WithGui)
    if ([System.Environment]::OSVersion.Platform -ne [System.PlatformID]::Win32NT) {
        throw "This script requires Windows. Use build-macos.sh on macOS."
    }
    $repo = Split-Path -Parent $PSScriptRoot
    $target = Get-TargetConfiguration (Get-WindowsArchitecture)
    # Resolve relative user paths before changing into the repository.
    $msys = Find-Msys2Root $RequestedRoot
    $minimumMatch = [regex]::Match((Get-Content -LiteralPath "$repo/go.mod" -Raw), '(?m)^go\s+(\d+\.\d+(?:\.\d+)?)\s*$')
    if (-not $minimumMatch.Success) { throw "Cannot read the minimum Go version from go.mod." }
    $minimum = [version]$minimumMatch.Groups[1].Value

    # Restore the caller's shell even after a failed install, compile or package.
    $names = @("Path", "GOOS", "GOARCH", "CGO_ENABLED", "CC", "PKG_CONFIG", "PKG_CONFIG_PATH", "PKG_CONFIG_LIBDIR", "PKG_CONFIG_SYSROOT_DIR", "GOTOOLCHAIN", "MSYSTEM", "MSYS2_PATH_TYPE", "CHERE_INVOKING")
    $saved = @{}
    foreach ($name in $names) { $saved[$name] = [System.Environment]::GetEnvironmentVariable($name, "Process") }
    $smoke = $null
    $isolated = $null
    Push-Location -LiteralPath $repo
    try {
        $env:GOTOOLCHAIN = "local"
        $env:GOOS = "windows"
        $env:GOARCH = $target.Arch
        $env:CGO_ENABLED = "1"
        $go = Get-GoStatus $minimum $target.Arch
        if (-not $go.Ready) {
            if (-not $AllowInstall) { throw "$($go.Reason) Rerun with -InstallDependencies, or install native Go manually." }
            Install-WingetPackage "GoLang.Go"
            $machinePath = [System.Environment]::GetEnvironmentVariable("Path", "Machine")
            $userPath = [System.Environment]::GetEnvironmentVariable("Path", "User")
            $env:Path = "$env:ProgramFiles\Go\bin;$machinePath;$userPath;$env:Path"
            $go = Get-GoStatus $minimum $target.Arch
            if (-not $go.Ready) { throw "$($go.Reason) Restart the terminal after installation and retry." }
        }
        if (-not $msys) {
            if (-not $AllowInstall) {
                throw "MSYS2 was not found. Rerun with -InstallDependencies, or use -Msys2Root D:\path\to\msys64."
            }
            Install-WingetPackage "MSYS2.MSYS2"
            $msys = Find-Msys2Root ""
            if (-not $msys) { throw "MSYS2 was installed but not located. Rerun with -Msys2Root pointing to its root directory." }
        }
        $prefix = Join-Path $msys $target.Prefix
        $required = @("bin/$($target.Compiler)", "bin/$($target.CxxLinker)", "bin/$($target.Objdump)", "bin/pkg-config.exe", "lib/pkgconfig/libass.pc")
        $missing = @($required | Where-Object { -not (Test-Path -LiteralPath (Join-Path $prefix $_) -PathType Leaf) })
        if ($missing.Count -gt 0) {
            if (-not $AllowInstall) {
                throw "Missing $($target.System) tools: $($missing -join ', '). Rerun with -InstallDependencies to install the matching compiler, pkgconf and libass."
            }
            $env:MSYSTEM = $target.System
            $env:MSYS2_PATH_TYPE = "minimal"
            $env:CHERE_INVOKING = "1"
            $bash = Join-Path $msys "usr/bin/bash.exe"
            # Core MSYS2 updates can require a new shell before the full upgrade.
            Invoke-CheckedNative $bash @("-lc", "pacman -Syu --noconfirm")
            Invoke-CheckedNative $bash @("-lc", "pacman -Syu --noconfirm")
            Invoke-CheckedNative $bash @("-lc", $target.Packages)
        }
        foreach ($relative in $required) {
            if (-not (Test-Path -LiteralPath (Join-Path $prefix $relative) -PathType Leaf)) {
                throw "Required tool is still missing: $prefix/$relative. Finish the MSYS2 installation/update and retry."
            }
        }
        if (-not (Test-Path -LiteralPath "$prefix/lib/libass.a" -PathType Leaf)) {
            throw "Static archive is missing: $prefix/lib/libass.a. Install an MSYS2 libass package containing static libraries; a DLL/import library alone is insufficient."
        }
        $env:Path = "$prefix\bin;$env:Path"
        # Use forward slashes for cgo's command parser, including quoted paths
        # when the MSYS2 installation directory contains spaces.
        $compiler = (Join-Path $prefix "bin/$($target.Compiler)").Replace("\", "/")
        $env:CC = '"' + $compiler + '"'
        $pkgConfig = Join-Path $prefix "bin/pkg-config.exe"
        $link = Get-WindowsLinkConfiguration $pkgConfig -CxxLinker $target.CxxLinker
        $env:PKG_CONFIG = $link.PkgConfig
        $env:PKG_CONFIG_PATH = "$prefix/lib/pkgconfig;$prefix/share/pkgconfig"
        $env:PKG_CONFIG_LIBDIR = $env:PKG_CONFIG_PATH
        Remove-Item Env:PKG_CONFIG_SYSROOT_DIR -ErrorAction SilentlyContinue
        Invoke-CheckedNative $pkgConfig @("--static", "--exists", "libass")
        Write-Host "Building Windows $($target.Arch) (static) with $($go.Path) and $prefix"
        Invoke-CheckedNative $go.Path @("test", "-ldflags", $link.LinkerFlags, "./...")
        Invoke-CheckedNative $go.Path @("vet", "./...")
        $runId = "$(Get-Date -Format 'yyyyMMdd-HHmmss-fff')-$PID"
        $build = Join-Path $repo "build/windows-$($target.Arch)-static"
        $packageName = "pgs-converter-windows-$($target.Arch)-static-$runId"
        $destination = Join-Path $repo "dist/$packageName"
        New-Item -ItemType Directory -Force -Path $build, (Join-Path $repo "dist") | Out-Null
        $binary = Join-Path $build "pgs-converter.exe"
        $version = "dev"
        if ($env:GITHUB_SHA) { $version = $env:GITHUB_SHA }
        $buildFlags = "-X github.com/ninghaofeng/pgs_converter/internal/cli.Version=$version $($link.LinkerFlags)"
        Invoke-CheckedNative $go.Path @("build", "-trimpath", "-ldflags", $buildFlags, "-o", $binary, "./cmd/pgs-converter")
        $guiBinary = $null
        if ($WithGui) {
            $guiBinary = Join-Path $build "pgs-converter-gui.exe"
            Invoke-CheckedNative $go.Path @("build", "-trimpath", "-ldflags", "$buildFlags -H windowsgui", "-o", $guiBinary, "./cmd/pgs-converter-gui")
        }
        New-WindowsBundle -Binary $binary -GuiBinary $guiBinary -Prefix $prefix -Destination $destination -Objdump (Join-Path $prefix "bin/$($target.Objdump)")
        Invoke-CheckedNative (Join-Path $msys "usr/bin/pacman.exe") @("-Q") |
            Set-Content -LiteralPath "$destination/MSYS2-PACKAGES.txt" -Encoding Ascii
        $smoke = Join-Path $build "smoke-$runId.sup"
        $isolated = Join-Path ([System.IO.Path]::GetTempPath()) ("pgs-static-smoke-" + [guid]::NewGuid().ToString("N"))
        New-Item -ItemType Directory -Path $isolated | Out-Null
        Copy-Item -LiteralPath "$destination/pgs-converter.exe" -Destination $isolated
        $buildPath = $env:Path
        Push-Location -LiteralPath $isolated
        try {
            $env:Path = "$env:SystemRoot\System32;$env:SystemRoot"
            foreach ($fixture in @("sample.srt", "animated.ass")) {
                Invoke-CheckedNative "$isolated/pgs-converter.exe" @("convert", "$repo/testdata/$fixture", "--size", "3840x2160", "--fps", "24000/1001", "--overwrite", "-o", $smoke)
            }
        } finally {
            $env:Path = $buildPath
            Pop-Location
        }
        $probe = Get-Command ffprobe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($probe) {
            Invoke-CheckedNative $probe.Source @("-v", "error", "-show_streams", $smoke)
        } else {
            Write-Host "Skipping independent packaged-output probe: ffprobe is not installed."
        }
        Compress-Archive -LiteralPath $destination -DestinationPath "$destination.zip"
        $hash = (Get-FileHash -LiteralPath "$destination.zip" -Algorithm SHA256).Hash.ToLowerInvariant()
        "$hash  $packageName.zip" | Set-Content -LiteralPath "$destination.zip.sha256" -Encoding Ascii
        Write-Host "`nBuild succeeded.`nExecutable: $destination/pgs-converter.exe`nArchive: $destination.zip`nChecksum: $destination.zip.sha256"
        if ($WithGui) {
            Write-Host "Desktop app: $destination/pgs-converter-gui.exe"
        }
    } finally {
        foreach ($name in $names) { [System.Environment]::SetEnvironmentVariable($name, $saved[$name], "Process") }
        Pop-Location
        if ($smoke -and (Test-Path -LiteralPath $smoke)) { Remove-Item -LiteralPath $smoke }
        if ($isolated -and (Test-Path -LiteralPath $isolated)) { Remove-Item -Recurse -LiteralPath $isolated }
    }
}

# Dot-sourcing exposes helpers for tests without starting installers or a build.
if ($MyInvocation.InvocationName -ne ".") {
    try {
        Invoke-WindowsBuild -AllowInstall:$InstallDependencies -RequestedRoot $Msys2Root -WithGui:$Gui
    } catch {
        [System.Console]::Error.WriteLine("Build failed: " + $_.Exception.Message)
        exit 1
    }
}
