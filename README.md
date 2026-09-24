# jetbrains-keymap-to-vscode

A small command-line tool that copies your keyboard shortcuts from an installed
JetBrains IDE (PhpStorm, GoLand, IntelliJ IDEA, WebStorm, PyCharm, Rider,
DataGrip, …) into VS Code's `keybindings.json`.

It is an alternative to installing a keymap extension in VS Code. It is a single
binary without dependencies. You run it once, or again after you change your
JetBrains keymap, and nothing extra runs inside your editor.

The transfer is one-way: JetBrains → VS Code.

## What it does

- Finds installed JetBrains IDEs and their configuration directories. JetBrains
  keeps old configuration directories after upgrades (`PhpStorm2025.3`,
  `PhpStorm2026.2`, …). The tool always uses the **newest version** of each
  product and tells you which older ones it ignored.
- Reads the keymap that is active in the IDE, including the keymaps it inherits
  from, the bundled default keymaps inside the IDE installation and the default
  shortcuts that plugins declare.
- Converts shortcuts of actions that have a VS Code counterpart and reports
  everything it could not convert.
- Writes the result into a marked block in `keybindings.json` and leaves
  everything else in that file untouched.

## Safety

- **Dry run by default.** Without `--apply`, nothing is written. The tool shows
  the block it would write and a report.
- **Backup before every write.** Before `keybindings.json` changes, the tool
  copies it to `keybindings.json.<timestamp>.bak`, for example
  `keybindings.json.2026-09-24T11-33-05.bak`, and reads the copy back to verify
  it. If the backup cannot be created or verified, nothing is written. Existing
  backups are never overwritten.
- **Comments stay.** VS Code reads `keybindings.json` as JSONC, JSON with
  comments and trailing commas. The tool never re-serializes the file.
- **Only its own block changes.** The generated keybindings sit between
  `// >>> jetbrains-keymap-to-vscode` and `// <<< jetbrains-keymap-to-vscode`.
  Your own keybindings, comments and formatting outside the block stay as they
  are. Running the tool again replaces only the block. Before writing, the tool
  checks that the new file parses and that every keybinding outside the block
  is unchanged.
- **Atomic write.** The new file is written to a temporary file and renamed into
  place. If `keybindings.json` changed while the tool was running (for example,
  you saved it in VS Code), the tool refuses to write. If `keybindings.json` is
  a symlink (dotfiles), the tool writes to the file it points to.
- **Restore.** `restore` shows what restoring the newest backup would change;
  `restore --apply` does it. The newest backup is the state before the last
  change made by the tool, so this undoes that change. Name a backup to restore
  an older one. The current file is backed up first, so a restore can be undone
  too.

## Usage

```sh
# Which IDEs were found, and which keymap is active in each
jetbrains-keymap-to-vscode list

# Show what would be written (asks which IDE to use if there are several)
jetbrains-keymap-to-vscode convert
jetbrains-keymap-to-vscode convert --ide phpstorm

# Write it
jetbrains-keymap-to-vscode convert --ide phpstorm --apply

# Show the exact JSON block and every skipped shortcut
jetbrains-keymap-to-vscode convert --ide phpstorm -v

# Undo the last change: dry run first, then for real
jetbrains-keymap-to-vscode restore -v
jetbrains-keymap-to-vscode restore --apply

# Restore a specific backup
jetbrains-keymap-to-vscode restore keybindings.json.2026-09-24T11-33-05.bak --apply
```

### Scope

`--scope` chooses which shortcuts to transfer:

| Scope | What is transferred |
|---|---|
| `custom` (default) | Only the actions you changed in your own JetBrains keymap |
| `all` | The complete keymap: the bundled defaults plus your changes |
| `default` | The bundled keymap your keymap is based on, without your changes |

`all` and `default` need the IDE installation, because that is where the
bundled keymaps are. `custom` works with only the configuration directory.

### Other flags

| Flag | Meaning |
|---|---|
| `--ide NAME` | IDE to read: `phpstorm`, `goland`, `IntelliJIdea`, … (a unique prefix is enough) |
| `--target NAME` | `code` (default), `insiders`, `vscodium` or `cursor` |
| `--keybindings PATH` | Write to this file instead of the target's `keybindings.json` |
| `--file PATH` | Read a JetBrains keymap you prepared instead of the IDE's own: a keymap `.xml`, or a settings export `.zip` (File → Manage IDE Settings → Export Settings) |
| `--keymap NAME` | Read this keymap instead of the one active in the IDE or in `--file` |
| `--keep-key KEY` | Leave this key to VS Code, e.g. `cmd+g` or `cmd+ě` (repeatable, remembered) |
| `--unkeep-key KEY` | Take a key off that list again |
| `--layout NAME` | Keyboard layout for character shortcuts: `auto` (default, macOS), `none`, `cz`, `cz-qwerty`, `sk`, `sk-qwerty` |
| `--config-root DIR` | Directory with JetBrains configuration directories |
| `--ide-home DIR` | IDE installation directory (the one containing `lib/`) |
| `-v` | Show the exact JSON block and list every skipped shortcut (with `restore`: list the keybindings it adds and removes) |

## Keyboard layouts

JetBrains stores some shortcuts as the character they type: `+`, `)`, `§` or
national letters such as `ě` or `š`. On a Czech keyboard, `Cmd+ě` is stored as
"Cmd + the character ě". VS Code can bind a physical key regardless of the
layout (`cmd+[Digit2]`). So the tool needs to know which key types the
character.

- On macOS the tool asks the system for the current layout (`--layout auto`,
  the default) and looks the character up in a table for that layout.
- The tables are generated from the layouts installed in macOS by
  `tools/gen-mac-layouts.swift`. They are not written by hand.
- Included: Czech (QWERTZ and QWERTY) and Slovak (QWERTZ and QWERTY).
  `--layout cz-qwerty` and similar names override the detected layout.
  `--layout none` turns the lookup off.
- Letters and the keys that VS Code already understands (`-`, `/`, `[`, …) are
  written as before. VS Code maps them through the active layout itself.
- On Linux and Windows, and for layouts without a table, these shortcuts are
  reported instead of converted.

Adding a macOS layout means adding its input source id to the generator and
running it again:

```sh
swift tools/gen-mac-layouts.swift > internal/layout/mac.json
```

## What is not converted

The report lists all of these:

- **Actions without a VS Code counterpart.** The action table
  (`internal/convert/actions.json`, about 250 actions) covers editing,
  navigation, search, refactoring, debugging, testing, terminal and version
  control. Actions that were checked and have no VS Code equivalent are listed
  with the reason in `internal/convert/reviewed.json`. The report separates
  them from actions nobody has checked yet.
- **Plain keys without a context.** A shortcut without Ctrl, Alt or Cmd
  (Escape, Delete, arrows, F-keys) is written only when the mapping restricts
  it with a `when` clause. Otherwise it would take the key away from every
  other part of VS Code.
- **Mouse shortcuts.** VS Code keybindings cannot use the mouse.
- **Keys that depend on the keyboard layout, when the layout is unknown.** See
  [Keyboard layouts](#keyboard-layouts).
- **Removed defaults.** If you removed a default shortcut in JetBrains, the tool
  reports it but leaves VS Code's own default shortcuts alone.

Conflicts (one key bound to several commands in the same context) and keys you
already use in your own keybindings are reported too.

## Platforms

Binaries: macOS (arm64, amd64), Linux (amd64), Windows (amd64).

The tool has been used on macOS with real IDE installations. On Linux and
Windows it has only been tested with synthetic data. The paths it searches are
the documented defaults (Toolbox, `/opt`, `%LOCALAPPDATA%\Programs`, …). If your
IDE is somewhere else, use `--ide-home`.

VS Code Settings Sync synchronizes `keybindings.json`, by default separately for
each platform. After `--apply`, the new keybindings reach your other machines on
the same platform through Settings Sync.

## Installing

With Go 1.22 or newer:

```sh
go install github.com/Rashengka/jetbrains-keymap-to-vscode@latest
```

## Building

Go 1.22 or newer, no dependencies:

```sh
make build   # ./jetbrains-keymap-to-vscode
make test
make dist    # binaries for all platforms in dist/
```

## Conflicts with VS Code's own shortcuts

VS Code runs exactly one command per key press: the last matching rule whose
`when` clause is true, and your `keybindings.json` wins over the defaults. So
two actions never run at once. But a generated shortcut does override VS Code's
default on the same key wherever its `when` clause matches. The report lists:

- keys bound to different commands in the same context within the generated
  block,
- keys you already use in your own keybindings outside the block.

### Keeping a key for VS Code

If a generated shortcut takes a key you want VS Code to keep, leave it out:

```sh
jetbrains-keymap-to-vscode convert --ide phpstorm --keep-key cmd+g --apply
```

The list is stored on a comment line inside the managed block, so later runs
keep honoring it without the flag. `--unkeep-key cmd+g` removes the key from
the list. Keys may be written with layout characters: on a Czech layout,
`cmd+ě` is the same key as `cmd+[Digit2]`, and the tool resolves it the same
way it resolves JetBrains shortcuts. A kept single key also drops chords that
start with it.

If you prefer to fix a key by hand, add your own rule **below** the
`// <<< jetbrains-keymap-to-vscode` line. A rule further down wins, and the
tool never touches lines outside the block. Do not edit inside the block:
the next run replaces it.

### Using a keymap file

`--file` reads a keymap you prepared, for example exported from another
machine. The IDE is still used for the bundled keymaps it inherits from. With
`--scope custom` the file alone is enough.

## Maintaining the action table

- Every command in the table is checked against an installed VS Code by
  `go test ./internal/convert/ -run CommandsExist`. The test needs VS Code on
  the machine (set `VSCODE_APP` to its `app` directory) and is skipped
  otherwise, for example in CI.
- Actions that only work inside a JetBrains tool window (for example dropping
  a stash) are not mapped: the VS Code command would act on the same key
  everywhere.

## Known limitations

- Plugins you disabled in the IDE, and plugins you installed yourself, are not
  taken into account when collecting plugin default shortcuts.
- The action table is hand-made. Corrections and additions are welcome.

## Credits and license

MIT, see [LICENSE](LICENSE). The action table used the mapping of
[IntelliJ IDEA Keybindings](https://github.com/kasecato/vscode-intellij-idea-keybindings)
by Keisuke Kato (MIT) as a starting point, see [NOTICE](NOTICE).
