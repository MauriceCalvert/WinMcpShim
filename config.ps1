#Requires -Version 5.1
# config.ps1 - edits allowed_roots and run.allowed_commands in shim.toml
# (next to this script). Preserves comments and other sections by patching
# only the two fields via regex. Writes atomically with a timestamped backup.

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

[System.Windows.Forms.Application]::EnableVisualStyles()
[System.Windows.Forms.Application]::SetCompatibleTextRenderingDefault($false)

$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $MyInvocation.MyCommand.Path }

# Locate shim.toml next to the shim.exe that Claude Desktop is actually running.
# Fall back to $scriptDir if Claude's config is missing or doesn't reference the shim.
function Get-ShimTomlPath([string]$FallbackDir) {
    $claudeCfg = Join-Path $env:APPDATA 'Claude\claude_desktop_config.json'
    if (Test-Path -LiteralPath $claudeCfg) {
        try {
            $json = Get-Content -Raw -LiteralPath $claudeCfg | ConvertFrom-Json
            if ($json.mcpServers) {
                foreach ($prop in $json.mcpServers.PSObject.Properties) {
                    $cmd = $prop.Value.command
                    if ($cmd -and (Split-Path -Leaf $cmd).ToLowerInvariant() -eq 'winmcpshim.exe') {
                        return Join-Path (Split-Path -Parent $cmd) 'shim.toml'
                    }
                }
            }
        } catch { }
    }
    Join-Path $FallbackDir 'shim.toml'
}

$tomlPath        = Get-ShimTomlPath $scriptDir
$tomlExamplePath = Join-Path (Split-Path -Parent $tomlPath) 'shim.toml.example'

# ---------------------------------------------------------------------------
# Message helpers
# ---------------------------------------------------------------------------

function Show-Error([string]$msg, [string]$title = 'WinMcpShim Configuration') {
    [void][System.Windows.Forms.MessageBox]::Show($msg, $title,
        [System.Windows.Forms.MessageBoxButtons]::OK,
        [System.Windows.Forms.MessageBoxIcon]::Error)
}

function Ask-YesNo([string]$msg, [string]$title) {
    [System.Windows.Forms.MessageBox]::Show($msg, $title,
        [System.Windows.Forms.MessageBoxButtons]::YesNo,
        [System.Windows.Forms.MessageBoxIcon]::Question)
}

# ---------------------------------------------------------------------------
# TOML helpers (mirror installer/toml.go)
# ---------------------------------------------------------------------------

$allowedRootsRe    = [regex]::new('(?ms)^(allowed_roots\s*=\s*)\[.*?\]')
$allowedCommandsRe = [regex]::new('(?ms)^(allowed_commands\s*=\s*)\[.*?\]')
$runSectionRe      = [regex]::new('(?m)^\[run\]\s*$')
$stringLiteralRe   = [regex]::new('"((?:[^"\\]|\\.)*)"')

function Get-TomlStringArray {
    param([string]$Content, [regex]$ArrayRe)
    $m = $ArrayRe.Match($Content)
    if (-not $m.Success) { return ,@() }
    $body = $Content.Substring($m.Index + $m.Groups[1].Length)  # starts at '['
    $body = $body.Substring(1, $body.IndexOf(']') - 1)
    $items = [System.Collections.Generic.List[string]]::new()
    $unescape = [System.Text.RegularExpressions.MatchEvaluator]{
        param($m)
        switch ($m.Groups[1].Value) {
            '\'     { '\' }
            '"'     { '"' }
            default { $m.Value }
        }
    }
    foreach ($sm in $stringLiteralRe.Matches($body)) {
        $raw = $sm.Groups[1].Value
        $items.Add([regex]::Replace($raw, '\\(.)', $unescape))
    }
    return ,$items.ToArray()
}

function Format-TomlString([string]$s) {
    # TOML basic string: escape \ and "
    $escaped = ($s -replace '\\', '\\') -replace '"', '\"'
    '"' + $escaped + '"'
}

function Format-TomlRoots($roots) {
    if (-not $roots -or $roots.Count -eq 0) { return '[]' }
    $lines = foreach ($r in $roots) { "    $(Format-TomlString $r)," }
    "[`r`n" + ($lines -join "`r`n") + "`r`n]"
}

function Format-TomlCommands($cmds) {
    if (-not $cmds -or $cmds.Count -eq 0) { return '[]' }
    $parts = foreach ($c in $cmds) { Format-TomlString $c }
    '[' + ($parts -join ', ') + ']'
}

function Set-AllowedRoots([string]$Content, $Roots) {
    if (-not $allowedRootsRe.IsMatch($Content)) {
        throw 'allowed_roots block not found in shim.toml'
    }
    $replacement = 'allowed_roots = ' + (Format-TomlRoots $Roots)
    $allowedRootsRe.Replace($Content, [System.Text.RegularExpressions.MatchEvaluator]{ param($m) $replacement })
}

function Set-AllowedCommands([string]$Content, $Cmds) {
    $replacement = 'allowed_commands = ' + (Format-TomlCommands $Cmds)
    if ($allowedCommandsRe.IsMatch($Content)) {
        return $allowedCommandsRe.Replace($Content, [System.Text.RegularExpressions.MatchEvaluator]{ param($m) $replacement })
    }
    $sec = $runSectionRe.Match($Content)
    if (-not $sec.Success) { throw '[run] section not found in shim.toml' }
    $headerEnd = $sec.Index + $sec.Length
    if ($headerEnd -lt $Content.Length -and $Content[$headerEnd] -eq [char]"`r") { $headerEnd++ }
    if ($headerEnd -lt $Content.Length -and $Content[$headerEnd] -eq [char]"`n") { $headerEnd++ }
    $Content.Substring(0, $headerEnd) + $replacement + "`r`n" + $Content.Substring($headerEnd)
}

function Write-AtomicUtf8([string]$Path, [string]$Content) {
    $tmp = "$Path.tmp"
    $utf8NoBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText($tmp, $Content, $utf8NoBom)
    Move-Item -LiteralPath $tmp -Destination $Path -Force
}

function Backup-File([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) { return }
    $stamp = Get-Date -Format 'yyyyMMdd\THHmmss'
    Copy-Item -LiteralPath $Path -Destination "$Path.bak.$stamp"
}

function Test-CommandName([string]$Name) {
    if ([string]::IsNullOrWhiteSpace($Name)) { return $false }
    # Accept either a bare basename (letters, digits, ., _, -) or a full path
    # to an existing executable. The shim resolves bare names on PATH at
    # startup, so both forms reach the runtime as absolute paths.
    if (Test-Path -LiteralPath $Name -PathType Leaf) { return $true }
    if ($Name -notmatch '^[A-Za-z0-9._-]+$')  { return $false }
    if ($Name.ToLowerInvariant().EndsWith('.exe')) { return $false }
    $true
}

# Resolve-CommandToPath turns a user-entered command into the absolute
# executable path that will end up in shim.toml. Mirrors the runtime's
# ResolveCommandPath: full paths are returned as-is (after Get-Item
# normalisation); bare names are looked up via Get-Command. Returns $null
# when resolution fails.
function Resolve-CommandToPath([string]$Name) {
    if ([string]::IsNullOrWhiteSpace($Name)) { return $null }
    if (Test-Path -LiteralPath $Name -PathType Leaf) {
        try { return (Get-Item -LiteralPath $Name).FullName } catch { return $Name }
    }
    $cmd = Get-Command -Name $Name -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $cmd) { return $null }
    return $cmd.Source
}

# Known interpreters: anything that loads and runs arbitrary user code.
# Listing one of these on the run allowlist makes the allowlist meaningless —
# Claude can pipe any command through the interpreter. config.cmd warns but
# does not refuse, because some users genuinely want powershell/python on
# the list.
$script:KnownInterpreters = @(
    'powershell', 'pwsh', 'cmd',
    'bash', 'sh', 'zsh', 'wsl',
    'python', 'python3', 'py',
    'node', 'deno', 'bun',
    'ruby', 'perl', 'php', 'lua', 'tclsh',
    'cscript', 'wscript',
    'dotnet', 'java'
)

function Test-IsInterpreter([string]$Name) {
    $bare = $Name.ToLowerInvariant()
    if ($bare.EndsWith('.exe')) { $bare = $bare.Substring(0, $bare.Length - 4) }
    return $script:KnownInterpreters -contains $bare
}

# ---------------------------------------------------------------------------
# Bootstrap shim.toml if missing
# ---------------------------------------------------------------------------

if (-not (Test-Path -LiteralPath $tomlPath)) {
    if (-not (Test-Path -LiteralPath $tomlExamplePath)) {
        Show-Error "shim.toml was not found at:`r`n`r`n$tomlPath`r`n`r`nNo shim.toml.example template is available either. Please run install.exe first, or copy shim.toml.example into this directory."
        exit 1
    }
    $r = Ask-YesNo "shim.toml was not found at:`r`n`r`n$tomlPath`r`n`r`nCreate it now from shim.toml.example?" 'Create shim.toml?'
    if ($r -ne [System.Windows.Forms.DialogResult]::Yes) { exit 0 }
    Copy-Item -LiteralPath $tomlExamplePath -Destination $tomlPath
}

$tomlContent = [System.IO.File]::ReadAllText($tomlPath)
$initialRoots = Get-TomlStringArray -Content $tomlContent -ArrayRe $allowedRootsRe
$initialCmds  = Get-TomlStringArray -Content $tomlContent -ArrayRe $allowedCommandsRe

# ---------------------------------------------------------------------------
# Form
# ---------------------------------------------------------------------------

$form                 = [System.Windows.Forms.Form]::new()
$form.Text            = "WinMcpShim Configuration - $tomlPath"
$form.ClientSize      = [System.Drawing.Size]::new(520, 470)
# Resizable so users can widen the window to read long full executable paths in
# the listboxes. MinimumSize is set after ClientSize so it captures the outer
# (border+titlebar inclusive) dimensions and keeps controls from being clipped.
$form.FormBorderStyle = [System.Windows.Forms.FormBorderStyle]::Sizable
$form.MaximizeBox     = $true
$form.MinimizeBox     = $true
$form.StartPosition   = [System.Windows.Forms.FormStartPosition]::CenterScreen
$form.Font            = [System.Drawing.SystemFonts]::MessageBoxFont
$form.MinimumSize     = $form.Size

$lblRoots          = [System.Windows.Forms.Label]::new()
$lblRoots.Text     = 'Allowed root directories'
$lblRoots.Location = [System.Drawing.Point]::new(12, 10)
$lblRoots.AutoSize = $true
$form.Controls.Add($lblRoots)

# Anchor constants — `Anchor` takes a flags enum, easier to read than raw ints.
$AnchorTop    = [System.Windows.Forms.AnchorStyles]::Top
$AnchorBottom = [System.Windows.Forms.AnchorStyles]::Bottom
$AnchorLeft   = [System.Windows.Forms.AnchorStyles]::Left
$AnchorRight  = [System.Windows.Forms.AnchorStyles]::Right

$lstRoots                    = [System.Windows.Forms.ListBox]::new()
$lstRoots.Location           = [System.Drawing.Point]::new(12, 32)
$lstRoots.Size               = [System.Drawing.Size]::new(380, 160)
$lstRoots.TabIndex           = 0
$lstRoots.HorizontalScrollbar = $true
$lstRoots.IntegralHeight     = $false
$lstRoots.Anchor             = $AnchorTop -bor $AnchorLeft -bor $AnchorRight
foreach ($r in $initialRoots) { [void]$lstRoots.Items.Add($r) }
$form.Controls.Add($lstRoots)

$btnRootsAdd          = [System.Windows.Forms.Button]::new()
$btnRootsAdd.Text     = 'Add...'
$btnRootsAdd.Location = [System.Drawing.Point]::new(400, 32)
$btnRootsAdd.Size     = [System.Drawing.Size]::new(100, 26)
$btnRootsAdd.TabIndex = 1
$btnRootsAdd.Anchor   = $AnchorTop -bor $AnchorRight
$form.Controls.Add($btnRootsAdd)

$btnRootsRemove          = [System.Windows.Forms.Button]::new()
$btnRootsRemove.Text     = 'Remove'
$btnRootsRemove.Location = [System.Drawing.Point]::new(400, 64)
$btnRootsRemove.Size     = [System.Drawing.Size]::new(100, 26)
$btnRootsRemove.TabIndex = 2
$btnRootsRemove.Anchor   = $AnchorTop -bor $AnchorRight
$form.Controls.Add($btnRootsRemove)

$lblCmds          = [System.Windows.Forms.Label]::new()
$lblCmds.Text     = 'Allowed run commands (optional)'
$lblCmds.Location = [System.Drawing.Point]::new(12, 210)
$lblCmds.AutoSize = $true
$form.Controls.Add($lblCmds)

$lstCmds                    = [System.Windows.Forms.ListBox]::new()
$lstCmds.Location           = [System.Drawing.Point]::new(12, 232)
$lstCmds.Size               = [System.Drawing.Size]::new(380, 160)
$lstCmds.TabIndex           = 3
$lstCmds.HorizontalScrollbar = $true
$lstCmds.IntegralHeight     = $false
# Bottom anchor: vertical extra space added by the user grows the cmds listbox.
$lstCmds.Anchor             = $AnchorTop -bor $AnchorLeft -bor $AnchorRight -bor $AnchorBottom
foreach ($c in $initialCmds) { [void]$lstCmds.Items.Add($c) }
$form.Controls.Add($lstCmds)

$btnCmdsAdd          = [System.Windows.Forms.Button]::new()
$btnCmdsAdd.Text     = 'Add...'
$btnCmdsAdd.Location = [System.Drawing.Point]::new(400, 232)
$btnCmdsAdd.Size     = [System.Drawing.Size]::new(100, 26)
$btnCmdsAdd.TabIndex = 4
$btnCmdsAdd.Anchor   = $AnchorTop -bor $AnchorRight
$form.Controls.Add($btnCmdsAdd)

$btnCmdsRemove          = [System.Windows.Forms.Button]::new()
$btnCmdsRemove.Text     = 'Remove'
$btnCmdsRemove.Location = [System.Drawing.Point]::new(400, 264)
$btnCmdsRemove.Size     = [System.Drawing.Size]::new(100, 26)
$btnCmdsRemove.TabIndex = 5
$btnCmdsRemove.Anchor   = $AnchorTop -bor $AnchorRight
$form.Controls.Add($btnCmdsRemove)

$btnSave          = [System.Windows.Forms.Button]::new()
$btnSave.Text     = 'Save'
$btnSave.Location = [System.Drawing.Point]::new(312, 420)
$btnSave.Size     = [System.Drawing.Size]::new(90, 30)
$btnSave.TabIndex = 6
$btnSave.Anchor   = $AnchorBottom -bor $AnchorRight
$form.Controls.Add($btnSave)

$btnCancel              = [System.Windows.Forms.Button]::new()
$btnCancel.Text         = 'Cancel'
$btnCancel.Location     = [System.Drawing.Point]::new(410, 420)
$btnCancel.Size         = [System.Drawing.Size]::new(90, 30)
$btnCancel.TabIndex     = 7
$btnCancel.DialogResult = [System.Windows.Forms.DialogResult]::Cancel
$btnCancel.Anchor       = $AnchorBottom -bor $AnchorRight
$form.Controls.Add($btnCancel)

$form.AcceptButton = $btnSave
$form.CancelButton = $btnCancel

# ---------------------------------------------------------------------------
# Handlers
# ---------------------------------------------------------------------------

$btnRootsAdd.Add_Click({
    $dlg = [System.Windows.Forms.FolderBrowserDialog]::new()
    $dlg.Description = 'Select a folder to allow'
    $dlg.ShowNewFolderButton = $false
    if ($dlg.ShowDialog($form) -ne [System.Windows.Forms.DialogResult]::OK) { return }
    $path = $dlg.SelectedPath.TrimEnd('\')
    $existingLower = @(foreach ($i in $lstRoots.Items) { $i.ToLowerInvariant() })
    if ($existingLower -contains $path.ToLowerInvariant()) { return }
    [void]$lstRoots.Items.Add($path)
})

$btnRootsRemove.Add_Click({
    if ($lstRoots.SelectedIndex -ge 0) {
        $lstRoots.Items.RemoveAt($lstRoots.SelectedIndex)
    }
})

function Show-InputBox([string]$Title, [string]$Prompt) {
    $dlg                 = [System.Windows.Forms.Form]::new()
    $dlg.Text            = $Title
    $dlg.ClientSize      = [System.Drawing.Size]::new(420, 120)
    $dlg.FormBorderStyle = [System.Windows.Forms.FormBorderStyle]::FixedDialog
    $dlg.MaximizeBox     = $false
    $dlg.MinimizeBox     = $false
    $dlg.StartPosition   = [System.Windows.Forms.FormStartPosition]::CenterParent
    $dlg.Font            = [System.Drawing.SystemFonts]::MessageBoxFont

    $lbl          = [System.Windows.Forms.Label]::new()
    $lbl.Text     = $Prompt
    $lbl.Location = [System.Drawing.Point]::new(12, 12)
    $lbl.Size     = [System.Drawing.Size]::new(396, 34)
    $dlg.Controls.Add($lbl)

    $edit          = [System.Windows.Forms.TextBox]::new()
    $edit.Location = [System.Drawing.Point]::new(12, 50)
    $edit.Size     = [System.Drawing.Size]::new(396, 22)
    $edit.TabIndex = 0
    $dlg.Controls.Add($edit)

    $ok              = [System.Windows.Forms.Button]::new()
    $ok.Text         = 'OK'
    $ok.Location     = [System.Drawing.Point]::new(240, 82)
    $ok.Size         = [System.Drawing.Size]::new(80, 26)
    $ok.DialogResult = [System.Windows.Forms.DialogResult]::OK
    $ok.TabIndex     = 1
    $dlg.Controls.Add($ok)

    $cancel              = [System.Windows.Forms.Button]::new()
    $cancel.Text         = 'Cancel'
    $cancel.Location     = [System.Drawing.Point]::new(328, 82)
    $cancel.Size         = [System.Drawing.Size]::new(80, 26)
    $cancel.DialogResult = [System.Windows.Forms.DialogResult]::Cancel
    $cancel.TabIndex     = 2
    $dlg.Controls.Add($cancel)

    $dlg.AcceptButton = $ok
    $dlg.CancelButton = $cancel

    $result = $dlg.ShowDialog($form)
    $value  = $edit.Text.Trim()
    $dlg.Dispose()
    if ($result -eq [System.Windows.Forms.DialogResult]::OK) { return $value }
    return $null
}

$btnCmdsAdd.Add_Click({
    $name = Show-InputBox 'Add allowed command' 'Command name (e.g. git, npm, powershell) or full path to an .exe. Bare names are resolved on PATH and stored as absolute paths.'
    if ($null -eq $name -or $name -eq '') { return }
    if (-not (Test-CommandName $name)) {
        Show-Error 'Command must be either a bare basename (letters, digits, dot, underscore, hyphen — no .exe suffix) or a full path to an existing executable.' 'Invalid command name'
        return
    }
    if (Test-IsInterpreter $name) {
        $msg = "'$name' is a shell or interpreter that can run arbitrary code. Adding it to the run allowlist effectively allows any command (Claude can pipe anything through $name).`r`n`r`nAdd it anyway?"
        $r = [System.Windows.Forms.MessageBox]::Show($msg, 'Allowlist bypass warning',
            [System.Windows.Forms.MessageBoxButtons]::YesNo,
            [System.Windows.Forms.MessageBoxIcon]::Warning,
            [System.Windows.Forms.MessageBoxDefaultButton]::Button2)
        if ($r -ne [System.Windows.Forms.DialogResult]::Yes) { return }
    }
    $resolved = Resolve-CommandToPath $name
    if ($null -eq $resolved) {
        Show-Error "Could not resolve '$name' on PATH. Add it to PATH first, or supply the full path to the executable." 'Command not found'
        return
    }
    $existingLower = @(foreach ($i in $lstCmds.Items) { $i.ToLowerInvariant() })
    if ($existingLower -contains $resolved.ToLowerInvariant()) { return }
    [void]$lstCmds.Items.Add($resolved)
})

$btnCmdsRemove.Add_Click({
    if ($lstCmds.SelectedIndex -ge 0) {
        $lstCmds.Items.RemoveAt($lstCmds.SelectedIndex)
    }
})

$btnSave.Add_Click({
    $roots = @(foreach ($i in $lstRoots.Items) { $i })
    $cmds  = @(foreach ($i in $lstCmds.Items)  { $i })

    try {
        $new = Set-AllowedRoots    -Content $tomlContent -Roots $roots
        $new = Set-AllowedCommands -Content $new         -Cmds  $cmds
    } catch {
        Show-Error "Save failed: $_"
        return
    }

    try {
        Backup-File $tomlPath
        Write-AtomicUtf8 -Path $tomlPath -Content $new
    } catch {
        Show-Error "Write failed: $_"
        return
    }

    $script:tomlContent = $new
    $form.DialogResult  = [System.Windows.Forms.DialogResult]::OK
    $form.Close()
})

# ---------------------------------------------------------------------------
# Show form; on Save, offer Claude Desktop restart
# ---------------------------------------------------------------------------

$result = $form.ShowDialog()
$form.Dispose()

if ($result -ne [System.Windows.Forms.DialogResult]::OK) { exit 0 }

$r = Ask-YesNo "Configuration saved.`r`n`r`nClaude Desktop must be restarted for changes to take effect.`r`n`r`nRestart Claude Desktop now?" 'Restart Claude Desktop?'
if ($r -ne [System.Windows.Forms.DialogResult]::Yes) { exit 0 }

Get-Process -Name claude -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

$candidates = @()
if ($env:LOCALAPPDATA) { $candidates += Join-Path $env:LOCALAPPDATA 'AnthropicClaude\claude.exe' }
if ($env:ProgramFiles) { $candidates += Join-Path $env:ProgramFiles 'AnthropicClaude\claude.exe' }
foreach ($c in $candidates) {
    if (Test-Path -LiteralPath $c) {
        Start-Process -FilePath $c
        exit 0
    }
}
Show-Error 'Could not find Claude Desktop to relaunch. Please start it manually.' 'Claude Desktop not found'
