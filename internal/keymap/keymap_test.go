package keymap

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Synthetic keymaps modeled on the structure of bundled JetBrains keymaps.
const defaultXML = `<keymap version="1" name="$default">
  <action id="$Copy">
    <keyboard-shortcut first-keystroke="control C"/>
    <keyboard-shortcut first-keystroke="control INSERT"/>
  </action>
  <action id="GotoClass"><keyboard-shortcut first-keystroke="control N"/></action>
  <action id="ReformatCode"><keyboard-shortcut first-keystroke="control alt L"/></action>
  <action id="EditorDuplicate"><keyboard-shortcut first-keystroke="control D"/></action>
  <action id="Chord"><keyboard-shortcut first-keystroke="control K" second-keystroke="control C"/></action>
  <action id="WithMouse"><mouse-shortcut keystroke="control button1"/></action>
</keymap>`

const macXML = `<keymap name="Mac OS X 10.5+" parent="$default" version="1">
  <action id="GotoClass"><keyboard-shortcut first-keystroke="meta O"/></action>
</keymap>`

const userXML = `<keymap version="1" name="Mine" parent="Mac OS X 10.5+">
  <action id="EditorDuplicate">
    <keyboard-shortcut first-keystroke="meta D"/>
    <keyboard-shortcut first-keystroke="shift meta #1000161"/>
  </action>
  <action id="ReformatCode" />
</keymap>`

const pluginXML = `<idea-plugin>
  <actions>
    <group id="G">
      <action id="PluginAction" class="x">
        <keyboard-shortcut keymap="$default" first-keystroke="control shift P"/>
      </action>
      <action id="GotoClass" class="y">
        <keyboard-shortcut keymap="Mac OS X 10.5+" first-keystroke="meta shift O"/>
      </action>
      <action id="$Copy" class="z">
        <keyboard-shortcut keymap="$default" first-keystroke="control INSERT" remove="true"/>
      </action>
      <action id="Aliased" use-shortcut-of="GotoClass"/>
    </group>
  </actions>
</idea-plugin>`

func mustParse(t *testing.T, x string) *Keymap {
	t.Helper()
	k, err := ParseKeymap([]byte(x), "test")
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func testSet(t *testing.T) *Set {
	t.Helper()
	s := NewSet()
	s.AddKeymap(mustParse(t, defaultXML))
	s.AddKeymap(mustParse(t, macXML))
	u := mustParse(t, userXML)
	u.User = true
	s.AddKeymap(u)
	d, err := ParseDescriptor([]byte(pluginXML))
	if err != nil {
		t.Fatal(err)
	}
	s.AddDescriptor(d)
	return s
}

func keys(t *testing.T, s *Set, keymap, action string) []string {
	t.Helper()
	sc, err := s.Effective(keymap, action)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, k := range sc.Keys {
		out = append(out, k.String())
	}
	return out
}

func TestNormalizeKeystroke(t *testing.T) {
	cases := map[string]Keystroke{
		"meta shift Z":        "shift meta z",
		"control alt L":       "control alt l",
		"ctrl pressed A":      "control a",
		"shift meta #1000161": "shift meta #1000161",
		"":                    "",
	}
	for in, want := range cases {
		if got := NormalizeKeystroke(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestMacKeymapSwapsControlAndMetaFromDefault(t *testing.T) {
	s := testSet(t)
	if got := keys(t, s, "Mac OS X 10.5+", "EditorDuplicate"); !reflect.DeepEqual(got, []string{"meta d"}) {
		t.Errorf("got %v", got)
	}
	if got := keys(t, s, "Mac OS X 10.5+", "Chord"); !reflect.DeepEqual(got, []string{"meta k, meta c"}) {
		t.Errorf("chord: got %v", got)
	}
	sc, _ := s.Effective("Mac OS X 10.5+", "WithMouse")
	if !reflect.DeepEqual(sc.Mouse, []string{"meta button1"}) {
		t.Errorf("mouse: got %v", sc.Mouse)
	}
}

func TestPluginShortcuts(t *testing.T) {
	s := testSet(t)
	if got := keys(t, s, "$default", "PluginAction"); !reflect.DeepEqual(got, []string{"control shift p"}) {
		t.Errorf("added to $default: got %v", got)
	}
	if got := keys(t, s, "Mac OS X 10.5+", "PluginAction"); !reflect.DeepEqual(got, []string{"shift meta p"}) {
		t.Errorf("inherited and swapped: got %v", got)
	}
	if got := keys(t, s, "Mac OS X 10.5+", "GotoClass"); !reflect.DeepEqual(got, []string{"meta o", "shift meta o"}) {
		t.Errorf("added to own list: got %v", got)
	}
	if got := keys(t, s, "$default", "$Copy"); !reflect.DeepEqual(got, []string{"control c"}) {
		t.Errorf("remove: got %v", got)
	}
	if got := keys(t, s, "Mine", "Aliased"); !reflect.DeepEqual(got, []string{"meta o", "shift meta o"}) {
		t.Errorf("use-shortcut-of: got %v", got)
	}
}

func TestUserKeymapOverridesAndRemoves(t *testing.T) {
	s := testSet(t)
	if got := keys(t, s, "Mine", "EditorDuplicate"); !reflect.DeepEqual(got, []string{"meta d", "shift meta #1000161"}) {
		t.Errorf("override: got %v", got)
	}
	if got := keys(t, s, "Mine", "ReformatCode"); len(got) != 0 {
		t.Errorf("removed: got %v", got)
	}
	if got := keys(t, s, "Mine", "GotoClass"); !reflect.DeepEqual(got, []string{"meta o", "shift meta o"}) {
		t.Errorf("inherited without swap: got %v", got)
	}
}

func TestSelectScopes(t *testing.T) {
	s := testSet(t)
	custom, err := s.Select("Mine", ScopeCustom)
	if err != nil {
		t.Fatal(err)
	}
	if custom.Base != "Mac OS X 10.5+" || len(custom.Entries) != 2 {
		t.Fatalf("custom: %+v", custom)
	}
	var reformat Entry
	for _, e := range custom.Entries {
		if e.Action == "ReformatCode" {
			reformat = e
		}
	}
	if len(reformat.Removed) != 1 || reformat.Removed[0].String() != "alt meta l" {
		t.Errorf("removed default: %+v", reformat)
	}

	all, _ := s.Select("Mine", ScopeAll)
	def, _ := s.Select("Mine", ScopeDefault)
	if def.Keymap != "Mac OS X 10.5+" {
		t.Errorf("default scope keymap: %s", def.Keymap)
	}
	has := func(sel Selection, action string) bool {
		for _, e := range sel.Entries {
			if e.Action == action {
				return true
			}
		}
		return false
	}
	if has(all, "ReformatCode") || !has(def, "ReformatCode") {
		t.Error("ReformatCode is removed by the user: absent in all, present in default")
	}
	if !has(all, "PluginAction") || !has(def, "PluginAction") {
		t.Error("plugin shortcuts belong to both all and default")
	}
}

func TestCustomScopeWorksWithoutInstallation(t *testing.T) {
	s := NewSet()
	u := mustParse(t, userXML)
	u.User = true
	s.AddKeymap(u)
	sel, err := s.Select("Mine", ScopeCustom)
	if err != nil {
		t.Fatal(err)
	}
	if len(sel.Entries) != 2 || sel.Base != "" {
		t.Errorf("got %+v", sel)
	}
	if _, err := s.Select("Mine", ScopeAll); err == nil {
		t.Error("scope all needs the bundled keymaps")
	}
}

func TestLoadInstallReadsJars(t *testing.T) {
	home := t.TempDir()
	writeJar(t, filepath.Join(home, "lib", "platform.jar"), map[string]string{
		"keymaps/$default.xml":       defaultXML,
		"keymaps/Mac OS X 10.5+.xml": macXML,
		"idea/PlatformActions.xml":   pluginXML,
		"other/readme.xml":           "<x/>",
	})
	writeJar(t, filepath.Join(home, "plugins", "p", "lib", "p.jar"), map[string]string{
		"META-INF/plugin.xml": pluginXML, // duplicate descriptor, must not double anything
	})
	s := NewSet()
	st, err := LoadInstall(s, home)
	if err != nil {
		t.Fatal(err)
	}
	if st.Jars != 2 || st.Keymaps != 2 || st.Descriptors != 2 {
		t.Errorf("stats %+v", st)
	}
	if got := keys(t, s, "Mac OS X 10.5+", "GotoClass"); !reflect.DeepEqual(got, []string{"meta o", "shift meta o"}) {
		t.Errorf("got %v", got)
	}
	cfg := t.TempDir()
	os.MkdirAll(filepath.Join(cfg, "keymaps"), 0o755)
	os.WriteFile(filepath.Join(cfg, "keymaps", "Mine.xml"), []byte(userXML), 0o644)
	if err := LoadUserKeymaps(s, cfg); err != nil {
		t.Fatal(err)
	}
	if !s.Keymaps["Mine"].User {
		t.Error("user keymap not marked")
	}
}

func writeJar(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte(content))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// A key the user gave to one action can still carry an inherited action, like
// Find (editor) and FindInPath (everywhere) on cmd+f. The IDE runs the first
// enabled one, so custom scope must see the inherited action too.
func TestSelectCustomAddsInheritedActionOnSharedKey(t *testing.T) {
	s := NewSet()
	s.AddKeymap(mustParse(t, `<keymap version="1" name="$default">
  <action id="Find"><keyboard-shortcut first-keystroke="control F"/><keyboard-shortcut first-keystroke="alt F3"/></action>
  <action id="FindInPath"><keyboard-shortcut first-keystroke="control shift F"/></action>
  <action id="Other"><keyboard-shortcut first-keystroke="control O"/></action>
</keymap>`))
	s.AddKeymap(mustParse(t, `<keymap name="Mac OS X 10.5+" parent="$default" version="1"/>`))
	u := mustParse(t, `<keymap version="1" name="Mine" parent="Mac OS X 10.5+">
  <action id="FindInPath"><keyboard-shortcut first-keystroke="meta F"/></action>
</keymap>`)
	u.User = true
	s.AddKeymap(u)

	sel, err := s.Select("Mine", ScopeCustom)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range sel.Entries {
		got = append(got, fmt.Sprintf("%s %v %v", e.Action, e.Inherited, e.Shortcuts.Keys))
	}
	want := []string{"Find true [meta f]", "FindInPath false [meta f]"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entries:\n got %q\nwant %q", got, want)
	}
}
