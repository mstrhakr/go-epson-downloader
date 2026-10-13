param([string]$Version = 'dev')
$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    go generate ./cmd/epson-downloader
    if ($LASTEXITCODE -ne 0) { throw 'Manifest generation failed.' }
    New-Item -ItemType Directory -Force -Path (Join-Path $PSScriptRoot 'bin') | Out-Null
    go build -trimpath -ldflags "-H windowsgui -s -w -X main.version=$Version" -o .\bin\EpsonDownloader.exe ./cmd/epson-downloader
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
    Get-Item .\bin\EpsonDownloader.exe | Select-Object FullName, Length
}
finally {
    Pop-Location
}
