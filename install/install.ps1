# gate4.ai installer for Windows (PowerShell 5.1 and 7, no admin rights).
#
#   irm https://gate4.ai/install.ps1 | iex
#
# Options (environment variables):
#   $env:GATE4AI_VERSION = 'v0.1.1'   install a specific version (default: latest)
#   $env:GATE4AI_INSTALL_DIR = 'DIR'  install folder (default: %LOCALAPPDATA%\Programs\gate4ai)
#   $env:GATE4AI_NO_TRAY = '1'        do not install the tray icon app
#   $env:GATE4AI_START = '1'          start the daemon now and at every login
#                                     (gate4ai service install: HKCU Run key)
#
# Downloads the release from GitHub, checks sha256 checksums and only then
# installs. Never touches your configuration or data, and starts the daemon
# only with GATE4AI_START=1.
#
# The whole script is one function called on the last line: a download cut
# short by the network does not run half of it.

function Install-Gate4ai {
    [CmdletBinding()]
    param()

    Set-StrictMode -Version 3
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $repo = 'gate4ai/gate4ai'
    $site = if ($env:GATE4AI_SITE) { $env:GATE4AI_SITE } else { 'https://gate4.ai' }
    $base = if ($env:GATE4AI_DOWNLOAD_BASE) { $env:GATE4AI_DOWNLOAD_BASE } else { "https://github.com/$repo/releases/download" }
    $releases = "https://github.com/$repo/releases"
    $dir = if ($env:GATE4AI_INSTALL_DIR) { $env:GATE4AI_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\gate4ai' }
    $noTray = [bool]$env:GATE4AI_NO_TRAY
    $start = [bool]$env:GATE4AI_START

    $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
        'AMD64' { 'amd64' }
        'ARM64' { 'arm64' }
        default { throw "gate4ai install: unsupported CPU $($env:PROCESSOR_ARCHITECTURE). Download manually: $releases" }
    }

    $tagPattern = '^v[0-9][A-Za-z0-9.-]*$'
    $tag = $env:GATE4AI_VERSION
    if (-not $tag) {
        try { $tag = (Invoke-RestMethod -Uri "$site/latest" -TimeoutSec 15 -UseBasicParsing).ToString().Trim() } catch { $tag = '' }
        if ($tag -notmatch $tagPattern) {
            try { $tag = (Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -TimeoutSec 15 -UseBasicParsing).tag_name } catch { $tag = '' }
        }
    }
    if ($tag -and -not $tag.StartsWith('v')) { $tag = "v$tag" }
    if ($tag -notmatch $tagPattern) {
        throw "gate4ai install: could not find the latest version; set `$env:GATE4AI_VERSION = 'v0.1.1'"
    }
    $ver = $tag.Substring(1)

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("gate4ai-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Host "Installing gate4ai $tag for windows/$arch"

        function Get-File([string]$name) {
            $out = Join-Path $tmp $name
            try { Invoke-WebRequest -Uri "$base/$tag/$name" -OutFile $out -UseBasicParsing }
            catch { throw "gate4ai install: cannot download $name (is $tag released for windows/$arch? $releases)" }
            return $out
        }

        # Download and verify everything before installing anything.
        $sums = Get-Content -LiteralPath (Get-File 'checksums.txt')
        $archives = @("gate4ai_${ver}_windows_${arch}.zip")
        if (-not $noTray) { $archives += "gate4ai-tray_${ver}_windows_${arch}.zip" }
        foreach ($a in $archives) {
            $path = Get-File $a
            $want = $null
            foreach ($line in $sums) {
                $parts = $line -split '\s+', 2
                if ($parts.Count -eq 2 -and $parts[1].TrimStart('*') -eq $a) { $want = $parts[0].ToLowerInvariant() }
            }
            if (-not $want) { throw "gate4ai install: $a is not listed in checksums.txt" }
            $got = (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant()
            if ($got -ne $want) { throw "gate4ai install: checksum of $a does NOT match; nothing was installed" }
            Write-Host "  ${a}: checksum ok"
            Expand-Archive -LiteralPath $path -DestinationPath (Join-Path $tmp ($a -replace '\.zip$', '')) -Force
        }

        $files = @(@{ Src = Join-Path $tmp "gate4ai_${ver}_windows_${arch}\gate4ai.exe"; Name = 'gate4ai.exe' })
        if (-not $noTray) {
            $files += @{ Src = Join-Path $tmp "gate4ai-tray_${ver}_windows_${arch}\gate4ai-tray.exe"; Name = 'gate4ai-tray.exe' }
        }
        foreach ($f in $files) {
            if (-not (Test-Path -LiteralPath $f.Src)) { throw "gate4ai install: $($f.Name) is missing in the archive" }
        }

        # Install. A running gate4ai.exe cannot be overwritten, but it can be renamed.
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
        foreach ($f in $files) {
            $dst = Join-Path $dir $f.Name
            $old = "$dst.old"
            if (Test-Path -LiteralPath $old) { Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue }
            if (Test-Path -LiteralPath $dst) { Move-Item -LiteralPath $dst -Destination $old -Force }
            Copy-Item -LiteralPath $f.Src -Destination $dst -Force
        }

        # Add to the user PATH (no admin rights needed).
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $parts = @()
        if ($userPath) { $parts = $userPath -split ';' | Where-Object { $_ } }
        if ($parts -notcontains $dir) {
            [Environment]::SetEnvironmentVariable('Path', (($parts + $dir) -join ';'), 'User')
            Write-Host "Added $dir to your PATH. Open a new terminal to use gate4ai."
        }
        if (($env:Path -split ';') -notcontains $dir) { $env:Path = "$env:Path;$dir" }

        $exe = Join-Path $dir 'gate4ai.exe'
        $version = try { & $exe --version } catch { "gate4ai $tag" }
        Write-Host ''
        Write-Host "Installed: $version"
        Write-Host "  $exe"
        if (-not $noTray) { Write-Host "  $(Join-Path $dir 'gate4ai-tray.exe')" }
        $started = $false
        if ($start) {
            Write-Host ''
            $svcArgs = @('service', 'install')
            if ($noTray) { $svcArgs += '--no-tray' }
            & $exe @svcArgs | Out-Null
            if ($LASTEXITCODE -eq 0) {
                $started = $true
                Write-Host 'The gate4ai daemon is running and starts at every login (gate4ai service status).'
            } else {
                Write-Host 'Could not set up the autostart; start the daemon yourself (below).'
            }
        }

        Write-Host ''
        Write-Host 'Next steps:'
        if (-not $started) {
            Write-Host '  gate4ai service install   # run the guard now and at every login'
            Write-Host '                            # (or: gate4ai daemon start - until logout; gate4ai daemon - in this window)'
        }
        Write-Host '  gate4ai ui                # open the Web UI (starts the guard if it is not running)'
        if (-not $noTray -and -not $started) { Write-Host '  gate4ai-tray              # tray icon: status, Allow / Deny, Web UI (service install adds it to login)' }
        Write-Host "First run guide: https://github.com/$repo#readme"
    }
    finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Install-Gate4ai
