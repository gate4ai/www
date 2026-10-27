# gate4.ai installer for Windows (PowerShell 5.1 and 7, no admin rights).
#
#   irm https://gate4.ai/install.ps1 | iex
#
# Options (environment variables):
#   $env:GATE4AI_VERSION = 'v0.1.1'   install a specific version (default: latest)
#   $env:GATE4AI_INSTALL_DIR = 'DIR'  install folder (default: %LOCALAPPDATA%\Programs\gate4ai)
#   $env:GATE4AI_NO_TRAY = '1'        do not install the tray icon app
#   $env:GATE4AI_NO_START = '1'       do not turn on the autostart. By default the
#                                     installer runs gate4ai service install: the
#                                     daemon starts now and at every login (HKCU Run
#                                     key), the tray icon at every login.
#   $env:GATE4AI_NO_OPEN = '1'        do not open the Web UI after installing. By
#                                     default, outside CI (CI not set), the installer
#                                     runs gate4ai ui: the browser opens the Web UI
#                                     (sign in with the generated password the
#                                     installer prints, then the setup wizard).
#   $env:GATE4AI_INSTALL_DEPS = '1'   install Google Chrome for the browser of agents
#                                     without asking (winget, for this user). By
#                                     default the installer checks it and asks first.
#   $env:GATE4AI_NO_BROWSER = '1'     do not check Chrome now (the browser is built
#                                     into gate4ai; later: gate4ai browser setup).
#   $env:GATE4AI_NO_RELAY = '1'       do not turn on access from the internet. By
#                                     default the daemon registers this computer on
#                                     relay.gate4.ai and gets https://<id>.relay.gate4.ai:
#                                     the relay sees only a random device id and
#                                     encrypted traffic, nothing else leaves the
#                                     computer. With this variable the daemon starts
#                                     with internet access off (relay.disabled);
#                                     later: gate4ai relay on | off.
#
# Downloads the release from GitHub, checks sha256 checksums and only then
# installs. Never touches your configuration or data.
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
    $noStart = [bool]$env:GATE4AI_NO_START
    $noOpen = [bool]$env:GATE4AI_NO_OPEN
    $noRelay = [bool]$env:GATE4AI_NO_RELAY

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
        # The browser of agents: Google Chrome (gate4ai browser setup asks before installing it).
        Write-Host ''
        if ($env:GATE4AI_NO_BROWSER) {
            Write-Host 'Chrome is not checked (GATE4AI_NO_BROWSER). Agents get browser_unavailable until it is installed: gate4ai browser setup'
        } else {
            Write-Host 'The browser of agents (gate4ai browser setup):'
            $setupArgs = @('browser', 'setup')
            if ($env:GATE4AI_INSTALL_DEPS) { $setupArgs += '--yes' } elseif ($env:CI) { $setupArgs += '--dry-run' }
            $prevOutput = $env:GATE4AI_OUTPUT
            $env:GATE4AI_OUTPUT = 'table'
            $browserOK = $false
            try { & $exe @setupArgs; $browserOK = ($LASTEXITCODE -eq 0) } catch { $browserOK = $false }
            $env:GATE4AI_OUTPUT = $prevOutput
            if (-not $browserOK) { Write-Host 'Agents get browser_unavailable until Chrome is installed: gate4ai browser setup' }
        }
        $running = $false
        if ($noStart) {
            # An update: a daemon that is already running keeps the old binary until it restarts.
            # Windows PowerShell 5.1 turns redirected stderr of a program into errors: try/catch.
            $wasRunning = $false
            try { & $exe daemon status *> $null; $wasRunning = ($LASTEXITCODE -eq 0) } catch { $wasRunning = $false }
            if ($wasRunning) {
                $restarted = $false
                try { & $exe daemon restart *> $null; $restarted = ($LASTEXITCODE -eq 0) } catch { $restarted = $false }
                if ($restarted) {
                    $running = $true
                    Write-Host 'The running gate4ai daemon was restarted with the new version.'
                } else {
                    Write-Host 'Restart the running daemon to use the new version: gate4ai daemon restart'
                }
            }
        } else {
            # The autostart; it also restarts a daemon that is already running (an update).
            Write-Host ''
            $svcArgs = @('service', 'install')
            if ($noTray) { $svcArgs += '--no-tray' }
            # GATE4AI_NO_RELAY: the daemon starts without the automatic registration on
            # the relay, then the owner's "off" is written to the configuration
            # (relay.disabled); gate4ai relay on turns it on later.
            if ($noRelay) { $svcArgs += @('--', '--relay-auto=false') }
            & $exe @svcArgs | Out-Null
            if ($LASTEXITCODE -eq 0) {
                $running = $true
                Write-Host 'The gate4ai daemon is running and starts at every login (gate4ai service status).'
                if ($noRelay) {
                    $off = $false
                    try { & $exe relay off *> $null; $off = ($LASTEXITCODE -eq 0) } catch { $off = $false }
                    if ($off) {
                        Write-Host 'Access from the internet is off (GATE4AI_NO_RELAY): this computer is not registered on relay.gate4.ai. Turn it on later: gate4ai relay on'
                    } else {
                        Write-Host 'Could not turn access from the internet off (GATE4AI_NO_RELAY); do it yourself: gate4ai relay off'
                    }
                }
            } else {
                Write-Host 'Could not set up the autostart: gate4ai ui starts the daemon for this session.'
            }
        }

        # The Web UI shows the setup wizard: open it right away (Windows always has a desktop).
        Write-Host ''
        $opened = $false
        if ($running -and -not $env:CI -and -not $noOpen) {
            try {
                & $exe ui
                $opened = ($LASTEXITCODE -eq 0)
            } catch { $opened = $false }
        }
        # The Web UI password is generated by the daemon at its first start: gate4ai ui
        # prints it until the first sign-in. Lost it: gate4ai ui --reset-password.
        if ($opened) {
            Write-Host 'The Web UI is open in your browser: sign in with the password above, then the setup wizard starts. Open it again any time: gate4ai ui'
        } elseif ($running) {
            try { & $exe ui --print | Out-Null } catch { $null = $_ }
            Write-Host 'Open the address above in a browser and sign in with the password above (save it; it is shown until the first sign-in). Then the setup wizard starts.'
        } else {
            Write-Host 'Next: gate4ai ui   # prints the Web UI address and password, opens the setup wizard in your browser'
        }
    }
    finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Install-Gate4ai
