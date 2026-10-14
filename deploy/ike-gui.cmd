@echo off
rem ike-gui.cmd - open IKE in a dedicated WezTerm window on Windows (#3002),
rem the counterpart of deploy/ike-gui. The window loads wezterm-ike.lua
rem EXCLUSIVELY (--config-file), so the user's own WezTerm config never
rem interferes, and runs ike.exe on the current directory as project root.
rem Any arguments are passed on to ike (e.g. a file to open).
rem
rem Lookup order:
rem   wezterm-gui.exe  %IKE_WEZTERM%, Scoop (user, global), winget / installer
rem                    (%ProgramFiles%\WezTerm, %LOCALAPPDATA%\Programs\WezTerm),
rem                    then PATH. Real install dirs come before PATH because a
rem                    Scoop shim on PATH is a console program and would flash
rem                    a console window.
rem   ike.exe          next to this script, else PATH.
rem   wezterm-ike.lua  next to this script, else next to ike.exe, else the
rem                    winget package dir.
setlocal EnableExtensions

set "WEZ="
if defined IKE_WEZTERM if exist "%IKE_WEZTERM%" set "WEZ=%IKE_WEZTERM%"
if not defined SCOOP set "SCOOP=%USERPROFILE%\scoop"
if not defined SCOOP_GLOBAL set "SCOOP_GLOBAL=%ProgramData%\scoop"
for %%P in (
  "%SCOOP%\apps\wezterm\current\wezterm-gui.exe"
  "%SCOOP%\apps\wezterm-nightly\current\wezterm-gui.exe"
  "%SCOOP_GLOBAL%\apps\wezterm\current\wezterm-gui.exe"
  "%ProgramFiles%\WezTerm\wezterm-gui.exe"
  "%LOCALAPPDATA%\Programs\WezTerm\wezterm-gui.exe"
) do if not defined WEZ if exist "%%~P" set "WEZ=%%~P"
if not defined WEZ for %%W in (wezterm-gui.exe) do if not "%%~$PATH:W"=="" set "WEZ=%%~$PATH:W"
if not defined WEZ (
  echo ike-gui: WezTerm not found - install it with "scoop install wezterm" or from https://wezterm.org/install/windows.html 1>&2
  exit /b 1
)

set "IKE=%~dp0ike.exe"
if not exist "%IKE%" (
  set "IKE="
  for %%I in (ike.exe) do if not "%%~$PATH:I"=="" set "IKE=%%~$PATH:I"
)
if not defined IKE (
  echo ike-gui: ike.exe not found next to %~nx0 or on PATH 1>&2
  exit /b 1
)

set "CONF=%~dp0wezterm-ike.lua"
if not exist "%CONF%" for %%I in ("%IKE%") do set "CONF=%%~dpIwezterm-ike.lua"
if not exist "%CONF%" for /d %%D in ("%LOCALAPPDATA%\Microsoft\WinGet\Packages\TrueDaerk.Ike_*") do if exist "%%~D\wezterm-ike.lua" set "CONF=%%~D\wezterm-ike.lua"
if not exist "%CONF%" (
  echo ike-gui: wezterm-ike.lua not found next to %~nx0 or ike.exe 1>&2
  exit /b 1
)

rem --always-new-process: a running WezTerm would otherwise open the window
rem in its own process, with the user's config instead of this one.
rem A trailing backslash (a drive root, C:\) would escape the closing quote.
set "CWD=%CD%"
if "%CWD:~-1%"=="\" set "CWD=%CWD%."
start "" "%WEZ%" --config-file "%CONF%" start --always-new-process --cwd "%CWD%" -- "%IKE%" %*
exit /b 0
