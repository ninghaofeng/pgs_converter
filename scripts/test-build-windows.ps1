# Framework-free helper tests also run under PowerShell on macOS.
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Assert {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Assert-Throws {
    param([scriptblock]$Action, [string]$Pattern)
    $message = $null
    try { & $Action | Out-Null } catch { $message = $_.Exception.Message }
    Assert ($null -ne $message -and $message -like $Pattern) "Expected error matching '$Pattern', got '$message'."
}

foreach ($name in @("build-windows.ps1", "bundle-windows.ps1", "windows-static.ps1", "test-build-windows.ps1")) {
    $tokens = $null
    $parseErrors = $null
    [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot/$name", [ref]$tokens, [ref]$parseErrors) | Out-Null
    Assert ($parseErrors.Count -eq 0) "PowerShell parse errors in ${name}: $parseErrors"
}

. "$PSScriptRoot/build-windows.ps1"
$pkgConfigPath = "C:\MSYS2 with spaces\ucrt64\bin\pkg-config.exe"
$sharedLink = Get-WindowsLinkConfiguration $pkgConfigPath
$staticLink = Get-WindowsLinkConfiguration $pkgConfigPath -UseStatic
Assert ($sharedLink.PkgConfig -eq '"C:/MSYS2 with spaces/ucrt64/bin/pkg-config.exe"') "pkg-config executable path was not quoted."
Assert ($sharedLink.LinkerFlags -eq "" -and $sharedLink.BuildTags -eq "") "Shared linking gained static flags."
Assert ($staticLink.PkgConfig -eq $sharedLink.PkgConfig) "PKG_CONFIG must not contain arguments that Go would discard."
Assert ($staticLink.BuildTags -eq "pgs_static") "Static cgo directives were not enabled."
Assert ($staticLink.LinkerFlags -eq "-linkmode=external -extldflags=-static") "Static external linker flags are missing."
$allowedImports = @(
    "The Import Tables",
    "    DLL Name: KERNEL32.dll",
    "    DLL Name: DWrite.dll",
    "    DLL Name: ucrtbase.dll",
    "    DLL Name: api-ms-win-crt-runtime-l1-1-0.dll",
    "    DLL Name: ext-ms-win-ntuser-window-l1-1-0.dll"
)
$audited = @(Assert-WindowsStaticImports $allowedImports)
Assert ($audited.Count -eq 5) "System imports were not retained in audit output."
foreach ($dll in @("libass-9.dll", "libfreetype-6.dll", "libwinpthread-1.dll", "libgcc_s_seh-1.dll", "libstdc++-6.dll", "msvcp140.dll", "unknown.dll", "../kernel32.dll")) {
    Assert-Throws { Assert-WindowsStaticImports ($allowedImports + "DLL Name: $dll") } "*non-allowlisted DLL*"
}
Assert-Throws { Assert-WindowsStaticImports @("not a PE import table") } "*no PE DLL imports*"
$x64 = Get-TargetConfiguration "X64"
Assert ($x64.Arch -eq "amd64" -and $x64.Prefix -eq "ucrt64" -and $x64.Compiler -eq "gcc.exe") "Incorrect x64 toolchain."
$arm = Get-TargetConfiguration "Arm64"
Assert ($arm.Arch -eq "arm64" -and $arm.Prefix -eq "clangarm64" -and $arm.Objdump -eq "llvm-objdump.exe") "Incorrect ARM64 toolchain."
Assert-Throws { Get-TargetConfiguration "x86" } "*Unsupported Windows architecture*"

$savedArchitecture = $env:PROCESSOR_ARCHITECTURE
$savedNativeArchitecture = $env:PROCESSOR_ARCHITEW6432
try {
    foreach ($case in @(
        @{ Process = "AMD64"; Native = $null; Expected = "x64" },
        @{ Process = "ARM64"; Native = $null; Expected = "arm64" },
        @{ Process = "x86"; Native = "AMD64"; Expected = "x64" },
        @{ Process = "AMD64"; Native = "ARM64"; Expected = "arm64" },
        @{ Process = "x86"; Native = "ARM64"; Expected = "arm64" }
    )) {
        $env:PROCESSOR_ARCHITECTURE = $case.Process
        $env:PROCESSOR_ARCHITEW6432 = $case.Native
        $detected = Get-WindowsArchitecture
        Assert ($detected -eq $case.Expected) "Incorrect native architecture for $($case.Process)/$($case.Native): $detected."
        $configuration = Get-TargetConfiguration $detected
        Assert ($configuration.Arch -in @("amd64", "arm64")) "Native architecture did not select a supported toolchain."
    }
    $env:PROCESSOR_ARCHITEW6432 = $null
    $env:PROCESSOR_ARCHITECTURE = "x86"
    Assert-Throws { Get-WindowsArchitecture } "*Unsupported Windows architecture*"
    $env:PROCESSOR_ARCHITECTURE = $null
    Assert-Throws { Get-WindowsArchitecture } "*Cannot detect the native Windows architecture*"
} finally {
    [System.Environment]::SetEnvironmentVariable("PROCESSOR_ARCHITECTURE", $savedArchitecture, "Process")
    [System.Environment]::SetEnvironmentVariable("PROCESSOR_ARCHITEW6432", $savedNativeArchitecture, "Process")
}

$temp = Join-Path ([System.IO.Path]::GetTempPath()) ("pgs-build-tests-" + [guid]::NewGuid().ToString("N"))
try {
    $root = Join-Path $temp "MSYS2 with spaces"
    New-Item -ItemType Directory -Path "$root/usr/bin" -Force | Out-Null
    New-Item -ItemType File -Path "$root/usr/bin/bash.exe" | Out-Null
    $binary = Join-Path $temp "input.exe"
    New-Item -ItemType File -Path $binary | Out-Null
    $objdump = Join-Path $temp "mock-objdump.ps1"
    @'
param([string]$Mode, [string]$Binary)
"    DLL Name: libass-9.dll"
$global:LASTEXITCODE = 0
'@ | Set-Content -LiteralPath $objdump -Encoding Ascii
    $rejectedBundle = Join-Path $temp "rejected-static-bundle"
    Assert-Throws {
        & "$PSScriptRoot/bundle-windows.ps1" -Binary $binary -Prefix $root -Destination $rejectedBundle -Objdump $objdump -Static
    } "*non-allowlisted DLL*"
    Assert (-not (Test-Path -LiteralPath "$rejectedBundle/libass-9.dll")) "Static bundler copied a DLL instead of failing."
    @'
param([string]$Mode, [string]$Binary)
$global:LASTEXITCODE = 7
'@ | Set-Content -LiteralPath $objdump -Encoding Ascii
    Assert-Throws {
        & "$PSScriptRoot/bundle-windows.ps1" -Binary $binary -Prefix $root -Destination "$temp/failed-inspection" -Objdump $objdump -Static
    } "*Cannot inspect imports*"
    Assert ((Find-Msys2Root $root) -eq [System.IO.Path]::GetFullPath($root)) "MSYS2 paths with spaces must remain intact."
    Assert-Throws { Find-Msys2Root "$root/ucrt64" } "*Invalid MSYS2 root*"
    $hostArch = & go env GOHOSTARCH
    Assert ($LASTEXITCODE -eq 0) "Cannot inspect test Go architecture."
    $go = Get-GoStatus ([version]"1.24.0") $hostArch.Trim()
    Assert $go.Ready "Installed native Go was not detected."
    $old = Get-GoStatus ([version]"999.0.0") $hostArch.Trim()
    Assert (-not $old.Ready) "An insufficient Go version must not pass preflight."
    $wrongArch = Get-GoStatus ([version]"1.24.0") "unsupported-test-architecture"
    Assert (-not $wrongArch.Ready) "An architecture mismatch must not pass preflight."
    $shell = (Get-Process -Id $PID).Path
    Assert-Throws { Invoke-CheckedNative $shell @("-NoProfile", "-NonInteractive", "-Command", "exit 7") } "*exit code 7*"
} finally {
    if (Test-Path -LiteralPath $temp) { Remove-Item -Recurse -LiteralPath $temp }
}
Write-Host "PowerShell syntax, architecture selection, static-link configuration/import audit, MSYS2 discovery and failure propagation passed."
exit 0
