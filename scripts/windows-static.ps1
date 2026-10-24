# Shared static-link configuration and PE import audit; no build side effects.
function Get-WindowsLinkConfiguration {
    param([string]$PkgConfig, [switch]$UseStatic, [string]$CxxLinker)
    $command = '"' + $PkgConfig.Replace("\", "/") + '"'
    $linkerFlags = ""
    $buildTags = ""
    if ($UseStatic) {
        if ($CxxLinker -notin @("g++.exe", "clang++.exe")) {
            throw "Static linking requires the matching C++ driver (g++.exe or clang++.exe)."
        }
        # The tag enables cgo's --static pkg-config directive and -static
        # link flag. Go's PKG_CONFIG variable accepts an executable, not args.
        $buildTags = "pgs_static"
        # Graphite2/HarfBuzz contain C++ code. The C++ driver adds its runtime
        # after the dependency archives; CC remains the C compiler for cgo.
        $linkerFlags = "-linkmode=external -extld=$CxxLinker -extldflags=-static"
    }
    return [pscustomobject]@{
        PkgConfig = $command
        LinkerFlags = $linkerFlags
        BuildTags = $buildTags
    }
}

function Assert-WindowsStaticImports {
    param([string[]]$Lines)
    # Deliberately do not trust every DLL found in System32: third-party
    # installers can place redistributable runtimes there as well.
    $systemDlls = @(
        "advapi32.dll", "bcrypt.dll", "bcryptprimitives.dll", "cfgmgr32.dll",
        "combase.dll", "comdlg32.dll", "crypt32.dll", "cryptbase.dll",
        "dnsapi.dll", "dwmapi.dll", "dwrite.dll", "gdi32.dll", "imm32.dll",
        "iphlpapi.dll", "kernel32.dll", "kernelbase.dll", "msimg32.dll",
        "msvcrt.dll", "mswsock.dll", "ncrypt.dll", "netapi32.dll",
        "normaliz.dll", "ntdll.dll", "ole32.dll", "oleaut32.dll",
        "powrprof.dll", "profapi.dll", "propsys.dll", "psapi.dll",
        "rpcrt4.dll", "secur32.dll", "setupapi.dll", "shell32.dll",
        "shlwapi.dll", "ucrtbase.dll", "user32.dll", "userenv.dll",
        "usp10.dll", "uxtheme.dll", "version.dll", "winhttp.dll",
        "wininet.dll", "winmm.dll", "winspool.drv", "wintrust.dll",
        "ws2_32.dll", "wtsapi32.dll"
    )
    $imports = @(
        foreach ($line in $Lines) {
            if ($line -match 'DLL Name:\s*(\S+)') { $Matches[1] }
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
