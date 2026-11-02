<#
.SYNOPSIS
  Builds ssmygo.exe.

.DESCRIPTION
  Checks the code (go vet), runs the tests, and builds a windowed exe with
  no cgo. If build\ssmygo.exe is running, the new exe is put beside it: the
  running file is renamed to ssmygo.old.exe (Windows allows renaming a
  running exe), so you only need to restart ssmygo.

.PARAMETER Arch
  amd64 (default: ordinary 64-bit PCs), arm64, or all (both, with the
  architecture in the file name).

.PARAMETER SkipTests
  Skip go vet and the tests (faster; for trying out UI changes).

.PARAMETER Version
  Version shown by the app; defaults to "version" in mygo.json.

.EXAMPLE
  .\build.ps1
  .\build.ps1 -SkipTests
  .\build.ps1 -Arch all -Version 0.2.0
#>
[CmdletBinding()]
param(
    [ValidateSet('amd64', 'arm64', 'all')]
    [string]$Arch = 'amd64',
    [switch]$SkipTests,
    [string]$Version
)

$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot

function Step($text) { Write-Host "==> $text" -ForegroundColor Cyan }
function Fail($text) { Write-Host "Build failed: $text" -ForegroundColor Red; exit 1 }

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Fail 'Go is not installed or not on PATH (https://go.dev/dl/).'
}

if (-not $Version) {
    $Version = (Get-Content -Raw -LiteralPath 'mygo.json' | ConvertFrom-Json).version
}

# Builds never need cgo; tests run on this machine's own architecture.
$env:CGO_ENABLED = '0'
Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue

if (-not $SkipTests) {
    Step 'go vet'
    go vet ./...
    if ($LASTEXITCODE -ne 0) { Fail 'go vet reported problems.' }

    Step 'tests (ssmygo)'
    go test -count=1 ./...
    if ($LASTEXITCODE -ne 0) { Fail 'tests failed.' }

    Step 'tests (bundled shadowsocks-go: the patched packages)'
    Push-Location third_party/shadowsocks-go
    try {
        go test -count=1 ./service ./direct ./clientgroup
        if ($LASTEXITCODE -ne 0) { Fail 'shadowsocks-go tests failed.' }
    } finally { Pop-Location }
}

New-Item -ItemType Directory -Force -Path build | Out-Null
# mygo build (MyGo's own builder, a Go tool in go.mod) embeds the icon from
# mygo.json ("icon"), the manifest and the version information, and links
# the version; MYGO_LDFLAGS carries an explicit -Version.
$env:MYGO_LDFLAGS = "-X main.version=$Version"
$archs = if ($Arch -eq 'all') { @('amd64', 'arm64') } else { @($Arch) }

foreach ($a in $archs) {
    $name = if ($Arch -eq 'all') { "ssmygo-$a.exe" } else { 'ssmygo.exe' }
    $target = Join-Path 'build' $name
    $out = Join-Path 'build' "windows-$a"
    $new = Join-Path $out 'ssmygo.exe'

    Step "build $name (windows/$a, version $Version, with icon)"
    Remove-Item -LiteralPath $out -Recurse -Force -ErrorAction SilentlyContinue
    go tool mygo build -platform "windows/$a" -skip-installer
    if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $new)) { Fail "mygo build for $a failed." }

    # Put the new exe in place, beside a running copy if there is one.
    if (Test-Path -LiteralPath $target) {
        $old = [IO.Path]::ChangeExtension($target, '.old.exe')
        Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
        if (Test-Path -LiteralPath $old) {
            # An older ssmygo still runs from it: a running exe cannot be
            # deleted but can be renamed. Delete these files later.
            $aside = [IO.Path]::ChangeExtension($old, ('.{0:yyyyMMdd-HHmmss}.exe' -f (Get-Date)))
            Move-Item -LiteralPath $old -Destination $aside
            Write-Host "    $old is still running; moved aside to $aside" -ForegroundColor Yellow
        }
        Move-Item -LiteralPath $target -Destination $old
    }
    Move-Item -LiteralPath $new -Destination $target
    Remove-Item -LiteralPath $out -Recurse -Force -ErrorAction SilentlyContinue

    $mb = '{0:N2}' -f ((Get-Item -LiteralPath $target).Length / 1MB)
    Write-Host "    $target  $mb MB" -ForegroundColor Green
}

if (Get-Process -Name ssmygo -ErrorAction SilentlyContinue) {
    Write-Host 'ssmygo is running the previous build: quit it from the tray and start build\ssmygo.exe.' -ForegroundColor Yellow
}
Write-Host 'Done.' -ForegroundColor Green
