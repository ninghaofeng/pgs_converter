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

foreach ($name in @("build-windows.ps1", "bundle-windows.ps1", "test-build-windows.ps1")) {
    $tokens = $null
    $parseErrors = $null
    [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot/$name", [ref]$tokens, [ref]$parseErrors) | Out-Null
    Assert ($parseErrors.Count -eq 0) "PowerShell parse errors in ${name}: $parseErrors"
}

. "$PSScriptRoot/build-windows.ps1"
$x64 = Get-TargetConfiguration "X64"
Assert ($x64.Arch -eq "amd64" -and $x64.Prefix -eq "ucrt64" -and $x64.Compiler -eq "gcc.exe") "Incorrect x64 toolchain."
$arm = Get-TargetConfiguration "Arm64"
Assert ($arm.Arch -eq "arm64" -and $arm.Prefix -eq "clangarm64" -and $arm.Objdump -eq "llvm-objdump.exe") "Incorrect ARM64 toolchain."
Assert-Throws { Get-TargetConfiguration "x86" } "*Unsupported Windows architecture*"

$temp = Join-Path ([System.IO.Path]::GetTempPath()) ("pgs-build-tests-" + [guid]::NewGuid().ToString("N"))
try {
    $root = Join-Path $temp "MSYS2 with spaces"
    New-Item -ItemType Directory -Path "$root/usr/bin" -Force | Out-Null
    New-Item -ItemType File -Path "$root/usr/bin/bash.exe" | Out-Null
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
Write-Host "PowerShell syntax, architecture selection, MSYS2 discovery and failure propagation passed."
exit 0
