-- IKE's dedicated WezTerm configuration (#3002) — the Windows counterpart of
-- deploy/ghostty/ike.conf.
--
-- Loaded EXCLUSIVELY by the Windows launcher (ike-gui.cmd and the "Ike"
-- Start-menu entry) via
--   wezterm-gui --config-file <this file> start --always-new-process ...
-- so your normal WezTerm config never interferes with the IKE window and
-- vice versa. The release archive ships it as wezterm-ike.lua next to
-- ike.exe; a package update replaces it, so keep personal changes in a copy.

local wezterm = require 'wezterm'
local act = wezterm.action

local config = wezterm.config_builder()

-- ---- the program ----
-- ike.exe sits next to this file in the release archive (Scoop app dir,
-- winget package dir or an unpacked zip). Fall back to `ike` on PATH when the
-- file was copied elsewhere. ike-gui.cmd passes the program explicitly; this
-- is what the Start-menu entry runs.
local sep = package.config:sub(1, 1)
local function exists(path)
  local f = io.open(path, 'rb')
  if f then
    f:close()
    return true
  end
  return false
end
local ike = wezterm.config_dir .. sep .. 'ike.exe'
if not exists(ike) then
  ike = 'ike'
end
config.default_prog = { ike }
-- A Start-menu launch starts in the home directory, which IKE never treats
-- as a project: it reopens the last project instead (#1010).
config.default_cwd = wezterm.home_dir
-- The window closes when IKE exits, whatever the exit code.
config.exit_behavior = 'Close'

-- ---- appearance (tune as desired) ----
config.enable_tab_bar = false
config.window_padding = { left = 0, right = 0, top = 0, bottom = 0 }
config.initial_cols = 160
config.initial_rows = 48
config.audible_bell = 'Disabled'
-- JetBrains Mono is built into WezTerm, so the first entry always resolves;
-- the rest cover glyphs it lacks (box drawing, symbols, emoji, CJK).
config.font = wezterm.font_with_fallback {
  'JetBrains Mono',
  'Cascadia Mono',
  'Consolas',
  'Segoe UI Symbol',
  'Segoe UI Emoji',
}
-- The runtime font zoom (ctrl + plus/minus below) is per-window and NOT
-- persisted — every launch starts from this value.
config.font_size = 12.0
-- Truecolor: WezTerm renders 24-bit colour natively; COLORTERM tells IKE
-- (and everything it spawns) so.
config.set_environment_variables = { COLORTERM = 'truecolor' }

-- ---- keyboard ----
-- IKE needs real key events with real modifiers (userdocs terminal setup).
-- The Kitty protocol covers programs that ask for it; under ConPTY the
-- win32-input-mode path (WezTerm's default, kept on) carries the full key
-- events to ike.exe.
config.enable_kitty_keyboard = true
config.allow_win32_input_mode = true

-- Wipe ALL WezTerm defaults so chords reach IKE, then re-add only what the
-- terminal itself must own — the same set deploy/ghostty/ike.conf keeps on
-- Linux. Clipboard chords stay with IKE.
config.disable_default_key_bindings = true
config.keys = {
  -- WezTerm housekeeping: open this file in Notepad / reload it.
  {
    key = ',',
    mods = 'CTRL',
    action = wezterm.action_callback(function()
      wezterm.background_child_process { 'notepad.exe', wezterm.config_file }
    end),
  },
  { key = ',', mods = 'CTRL|SHIFT', action = act.ReloadConfiguration },
  -- After the wipe the window would only close through its title bar.
  -- (ctrl+q itself belongs to IKE — quick documentation — hence the shift.)
  { key = 'q', mods = 'CTRL|SHIFT', action = act.QuitApplication },
  -- Font size is terminal territory, IKE can't do it. '+' needs shift on
  -- most layouts, so both spellings are bound.
  { key = '+', mods = 'CTRL', action = act.IncreaseFontSize },
  { key = '+', mods = 'CTRL|SHIFT', action = act.IncreaseFontSize },
  { key = '-', mods = 'CTRL', action = act.DecreaseFontSize },
  { key = '0', mods = 'CTRL', action = act.ResetFontSize },
}

return config
