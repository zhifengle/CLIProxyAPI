[CmdletBinding()]
param(
    [string]$OutputDir = "bin",
    [string]$BinaryName = "cli-proxy-api.exe",
    [string]$Version = "",
    [ValidateSet("amd64", "arm64")]
    [string]$GoArch = "amd64",
    [switch]$DownloadModels,
    [switch]$Package
)

# build.ps1 - Local Windows build script aligned with the GitHub release workflow.
#
# By default the script builds the Windows binary using the local embedded models catalog.
# Pass -DownloadModels to refresh internal/registry/models/models.json from router-for-me/models.
#
# Examples:
#   ./build.ps1
#   ./build.ps1 -Version v6.0.0-local
#   ./build.ps1 -DownloadModels
#   ./build.ps1 -Package

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Invoke-Git {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    $output = & git @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "git $($Arguments -join ' ') failed.`n$output"
    }
    return ($output | Out-String).Trim()
}

function Write-Info {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Message
    )

    Write-Host "[build] $Message"
}

function Update-ModelsCatalog {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ModelsPath
    )

    Write-Info "Refreshing embedded models catalog from router-for-me/models"

    & git fetch --depth 1 https://github.com/router-for-me/models.git main
    if ($LASTEXITCODE -ne 0) {
        throw "git fetch for models catalog failed."
    }

    $remoteModels = & git show FETCH_HEAD:models.json 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "git show FETCH_HEAD:models.json failed.`n$remoteModels"
    }

    $modelsText = (($remoteModels | ForEach-Object { [string]$_ }) -join "`n")
    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($ModelsPath, $modelsText, $utf8NoBom)

    Write-Info ("Models catalog updated: {0}" -f $ModelsPath)
}

function New-PackageArchive {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepoRoot,
        [Parameter(Mandatory = $true)]
        [string]$BinaryPath,
        [Parameter(Mandatory = $true)]
        [string]$OutputDirectory,
        [Parameter(Mandatory = $true)]
        [string]$BinaryFileName,
        [Parameter(Mandatory = $true)]
        [string]$VersionString,
        [Parameter(Mandatory = $true)]
        [string]$Arch
    )

    $safeVersion = ($VersionString -replace '[\\/:*?"<>|\s]+', '-').Trim('-')
    if ([string]::IsNullOrWhiteSpace($safeVersion)) {
        $safeVersion = "dev"
    }

    $stagingRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("cli-proxy-api-package-" + [Guid]::NewGuid().ToString("N"))
    $packageRoot = Join-Path $stagingRoot "cli-proxy-api"
    New-Item -ItemType Directory -Path $packageRoot -Force | Out-Null

    try {
        Copy-Item -LiteralPath $BinaryPath -Destination (Join-Path $packageRoot $BinaryFileName) -Force

        foreach ($path in @("LICENSE", "README.md", "README_CN.md", "config.example.yaml")) {
            Copy-Item -LiteralPath (Join-Path $RepoRoot $path) -Destination (Join-Path $packageRoot $path) -Force
        }

        $zipPath = Join-Path $OutputDirectory ("cli-proxy-api_{0}_windows_{1}.zip" -f $safeVersion, $Arch)
        if (Test-Path -LiteralPath $zipPath) {
            Remove-Item -LiteralPath $zipPath -Force
        }

        Compress-Archive -Path (Join-Path $packageRoot '*') -DestinationPath $zipPath -CompressionLevel Optimal
        return $zipPath
    }
    finally {
        if (Test-Path -LiteralPath $stagingRoot) {
            Remove-Item -LiteralPath $stagingRoot -Recurse -Force
        }
    }
}

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $scriptRoot

$repoRoot = Invoke-Git -Arguments @("rev-parse", "--show-toplevel")
Set-Location $repoRoot

$modelsPath = Join-Path $repoRoot "internal/registry/models/models.json"

$oldCgoEnabled = $env:CGO_ENABLED
$oldGoos = $env:GOOS
$oldGoarch = $env:GOARCH

try {
    if ($DownloadModels) {
        Update-ModelsCatalog -ModelsPath $modelsPath
        return
    }

    if (-not (Test-Path -LiteralPath $modelsPath)) {
        throw "models.json not found at $modelsPath. Run ./build.ps1 -DownloadModels first."
    }

    if ([string]::IsNullOrWhiteSpace($Version)) {
        $Version = Invoke-Git -Arguments @("describe", "--tags", "--always", "--dirty")
    } else {
        $Version = $Version.Trim()
    }

    $commit = Invoke-Git -Arguments @("rev-parse", "--short", "HEAD")
    $buildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    $outputDirectory = Join-Path $repoRoot $OutputDir
    $binaryPath = Join-Path $outputDirectory $BinaryName

    New-Item -ItemType Directory -Path $outputDirectory -Force | Out-Null

    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = $GoArch

    $ldflags = "-s -w -X 'main.Version=$Version' -X 'main.Commit=$commit' -X 'main.BuildDate=$buildDate'"

    Write-Info ("Building {0}" -f $binaryPath)
    Write-Info ("Version={0} Commit={1} BuildDate={2} GOARCH={3}" -f $Version, $commit, $buildDate, $GoArch)

    $goBuildArgs = @(
        "build",
        "-ldflags=$ldflags",
        "-o",
        $binaryPath,
        "./cmd/server"
    )

    & go @goBuildArgs
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed."
    }

    Write-Info ("Binary ready: {0}" -f $binaryPath)

    if ($Package) {
        $zipPath = New-PackageArchive -RepoRoot $repoRoot -BinaryPath $binaryPath -OutputDirectory $outputDirectory -BinaryFileName $BinaryName -VersionString $Version -Arch $GoArch
        Write-Info ("Package ready: {0}" -f $zipPath)
    }
}
finally {
    $env:CGO_ENABLED = $oldCgoEnabled
    $env:GOOS = $oldGoos
    $env:GOARCH = $oldGoarch
}
