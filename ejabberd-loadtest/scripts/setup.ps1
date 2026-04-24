# setup.ps1 - install Go (if missing) and build the loadtest binary on Windows.
# Run from the repo root in PowerShell:
#   powershell -ExecutionPolicy Bypass -File .\scripts\setup.ps1

$ErrorActionPreference = "Stop"

function Log($msg) { Write-Host "[setup] $msg" -ForegroundColor Cyan }
function Warn($msg) { Write-Host "[warn]  $msg" -ForegroundColor Yellow }

Set-Location (Join-Path $PSScriptRoot "..")

function Ensure-Go {
    if (Get-Command go -ErrorAction SilentlyContinue) {
        Log "go found: $(go version)"
        return
    }
    Log "go not found, attempting install via winget..."
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        winget install --id GoLang.Go --silent --accept-package-agreements --accept-source-agreements
        $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + `
                    [System.Environment]::GetEnvironmentVariable("Path", "User")
    } else {
        Warn "winget not available. Install Go manually from https://go.dev/dl/ and re-run."
        exit 1
    }
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        Warn "go still not on PATH. Open a new terminal and re-run."
        exit 1
    }
}

function Tune-Windows {
    Log "tuning Windows TCP limits (may require Administrator)"
    # Expand ephemeral port range.
    try {
        netsh int ipv4 set dynamicport tcp start=10000 num=55535 | Out-Null
        Log "ephemeral ports set to 10000-65535"
    } catch {
        Warn "could not expand dynamic port range (run PowerShell as Administrator for this)"
    }
    # TIME_WAIT reuse has limited knobs on modern Windows; documented as-is.
}

function Build {
    Log "building loadtest.exe"
    go mod tidy
    $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags "-s -w" -o loadtest.exe .\cmd\loadtest
    Log "built: $(Resolve-Path .\loadtest.exe)"
}

Ensure-Go
Tune-Windows
Build
Log "done. run .\scripts\run.ps1 to start."
