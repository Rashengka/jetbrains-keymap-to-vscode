package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic fixtures only: no real keymap goes into the repository.
const fxDefault = `<keymap version="1" name="$default">
  <action id="EditorDuplicate"><keyboard-shortcut first-keystroke="control D"/></action>
  <action id="GotoClass"><keyboard-shortcut first-keystroke="control N"/></action>
</keymap>`

const fxMac = `<keymap version="1" name="Mac OS X 10.5+" parent="$default">
  <action id="GotoClass"><keyboard-shortcut first-keystroke="meta O"/></action>
</keymap>`

const fxUser = `<keymap version="1" name="Custom" parent="Mac OS X 10.5+">
  <action id="EditorDuplicate"><keyboard-shortcut first-keystroke="shift meta D"/></action>
  <action id="GotoFile"><keyboard-shortcut first-keystroke="meta #100011b"/></action>
</keymap>`

const fxUserKeybindings = `// my file
[
  { "key": "ctrl+alt+x", "command": "my.command" } // keep me
]
`

type fixture struct {
	configRoot, ideHome, keybindings string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	f := fixture{
		configRoot:  filepath.Join(dir, "JetBrains"),
		ideHome:     filepath.Join(dir, "ide"),
		keybindings: filepath.Join(dir, "User", "keybindings.json"),
	}
	for _, v := range []string{"TestIDE2025.3", "TestIDE2026.1"} {
		cfg := filepath.Join(f.configRoot, v)
		must(t, os.MkdirAll(filepath.Join(cfg, "keymaps"), 0o755))
		must(t, os.MkdirAll(filepath.Join(cfg, "options", "mac"), 0o755))
		must(t, os.WriteFile(filepath.Join(cfg, "keymaps", "Custom.xml"), []byte(fxUser), 0o644))
		active := `<application><component name="KeymapManager"><active_keymap name="Custom"/></component></application>`
		must(t, os.WriteFile(filepath.Join(cfg, "options", "mac", "keymap.xml"), []byte(active), 0o644))
		must(t, os.WriteFile(filepath.Join(cfg, "options", "keymap.xml"), []byte(active), 0o644))
	}
	must(t, os.MkdirAll(filepath.Join(f.ideHome, "lib"), 0o755))
	jar, err := os.Create(filepath.Join(f.ideHome, "lib", "platform.jar"))
	must(t, err)
	w := zip.NewWriter(jar)
	for name, content := range map[string]string{"keymaps/$default.xml": fxDefault, "keymaps/Mac OS X 10.5+.xml": fxMac} {
		fw, err := w.Create(name)
		must(t, err)
		fw.Write([]byte(content))
	}
	must(t, w.Close())
	must(t, jar.Close())
	must(t, os.MkdirAll(filepath.Dir(f.keybindings), 0o755))
	must(t, os.WriteFile(f.keybindings, []byte(fxUserKeybindings), 0o644))
	return f
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (f fixture) run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(""), &out, &errOut)
	return out.String() + errOut.String(), code
}

func (f fixture) convertArgs(extra ...string) []string {
	return append([]string{"convert", "--config-root", f.configRoot, "--ide-home", f.ideHome,
		"--keybindings", f.keybindings, "--ide", "testide", "--keymap", "Custom", "--platform", "darwin", "--layout", "none"}, extra...)
}

func TestConvertDryRunWritesNothing(t *testing.T) {
	f := newFixture(t)
	out, code := f.run(t, f.convertArgs()...)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	for _, want := range []string{"TestIDE 2026.1", "ignored older version TestIDE2025.3", "shift+cmd+d", "Dry run: nothing was written"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
	if b, _ := os.ReadFile(f.keybindings); string(b) != fxUserKeybindings {
		t.Error("dry run changed keybindings.json")
	}
	if m, _ := filepath.Glob(f.keybindings + ".*.bak"); len(m) != 0 {
		t.Errorf("dry run created backups: %v", m)
	}
}

func TestConvertApplyBacksUpAndRestoreWorks(t *testing.T) {
	f := newFixture(t)
	out, code := f.run(t, f.convertArgs("--apply", "--scope", "all")...)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	backups, _ := filepath.Glob(f.keybindings + ".*.bak")
	if len(backups) != 1 {
		t.Fatalf("want one backup, got %v", backups)
	}
	if b, _ := os.ReadFile(backups[0]); string(b) != fxUserKeybindings {
		t.Error("backup is not the original file")
	}
	written, _ := os.ReadFile(f.keybindings)
	for _, want := range []string{"// my file", "// keep me", `"shift+cmd+d"`, `"cmd+o"`} {
		if !strings.Contains(string(written), want) {
			t.Errorf("missing %q in written file:\n%s", want, written)
		}
	}

	out, code = f.run(t, f.convertArgs("--apply", "--scope", "all")...)
	if code != 0 || !strings.Contains(out, "already up to date") {
		t.Errorf("second run should be a no-op: %s", out)
	}

	out, _ = f.run(t, "restore", "--keybindings", f.keybindings, "-v")
	if !strings.Contains(out, "Dry run") || !strings.Contains(out, "using the newest one") || !strings.Contains(out, "- cmd+o") {
		t.Errorf("restore without --apply must be a dry run of the newest backup: %s", out)
	}
	if b, _ := os.ReadFile(f.keybindings); string(b) != string(written) {
		t.Error("dry-run restore changed the file")
	}
	out, code = f.run(t, "restore", "--keybindings", f.keybindings, "--apply")
	if code != 0 {
		t.Fatalf("restore failed: %s", out)
	}
	if b, _ := os.ReadFile(f.keybindings); string(b) != fxUserKeybindings {
		t.Error("restore did not bring back the original")
	}
	if b, _ := filepath.Glob(f.keybindings + ".*.bak"); len(b) != 2 {
		t.Errorf("restore must back up the current file first, backups: %v", b)
	}
	// An explicit name still works.
	out, code = f.run(t, "restore", filepath.Base(backups[0]), "--keybindings", f.keybindings)
	if code != 0 || !strings.Contains(out, "nothing to do") {
		t.Errorf("explicit backup: %d %s", code, out)
	}
}

func TestConvertVerboseShowsJSONBlock(t *testing.T) {
	f := newFixture(t)
	out, _ := f.run(t, f.convertArgs()...)
	if strings.Contains(out, `"key":`) {
		t.Error("the default dry run shows the short table, not JSON")
	}
	out, _ = f.run(t, f.convertArgs("-v")...)
	if !strings.Contains(out, `"key": "shift+cmd+d"`) {
		t.Errorf("-v must show the JSON block: %s", out)
	}
}

func TestConvertLayoutDependentKeys(t *testing.T) {
	f := newFixture(t)
	out, _ := f.run(t, f.convertArgs()...) // --layout none
	if !strings.Contains(out, "U+011B") || strings.Contains(out, "[Digit2]") {
		t.Errorf("without a layout the key must be reported:\n%s", out)
	}
	out, code := f.run(t, f.convertArgs("--layout", "cz-qwerty")...)
	if code != 0 || !strings.Contains(out, "cmd+[Digit2]") || !strings.Contains(out, "Layout:  Czech-QWERTY") {
		t.Errorf("with cz-qwerty ě must become cmd+[Digit2]:\n%s", out)
	}
	args := f.convertArgs("--layout", "cz-qwerty")
	for i, a := range args {
		if a == "darwin" {
			args[i] = "windows"
		}
	}
	if out, code := f.run(t, args...); code == 0 || !strings.Contains(out, "macOS only") {
		t.Errorf("layout tables must be refused for other platforms: %d %s", code, out)
	}
}

func TestConvertRefusesBrokenKeybindings(t *testing.T) {
	f := newFixture(t)
	broken := `[ { "key": "a", "command": ` // truncated
	must(t, os.WriteFile(f.keybindings, []byte(broken), 0o644))
	out, code := f.run(t, f.convertArgs("--apply")...)
	if code == 0 {
		t.Fatalf("expected failure: %s", out)
	}
	if b, _ := os.ReadFile(f.keybindings); string(b) != broken {
		t.Error("broken file was changed")
	}
}

func TestConvertNeedsIDEChoiceWithoutTerminal(t *testing.T) {
	f := newFixture(t)
	must(t, os.MkdirAll(filepath.Join(f.configRoot, "OtherIDE2026.1", "options"), 0o755))
	args := []string{"convert", "--config-root", f.configRoot, "--ide-home", f.ideHome, "--keybindings", f.keybindings}
	out, code := f.run(t, args...)
	if code == 0 || !strings.Contains(out, "--ide") {
		t.Errorf("expected a request for --ide, got %d: %s", code, out)
	}
}

func TestKeepKeyIsRememberedAndMatchesLayoutCharacters(t *testing.T) {
	f := newFixture(t)
	args := func(extra ...string) []string {
		a := f.convertArgs("--layout", "cz-qwerty", "--apply")
		return append(a, extra...)
	}
	// GotoFile is bound to Cmd+ě (meta #100011b) in the fixture; keep it via the character.
	out, code := f.run(t, args("--keep-key", "cmd+ě")...)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	written, _ := os.ReadFile(f.keybindings)
	if strings.Contains(string(written), "[Digit2]\"") || !strings.Contains(string(written), "// keep for VS Code: cmd+[Digit2]") {
		t.Errorf("kept key must not be written and must be remembered:\n%s", written)
	}
	// Next run without the flag still keeps it.
	out, _ = f.run(t, args()...)
	if !strings.Contains(out, "already up to date") {
		t.Errorf("keep list must survive the next run: %s", out)
	}
	// Unkeep with the physical key name brings the binding back.
	out, code = f.run(t, args("--unkeep-key", "cmd+[Digit2]")...)
	written, _ = os.ReadFile(f.keybindings)
	if code != 0 || !strings.Contains(string(written), `"cmd+[Digit2]"`) || strings.Contains(string(written), "keep for VS Code") {
		t.Errorf("unkeep failed (%d): %s\n%s", code, out, written)
	}
}

func TestKeepKeyNeedsLayoutForCharacters(t *testing.T) {
	f := newFixture(t)
	out, code := f.run(t, f.convertArgs("--keep-key", "cmd+ě")...) // --layout none
	if code == 0 || !strings.Contains(out, "depends on the keyboard layout") {
		t.Errorf("expected a clear error, got %d: %s", code, out)
	}
}

func TestFileInput(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	xmlPath := filepath.Join(dir, "prepared.xml")
	must(t, os.WriteFile(xmlPath, []byte(strings.Replace(fxUser, `name="Custom"`, `name="Prepared"`, 1)), 0o644))

	args := []string{"convert", "--config-root", f.configRoot, "--ide-home", f.ideHome, "--ide", "testide",
		"--keybindings", f.keybindings, "--platform", "darwin", "--layout", "none", "--file", xmlPath}
	out, code := f.run(t, args...)
	if code != 0 || !strings.Contains(out, `Keymap:  "Prepared"`) || !strings.Contains(out, "shift+cmd+d") {
		t.Errorf("xml input (%d): %s", code, out)
	}

	// A settings export zip with the active keymap in options/.
	zipPath := filepath.Join(dir, "settings.zip")
	zf, err := os.Create(zipPath)
	must(t, err)
	w := zip.NewWriter(zf)
	for name, content := range map[string]string{
		"keymaps/Custom.xml":     fxUser,
		"keymaps/Other.xml":      `<keymap version="1" name="Other" parent="$default"/>`,
		"options/keymap.xml":     `<application><component name="KeymapManager"><active_keymap name="Custom"/></component></application>`,
		"options/mac/keymap.xml": `<application><component name="KeymapManager"><active_keymap name="Custom"/></component></application>`,
	} {
		fw, err := w.Create(name)
		must(t, err)
		fw.Write([]byte(content))
	}
	must(t, w.Close())
	must(t, zf.Close())
	args[len(args)-1] = zipPath
	out, code = f.run(t, args...)
	if code != 0 || !strings.Contains(out, `Keymap:  "Custom"`) {
		t.Errorf("zip input (%d): %s", code, out)
	}

	// Without any IDE, --scope custom still works from the file alone.
	out, code = f.run(t, "convert", "--config-root", filepath.Join(dir, "none"), "--keybindings", f.keybindings,
		"--platform", "darwin", "--layout", "none", "--file", xmlPath)
	if code != 0 || !strings.Contains(out, "shift+cmd+d") {
		t.Errorf("file without IDE (%d): %s", code, out)
	}
}

func TestFileInputVSCodeKeybindings(t *testing.T) {
	f := newFixture(t)
	prepared := filepath.Join(t.TempDir(), "keybindings.json")
	must(t, os.WriteFile(prepared, []byte(`// prepared on another machine
[
  { "key": "cmd+shift+g", "command": "workbench.action.gotoLine" },
  { "key": "ctrl+alt+t", "command": "workbench.action.terminal.new", "when": "!terminalFocus" }, // trailing comma next
]
`), 0o644))
	base := []string{"convert", "--keybindings", f.keybindings, "--platform", "darwin", "--layout", "none", "--file", prepared}

	out, code := f.run(t, append(base, "--apply")...)
	if code != 0 || !strings.Contains(out, "copied as they are") {
		t.Fatalf("exit %d: %s", code, out)
	}
	written, _ := os.ReadFile(f.keybindings)
	for _, want := range []string{"// my file", "copied from file keybindings.json", `"key": "cmd+shift+g"`, `"when": "!terminalFocus"`} {
		if !strings.Contains(string(written), want) {
			t.Errorf("missing %q:\n%s", want, written)
		}
	}
	// --keep-key matches the prepared key even though it is written in another order.
	out, _ = f.run(t, append(base, "--keep-key", "shift+cmd+g", "--apply")...)
	written, _ = os.ReadFile(f.keybindings)
	if strings.Contains(string(written), `"key": "cmd+shift+g"`) || !strings.Contains(out, "not written") {
		t.Errorf("kept key still written:\n%s\n%s", out, written)
	}

	// The target itself cannot be the input.
	out, code = f.run(t, "convert", "--keybindings", f.keybindings, "--file", f.keybindings, "--layout", "none")
	if code == 0 || !strings.Contains(out, "being written") {
		t.Errorf("same file must be refused: %d %s", code, out)
	}
	// Unknown extension.
	out, code = f.run(t, "convert", "--keybindings", f.keybindings, "--file", "keys.txt")
	if code == 0 || !strings.Contains(out, "unknown type") {
		t.Errorf("unknown type must be refused: %d %s", code, out)
	}
}
