package ide

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfigDirName(t *testing.T) {
	cases := []struct {
		in      string
		product string
		version Version
		ok      bool
	}{
		{"PhpStorm2026.2", "PhpStorm", Version{2026, 2}, true},
		{"IntelliJIdea2025.10", "IntelliJIdea", Version{2025, 10}, true},
		{"Phpstorm", "", Version{}, false},
		{"Toolbox", "", Version{}, false},
		{"consentOptions", "", Version{}, false},
	}
	for _, c := range cases {
		p, v, ok := ParseConfigDirName(c.in)
		if p != c.product || v != c.version || ok != c.ok {
			t.Errorf("%s: got %q %v %v", c.in, p, v, ok)
		}
	}
}

func TestVersionComparesNumerically(t *testing.T) {
	if !(Version{2026, 9}).Less(Version{2026, 10}) {
		t.Error("2026.9 should be older than 2026.10")
	}
	if CompareReleases("2026.2.10", "2026.2.9") != 1 {
		t.Error("2026.2.10 should be newer than 2026.2.9")
	}
}

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDetectPicksNewestVersion(t *testing.T) {
	root := t.TempDir()
	mkdirs(t,
		filepath.Join(root, "PhpStorm2025.3", "options"),
		filepath.Join(root, "PhpStorm2026.10", "keymaps"),
		filepath.Join(root, "PhpStorm2026.9", "options"),
		filepath.Join(root, "GoLand2026.2", "options"),
		filepath.Join(root, "Phpstorm", "consentOptions"),
		filepath.Join(root, "WebStorm2026.1"), // empty and not installed: a leftover
		filepath.Join(root, "GoLand2026.3"),   // empty but installed: never configured
		filepath.Join(root, "DataGrip2026.2"), // empty but installed
	)
	configs, err := ScanConfigs(root)
	if err != nil {
		t.Fatal(err)
	}
	installs := []Install{
		{Name: "PhpStorm", Version: "2026.9.1", DataDir: "PhpStorm2026.9", Home: "/old"},
		{Name: "PhpStorm", Version: "2026.10.1", DataDir: "PhpStorm2026.10", Home: "/a"},
		{Name: "PhpStorm", Version: "2026.10.3", DataDir: "PhpStorm2026.10", Home: "/b"},
		{Name: "GoLand", Version: "2026.3.0", DataDir: "GoLand2026.3", Home: "/g"},
		{Name: "DataGrip", Version: "2026.2.5", DataDir: "DataGrip2026.2", Home: "/d"},
	}
	ides := Detect(configs, installs)
	if len(ides) != 3 {
		t.Fatalf("want 3 IDEs, got %d: %+v", len(ides), ides)
	}
	ps, err := Find(ides, "phpstorm")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(ps.Config.Dir) != "PhpStorm2026.10" {
		t.Errorf("newest config: got %s", ps.Config.Dir)
	}
	if ps.Install == nil || ps.Install.Home != "/b" {
		t.Errorf("newest install: got %+v", ps.Install)
	}
	if len(ps.Older) != 2 {
		t.Errorf("older: got %d", len(ps.Older))
	}
	gl, _ := Find(ides, "goland")
	if filepath.Base(gl.Config.Dir) != "GoLand2026.3" || gl.Install == nil {
		t.Errorf("GoLand: installed newer version wins even without settings, got %+v", gl)
	}
	if _, err := Find(ides, "webstorm"); err == nil {
		t.Error("leftover WebStorm directory should be ignored")
	}
}

func TestReadInstallMacLayout(t *testing.T) {
	root := t.TempDir()
	res := filepath.Join(root, "PhpStorm.app", "Contents", "Resources")
	mkdirs(t, res)
	info := `{"name":"PhpStorm","version":"2026.2.3","dataDirectoryName":"PhpStorm2026.2"}`
	if err := os.WriteFile(filepath.Join(res, "product-info.json"), []byte(info), 0o644); err != nil {
		t.Fatal(err)
	}
	ins := ScanInstalls([]string{filepath.Join(root, "*.app", "Contents", "Resources", "product-info.json")})
	if len(ins) != 1 || ins[0].Home != filepath.Join(root, "PhpStorm.app", "Contents") {
		t.Fatalf("got %+v", ins)
	}
}

func TestActiveKeymapName(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, filepath.Join(dir, "options"))
	xml := `<application><component name="KeymapManager"><active_keymap name="My &amp; keys" /></component></application>`
	if err := os.WriteFile(filepath.Join(dir, "options", "keymap.xml"), []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ActiveKeymapName(dir); got != "My & keys" {
		t.Errorf("got %q", got)
	}
}
