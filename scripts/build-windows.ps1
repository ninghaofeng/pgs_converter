<#
.SYNOPSIS
Build, test and package the native Windows CLI without mandatory path arguments.
.DESCRIPTION
By default, only checks for dependencies. -InstallDependencies permits winget
to install Go/MSYS2 and pacman to install the matching native C toolchain/libass.
Installers may request elevation. Existing packages are never deleted.
-Static links third-party native libraries into the EXE and rejects remaining
non-system DLL imports. Static redistribution requires a separate license review.
.EXAMPLE
.\scripts\build-windows.ps1
.EXAMPLE
.\scripts\build-windows.ps1 -InstallDependencies
.EXAMPLE
.\scripts\build-windows.ps1 -Msys2Root D:\tools\msys64
.EXAMPLE
.\scripts\build-windows.ps1 -Static
#>
[CmdletBinding()]
param(
    [switch]$InstallDependencies,
    [string]$Msys2Root,
    [switch]$Static
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
. "$PSScriptRoot/windows-static.ps1"

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
    $shell = Get-Command msys2_shell.cmd -CommandType Application -ErrorAction SilentlyContinue
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
    $command = Get-Command go -CommandType Application -ErrorAction SilentlyContinue
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
    $winget = Get-Command winget -CommandType Application -ErrorAction SilentlyContinue
    if (-not $winget) {
        throw "winget is missing. Install/update Microsoft's App Installer, or install $Id manually, then rerun this script."
    }
    Invoke-CheckedNative $winget.Source @("install", "--id", $Id, "--exact", "--source", "winget", "--accept-source-agreements", "--accept-package-agreements")
}

function Invoke-WindowsBuild {
    param([switch]$AllowInstall, [string]$RequestedRoot, [switch]$UseStatic)
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
        $required = @("bin/$($target.Compiler)", "bin/$($target.Objdump)", "bin/pkg-config.exe", "lib/pkgconfig/libass.pc")
        if ($UseStatic) { $required += "bin/$($target.CxxLinker)" }
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
            if ($UseStatic -and -not (Test-Path -LiteralPath "$prefix/lib/libass.a" -PathType Leaf)) {
                throw "Static archive is missing: $prefix/lib/libass.a. Install an MSYS2 libass package containing static libraries; a DLL/import library alone is insufficient."
            }
        }
        $env:Path = "$prefix\bin;$env:Path"
        # Use forward slashes for cgo's command parser, including quoted paths
        # when the MSYS2 installation directory contains spaces.
        $compiler = (Join-Path $prefix "bin/$($target.Compiler)").Replace("\", "/")
        $env:CC = '"' + $compiler + '"'
        $pkgConfig = Join-Path $prefix "bin/pkg-config.exe"
        $link = Get-WindowsLinkConfiguration $pkgConfig -UseStatic:$UseStatic -CxxLinker $target.CxxLinker
        $env:PKG_CONFIG = $link.PkgConfig
        $env:PKG_CONFIG_PATH = "$prefix/lib/pkgconfig;$prefix/share/pkgconfig"
        $env:PKG_CONFIG_LIBDIR = $env:PKG_CONFIG_PATH
        Remove-Item Env:PKG_CONFIG_SYSROOT_DIR -ErrorAction SilentlyContinue
        $pkgArgs = @("--exists", "libass")
        if ($UseStatic) { $pkgArgs = @("--static") + $pkgArgs }
        Invoke-CheckedNative $pkgConfig $pkgArgs
        $mode = "shared"
        if ($UseStatic) { $mode = "static" }
        Write-Host "Building Windows $($target.Arch) ($mode) with $($go.Path) and $prefix"
        $testArgs = @("test")
        if ($UseStatic) { $testArgs += @("-tags", $link.BuildTags, "-ldflags", $link.LinkerFlags) }
        Invoke-CheckedNative $go.Path ($testArgs + "./...")
        $vetArgs = @("vet")
        if ($UseStatic) { $vetArgs += @("-tags", $link.BuildTags) }
        Invoke-CheckedNative $go.Path ($vetArgs + "./...")
        $runId = "$(Get-Date -Format 'yyyyMMdd-HHmmss-fff')-$PID"
        $build = Join-Path $repo "build/windows-$($target.Arch)"
        $packageName = "pgs-converter-windows-$($target.Arch)-$runId"
        if ($UseStatic) {
            $build += "-static"
            $packageName = "pgs-converter-windows-$($target.Arch)-static-$runId"
        }
        $destination = Join-Path $repo "dist/$packageName"
        New-Item -ItemType Directory -Force -Path $build, (Join-Path $repo "dist") | Out-Null
        $binary = Join-Path $build "pgs-converter.exe"
        $version = "dev"
        if ($env:GITHUB_SHA) { $version = $env:GITHUB_SHA }
        $buildFlags = "-X github.com/ninghaofeng/pgs_converter/internal/cli.Version=$version"
        if ($UseStatic) { $buildFlags += " $($link.LinkerFlags)" }
        $buildArgs = @("build", "-trimpath")
        if ($UseStatic) { $buildArgs += @("-tags", $link.BuildTags) }
        Invoke-CheckedNative $go.Path ($buildArgs + @("-ldflags", $buildFlags, "-o", $binary, "./cmd/pgs-converter"))
        & "$PSScriptRoot/bundle-windows.ps1" -Binary $binary -Prefix $prefix -Destination $destination -Objdump (Join-Path $prefix "bin/$($target.Objdump)") -Static:$UseStatic
        if ($UseStatic) {
            Invoke-CheckedNative (Join-Path $msys "usr/bin/pacman.exe") @("-Q") |
                Set-Content -LiteralPath "$destination/MSYS2-PACKAGES.txt" -Encoding Ascii
        }
        $smoke = Join-Path $build "smoke-$runId.sup"
        if ($UseStatic) {
            $isolated = Join-Path ([System.IO.Path]::GetTempPath()) ("pgs-static-smoke-" + [guid]::NewGuid().ToString("N"))
            New-Item -ItemType Directory -Path $isolated | Out-Null
            Copy-Item -LiteralPath "$destination/pgs-converter.exe" -Destination $isolated
            $buildPath = $env:Path
            Push-Location -LiteralPath $isolated
            try {
                $env:Path = "$env:SystemRoot\System32;$env:SystemRoot"
                foreach ($fixture in @("sample.srt", "animated.ass")) {
                    Invoke-CheckedNative "$isolated/pgs-converter.exe" @("convert", "$repo/testdata/$fixture", "--size", "1280x720", "--fps", "24000/1001", "--overwrite", "-o", $smoke)
                }
            } finally {
                $env:Path = $buildPath
                Pop-Location
            }
        } else {
            Invoke-CheckedNative "$destination/pgs-converter.exe" @("convert", "$repo/testdata/sample.srt", "--size", "1280x720", "--fps", "24", "-o", $smoke)
        }
        $probe = Get-Command ffprobe -CommandType Application -ErrorAction SilentlyContinue
        if ($probe) {
            Invoke-CheckedNative $probe.Source @("-v", "error", "-show_streams", $smoke)
        } else {
            Write-Host "Skipping independent packaged-output probe: ffprobe is not installed."
        }
        Compress-Archive -LiteralPath $destination -DestinationPath "$destination.zip"
        $hash = (Get-FileHash -LiteralPath "$destination.zip" -Algorithm SHA256).Hash.ToLowerInvariant()
        "$hash  $packageName.zip" | Set-Content -LiteralPath "$destination.zip.sha256" -Encoding Ascii
        Write-Host "`nBuild succeeded.`nExecutable: $destination/pgs-converter.exe`nArchive: $destination.zip`nChecksum: $destination.zip.sha256"
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
        Invoke-WindowsBuild -AllowInstall:$InstallDependencies -RequestedRoot $Msys2Root -UseStatic:$Static
    } catch {
        [System.Console]::Error.WriteLine("Build failed: " + $_.Exception.Message)
        exit 1
    }
}
