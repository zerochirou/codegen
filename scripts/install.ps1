$ErrorActionPreference = "Stop"

$RepoPkg = "github.com/zerochirou/codegen/cmd/codegen@latest"
$InstallDir = if ($env:CODEGEN_INSTALL_DIR) { $env:CODEGEN_INSTALL_DIR } else { Join-Path $HOME ".local\bin" }
$GoInstallDir = if ($env:CODEGEN_GO_INSTALL_DIR) { $env:CODEGEN_GO_INSTALL_DIR } else { Join-Path $HOME ".local\go" }
$FallbackVersion = "go1.26.3"

function Find-GoBin {
    $cmd = Get-Command go -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    foreach ($candidate in @(
        (Join-Path $GoInstallDir "bin\go.exe"),
        "C:\Program Files\Go\bin\go.exe"
    )) {
        if (Test-Path $candidate) { return $candidate }
    }
    return $null
}

function Test-GoVersionOk($GoBin) {
    $out = & $GoBin version 2>$null
    if ($out -match 'go(\d+)\.(\d+)') {
        $maj = [int]$Matches[1]
        $min = [int]$Matches[2]
        return ($maj -gt 1) -or ($maj -eq 1 -and $min -ge 24)
    }
    return $false
}

function Confirm-InstallGo($Message) {
    $override = $env:CODEGEN_AUTO_INSTALL_GO
    if ($override -match '^(1|y|yes|true)$') { return $true }
    if ($override -match '^(0|n|no|false)$') { return $false }
    $ans = Read-Host "$Message Install Go automatically now? [y/N]"
    return ($ans -match '^(y|yes)$')
}

function Add-ToPath($Dir) {
    $env:PATH = "$Dir;$env:PATH"
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (-not ($userPath -split ';' | Where-Object { $_ -eq $Dir })) {
        $nextPath = if ($userPath) { "$Dir;$userPath" } else { $Dir }
        [Environment]::SetEnvironmentVariable("Path", $nextPath, "User")
        Write-Host "==> Added $Dir to User PATH"
    }
}

function Install-GoToolchain {
    $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
        "ARM64" { "arm64" }
        "x86"   { "386" }
        default { "amd64" }
    }
    $goVer = try {
        ((Invoke-RestMethod -Uri "https://go.dev/VERSION?m=text" -UseBasicParsing) -split "`n")[0].Trim()
    } catch {
        $FallbackVersion
    }
    if ($goVer -notmatch '^go\d') { $goVer = $FallbackVersion }

    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("codegen-go-" + [guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null
    try {
        $zipPath = Join-Path $tmpDir "$goVer.windows-$arch.zip"
        $url = "https://go.dev/dl/$goVer.windows-$arch.zip"
        Write-Host "==> Downloading $goVer (windows/$arch)..."
        Invoke-WebRequest -Uri $url -OutFile $zipPath -UseBasicParsing
        if (Test-Path $GoInstallDir) { Remove-Item -Recurse -Force $GoInstallDir }
        $extractDir = Join-Path $tmpDir "extract"
        Expand-Archive -Path $zipPath -DestinationPath $extractDir -Force
        Move-Item -Path (Join-Path $extractDir "go") -Destination $GoInstallDir
    } finally {
        Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
    }
    Add-ToPath (Join-Path $GoInstallDir "bin")
}

Write-Host "==> Checking Go toolchain..."
$goBin = Find-GoBin
if (-not $goBin) {
    if (Confirm-InstallGo "Go (>= 1.24) is not installed.") {
        Install-GoToolchain
        $goBin = Find-GoBin
    } else {
        Write-Host "==> Cancelled. Install Go (>= 1.24) from https://go.dev/dl/ and run again."
        exit 0
    }
} elseif (-not (Test-GoVersionOk $goBin)) {
    $cur = & $goBin version 2>$null
    if (Confirm-InstallGo "Found outdated Go ($cur); >= 1.24 is required.") {
        Install-GoToolchain
        $goBin = Find-GoBin
    } else {
        Write-Host "==> Cancelled. Please upgrade Go to >= 1.24 from https://go.dev/dl/."
        exit 0
    }
} else {
    Add-ToPath (Split-Path -Parent $goBin)
}

Write-Host "==> Installing codegen CLI from $RepoPkg..."
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$env:GOBIN = $InstallDir
& $goBin install $RepoPkg
Add-ToPath $InstallDir

Write-Host ""
Write-Host "codegen installed successfully at: $(Join-Path $InstallDir 'codegen.exe')"
Write-Host ""
Write-Host "Next step: run 'codegen init' inside your project (or choose Global scope)."
