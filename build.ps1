[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

$projectRoot = $PSScriptRoot
$outputDirectory = Join-Path $projectRoot "bin"
$outputPath = Join-Path $outputDirectory "proxy-subs-backend.exe"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Go was not found in PATH. Install Go and reopen PowerShell before building."
}

New-Item -ItemType Directory -Path $outputDirectory -Force | Out-Null

$previousGOOS = [Environment]::GetEnvironmentVariable("GOOS", "Process")
$previousGOARCH = [Environment]::GetEnvironmentVariable("GOARCH", "Process")
$previousCGOEnabled = [Environment]::GetEnvironmentVariable("CGO_ENABLED", "Process")

Push-Location $projectRoot
try {
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    $env:CGO_ENABLED = "0"

    Write-Host "Building Windows amd64 executable..."
    & go build -trimpath -ldflags="-s -w" -o $outputPath .
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
}
finally {
    if ($null -eq $previousGOOS) {
        Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    }
    else {
        $env:GOOS = $previousGOOS
    }

    if ($null -eq $previousGOARCH) {
        Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
    }
    else {
        $env:GOARCH = $previousGOARCH
    }

    if ($null -eq $previousCGOEnabled) {
        Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
    }
    else {
        $env:CGO_ENABLED = $previousCGOEnabled
    }

    Pop-Location
}

$executable = Get-Item -LiteralPath $outputPath
Write-Host "Build completed: $($executable.FullName) ($([Math]::Round($executable.Length / 1MB, 2)) MB)"
