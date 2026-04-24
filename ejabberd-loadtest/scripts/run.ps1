# run.ps1 - start the loadtest against the default ejabberd server.
# Override any env var before calling, e.g.:
#   $env:TARGET = 5000; $env:RAMP = 500; .\scripts\run.ps1

$ErrorActionPreference = "Stop"

Set-Location (Join-Path $PSScriptRoot "..")

if (-not (Test-Path .\loadtest.exe)) {
    Write-Host "binary not found, running setup..." -ForegroundColor Yellow
    powershell -ExecutionPolicy Bypass -File .\scripts\setup.ps1
}

if (-not $env:HOST)            { $env:HOST = "testqa.tripleenableverified.com" }
if (-not $env:PORT)            { $env:PORT = "5222" }
if (-not $env:DOMAIN)          { $env:DOMAIN = "testqa.tripleenableverified.com" }
if (-not $env:REG_URL)         { $env:REG_URL = "https://$($env:HOST):5443/api/register" }
if (-not $env:USER_PREFIX)     { $env:USER_PREFIX = "lt" }
if (-not $env:PASSWORD)        { $env:PASSWORD = "loadtest" }
if (-not $env:TARGET)          { $env:TARGET = "1000" }
if (-not $env:RAMP)            { $env:RAMP = "100" }
if (-not $env:MSG_INTERVAL_MS) { $env:MSG_INTERVAL_MS = "5000" }
if (-not $env:PAIR)            { $env:PAIR = "true" }
if (-not $env:SKIP_REGISTER)   { $env:SKIP_REGISTER = "false" }
if (-not $env:REG_INSECURE)    { $env:REG_INSECURE = "true" }
if (-not $env:CSV)             { $env:CSV = "loadtest-report-$(Get-Date -Format yyyyMMdd-HHmmss).csv" }

$argsList = @(
    "-host", $env:HOST,
    "-port", $env:PORT,
    "-domain", $env:DOMAIN,
    "-user-prefix", $env:USER_PREFIX,
    "-password", $env:PASSWORD,
    "-target", $env:TARGET,
    "-ramp", $env:RAMP,
    "-msg-interval-ms", $env:MSG_INTERVAL_MS,
    "-register-mode", "http",
    "-register-url", $env:REG_URL,
    "-register-insecure=$($env:REG_INSECURE)",
    "-register-api-user", ($env:REG_API_USER, "" -ne $null)[0],
    "-register-api-pass", ($env:REG_API_PASS, "" -ne $null)[0],
    "-csv", $env:CSV
)

if ($env:HEADLESS -eq "1") { $argsList += "-headless" }
if ($env:DURATION)         { $argsList += @("-duration", $env:DURATION) }
if ($env:SKIP_REGISTER -eq "true") { $argsList += "-skip-register" }
if ($env:PAIR -ne "true")  { $argsList += "-pair=false" }

& .\loadtest.exe @argsList
