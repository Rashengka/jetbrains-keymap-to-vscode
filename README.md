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
- **Restore.** `restore` lists backups, and `restore <backup> --apply` puts one
  back. The current file is backed up first, so a restore can be undone too.

## Usage

```sh
# Which IDEs were found, and which keymap is active in each
jetbrains-keymap-to-vscode list

# Show what would be written (asks which IDE to use if there are several)
jetbrains-keymap-to-vscode convert
jetbrains-keymap-to-vscode convert --ide phpstorm

# Write it
jetbrains-keymap-to-vscode convert --ide phpstorm --apply

# List backups and restore one
jetbrains-keymap-to-vscode restore
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
| `--keymap NAME` | Read this keymap instead of the one active in the IDE |
| `--config-root DIR` | Directory with JetBrains configuration directories |
| `--ide-home DIR` | IDE installation directory (the one containing `lib/`) |
| `-v` | List every skipped shortcut in the report |

## What is not converted

The report lists all of these:

- **Actions without a VS Code counterpart.** The action table
  (`internal/convert/actions.json`) covers common editing, navigation, search,
  refactoring, debugging and version-control actions. Many JetBrains actions
  have no equivalent in VS Code.
- **Mouse shortcuts.** VS Code keybindings cannot use the mouse.
- **Keys that depend on the keyboard layout.** JetBrains stores keys such as
  `+`, `(` or national characters (`š`, `ě`, `ú`, …) as characters. VS Code
  binds physical keys, so these cannot be converted reliably.
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

## Known limitations

- Plugins you disabled in the IDE, and plugins you installed yourself, are not
  taken into account when collecting plugin default shortcuts.
- The action table is hand-made. Corrections and additions are welcome.

## Credits and license

MIT, see [LICENSE](LICENSE). The action table used the mapping of
[IntelliJ IDEA Keybindings](https://github.com/kasecato/vscode-intellij-idea-keybindings)
by Keisuke Kato (MIT) as a starting point, see [NOTICE](NOTICE).
