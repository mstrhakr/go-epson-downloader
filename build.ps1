param(
    # Defaults to `git describe`, e.g. v0.1.0 or v0.1.0-3-gabc1234-dirty.
    [string]$Version,
    [ValidateSet('amd64', 'arm64')]
    [string]$Arch = 'amd64'
)
$ErrorActionPreference = 'Stop'
$savedEnv = $env:GOOS, $env:GOARCH
Push-Location $PSScriptRoot
try {
    if (-not $Version) {
        $Version = git describe --tags --always --dirty 2>$null
        if ($LASTEXITCODE -ne 0 -or -not $Version) { $Version = 'dev' }
    }
    Push-Location cmd\epson-downloader
    try {
        go run github.com/tc-hib/go-winres@v0.3.3 make --arch $Arch --product-version $Version --file-version $Version
        if ($LASTEXITCODE -ne 0) { throw 'Resource generation failed.' }
    }
    finally {
        Pop-Location
    }
    $suffix = if ($Arch -eq 'amd64') { '' } else { "-$Arch" }
    $out = ".\bin\EpsonDownloader$suffix.exe"
    New-Item -ItemType Directory -Force -Path (Join-Path $PSScriptRoot 'bin') | Out-Null
    $env:GOOS = 'windows'
    $env:GOARCH = $Arch
    go build -trimpath -ldflags "-H windowsgui -s -w -X main.version=$Version" -o $out ./cmd/epson-downloader
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
    Get-Item $out | Select-Object FullName, Length, @{ n = 'Version'; e = { $Version } }
}
finally {
    $env:GOOS, $env:GOARCH = $savedEnv
    Pop-Location
}
