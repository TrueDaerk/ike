<#
.SYNOPSIS
  Create (or remove) the "Ike" Start-menu entry on Windows (#3002).

.DESCRIPTION
  The Windows counterpart of scripts/install-desktop.sh, for installs that do
  not create the entry themselves: an unpacked release zip and winget (Scoop
  does it through its manifest's `shortcuts`). Run it from the directory that
  holds ike.exe, wezterm-ike.lua and ike.ico - the release archive ships it
  there:

    powershell -ExecutionPolicy Bypass -File .\install-desktop.ps1
    powershell -ExecutionPolicy Bypass -File .\install-desktop.ps1 -Uninstall

  The entry starts wezterm-gui.exe directly (no console window flashes) with
  the dedicated config, which runs the ike.exe next to it. WezTerm is looked
  up like ike-gui.cmd does: $env:IKE_WEZTERM, Scoop, the WezTerm installer /
  winget locations, then PATH.
#>
param([switch]$Uninstall)

$ErrorActionPreference = 'Stop'

$programs = [Environment]::GetFolderPath('Programs')
$lnk = Join-Path $programs 'Ike.lnk'

if ($Uninstall) {
    if (Test-Path $lnk) { Remove-Item $lnk; Write-Host "Removed $lnk" }
    exit 0
}

$here = $PSScriptRoot
$conf = Join-Path $here 'wezterm-ike.lua'
$icon = Join-Path $here 'ike.ico'
foreach ($f in @((Join-Path $here 'ike.exe'), $conf)) {
    if (-not (Test-Path $f)) {
        [Console]::Error.WriteLine("install-desktop: $f not found - run this script from the unpacked IKE release")
        exit 1
    }
}

$scoop = if ($env:SCOOP) { $env:SCOOP } else { Join-Path $env:USERPROFILE 'scoop' }
$scoopGlobal = if ($env:SCOOP_GLOBAL) { $env:SCOOP_GLOBAL } else { Join-Path $env:ProgramData 'scoop' }
$candidates = @(
    $env:IKE_WEZTERM,
    (Join-Path $scoop 'apps\wezterm\current\wezterm-gui.exe'),
    (Join-Path $scoop 'apps\wezterm-nightly\current\wezterm-gui.exe'),
    (Join-Path $scoopGlobal 'apps\wezterm\current\wezterm-gui.exe'),
    (Join-Path $env:ProgramFiles 'WezTerm\wezterm-gui.exe'),
    (Join-Path $env:LOCALAPPDATA 'Programs\WezTerm\wezterm-gui.exe')
)
$wez = $candidates | Where-Object { $_ -and (Test-Path $_) } | Select-Object -First 1
if (-not $wez) {
    $cmd = Get-Command wezterm-gui.exe -ErrorAction SilentlyContinue
    if ($cmd) { $wez = $cmd.Source }
}
if (-not $wez) {
    [Console]::Error.WriteLine('install-desktop: WezTerm not found - install it with "scoop install wezterm", "winget install wez.wezterm" or from https://wezterm.org/install/windows.html')
    exit 1
}

$shell = New-Object -ComObject WScript.Shell
$s = $shell.CreateShortcut($lnk)
$s.TargetPath = $wez
$s.Arguments = "--config-file `"$conf`" start --always-new-process"
$s.WorkingDirectory = $env:USERPROFILE
$s.Description = 'IKE - terminal IDE'
if (Test-Path $icon) { $s.IconLocation = $icon }
$s.Save()
Write-Host "Created $lnk -> $wez"
