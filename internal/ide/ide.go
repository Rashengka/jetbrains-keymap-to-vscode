// Package ide finds installed JetBrains IDEs and their configuration directories.
//
// JetBrains keeps one configuration directory per product and major version
// (for example "PhpStorm2026.1" and "PhpStorm2026.2") and does not remove the
// old ones after an upgrade. Detection therefore groups directories by product
// and always picks the newest version.
package ide

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Version is the major version of a JetBrains product, e.g. 2026.2.
type Version struct {
	Year  int
	Minor int
}

func (v Version) String() string { return fmt.Sprintf("%d.%d", v.Year, v.Minor) }

// Less compares versions numerically, so 2026.10 is newer than 2026.9.
func (v Version) Less(o Version) bool {
	if v.Year != o.Year {
		return v.Year < o.Year
	}
	return v.Minor < o.Minor
}

// Config is one versioned configuration directory, e.g. ".../JetBrains/PhpStorm2026.2".
type Config struct {
	Product string // "PhpStorm", "IntelliJIdea", ...
	Version Version
	Dir     string
	// HasSettings is false for directories without options/ and keymaps/,
	// e.g. an IDE that was started but never configured.
	HasSettings bool
}

// Install is one IDE installation described by its product-info.json.
type Install struct {
	Name     string // "PhpStorm"
	Version  string // "2026.2.3"
	DataDir  string // "PhpStorm2026.2", matches Config directory name
	Home     string // directory containing lib/ and plugins/
	InfoPath string
}

// IDE is a detected product: its newest configuration and the matching installation.
type IDE struct {
	Product string
	Config  Config
	Install *Install // nil when no matching installation was found
	Older   []Config // older configuration directories that were ignored
}

// DisplayName is the human-friendly product name.
func (i IDE) DisplayName() string {
	if i.Install != nil && i.Install.Name != "" {
		return i.Install.Name
	}
	return i.Product
}

var configDirRe = regexp.MustCompile(`^([A-Za-z][A-Za-z-]*?)(\d{4})\.(\d+)$`)

// ParseConfigDirName splits "PhpStorm2026.2" into product and version.
func ParseConfigDirName(name string) (string, Version, bool) {
	m := configDirRe.FindStringSubmatch(name)
	if m == nil {
		return "", Version{}, false
	}
	year, _ := strconv.Atoi(m[2])
	minor, _ := strconv.Atoi(m[3])
	return m[1], Version{year, minor}, true
}

// DefaultConfigRoot returns the directory holding JetBrains configuration directories.
func DefaultConfigRoot() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "JetBrains"), nil
}

// ScanConfigs lists versioned configuration directories under root.
func ScanConfigs(root string) ([]Config, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []Config
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		product, v, ok := ParseConfigDirName(e.Name())
		if !ok {
			continue
		}
		dir := filepath.Join(root, e.Name())
		has := isDir(filepath.Join(dir, "options")) || isDir(filepath.Join(dir, "keymaps"))
		out = append(out, Config{Product: product, Version: v, Dir: dir, HasSettings: has})
	}
	return out, nil
}

// DefaultInstallGlobs returns glob patterns matching product-info.json of installed IDEs.
func DefaultInstallGlobs() []string {
	home, _ := os.UserHomeDir()
	var g []string
	switch runtime.GOOS {
	case "darwin":
		tb := filepath.Join(home, "Library", "Application Support", "JetBrains", "Toolbox", "apps")
		for _, root := range []string{"/Applications", filepath.Join(home, "Applications"), filepath.Join(home, "Applications", "JetBrains Toolbox")} {
			g = append(g, filepath.Join(root, "*.app", "Contents", "Resources", "product-info.json"))
		}
		g = append(g,
			filepath.Join(tb, "*", "*.app", "Contents", "Resources", "product-info.json"),
			filepath.Join(tb, "*", "ch-*", "*", "*.app", "Contents", "Resources", "product-info.json"),
		)
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		tb := filepath.Join(local, "JetBrains", "Toolbox", "apps")
		g = append(g,
			filepath.Join(local, "Programs", "*", "product-info.json"),
			filepath.Join(tb, "*", "product-info.json"),
			filepath.Join(tb, "*", "ch-*", "*", "product-info.json"),
		)
		for _, pf := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if pf != "" {
				g = append(g, filepath.Join(pf, "JetBrains", "*", "product-info.json"))
			}
		}
	default:
		tb := filepath.Join(home, ".local", "share", "JetBrains", "Toolbox", "apps")
		g = append(g,
			filepath.Join(tb, "*", "product-info.json"),
			filepath.Join(tb, "*", "ch-*", "*", "product-info.json"),
			filepath.Join(home, ".local", "share", "JetBrains", "*", "product-info.json"),
			"/opt/*/product-info.json",
			"/opt/jetbrains/*/product-info.json",
			"/snap/*/current/product-info.json",
		)
	}
	return g
}

type productInfo struct {
	Name              string `json:"name"`
	Version           string `json:"version"`
	DataDirectoryName string `json:"dataDirectoryName"`
	ProductVendor     string `json:"productVendor"`
}

// ScanInstalls reads every product-info.json matched by the glob patterns.
func ScanInstalls(globs []string) []Install {
	seen := map[string]bool{}
	var out []Install
	for _, pattern := range globs {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			if seen[path] {
				continue
			}
			seen[path] = true
			in, err := ReadInstall(path)
			if err == nil {
				out = append(out, in)
			}
		}
	}
	return out
}

// ReadInstall reads one product-info.json. The IDE home is the directory with lib/.
func ReadInstall(infoPath string) (Install, error) {
	data, err := os.ReadFile(infoPath)
	if err != nil {
		return Install{}, err
	}
	var pi productInfo
	if err := json.Unmarshal(data, &pi); err != nil {
		return Install{}, fmt.Errorf("%s: %w", infoPath, err)
	}
	if pi.DataDirectoryName == "" {
		return Install{}, fmt.Errorf("%s: no dataDirectoryName", infoPath)
	}
	home := filepath.Dir(infoPath)
	if filepath.Base(home) == "Resources" { // macOS: Contents/Resources/product-info.json
		home = filepath.Dir(home)
	}
	return Install{Name: pi.Name, Version: pi.Version, DataDir: pi.DataDirectoryName, Home: home, InfoPath: infoPath}, nil
}

// Detect groups configurations by product, keeps the newest version of each product
// and attaches the newest installation whose data directory matches it.
// A directory without settings counts only when an installation matches it;
// otherwise it is a leftover and would hide the real configuration.
func Detect(configs []Config, installs []Install) []IDE {
	installed := map[string]bool{}
	for _, in := range installs {
		installed[in.DataDir] = true
	}
	byProduct := map[string][]Config{}
	for _, c := range configs {
		if !c.HasSettings && !installed[filepath.Base(c.Dir)] {
			continue
		}
		byProduct[c.Product] = append(byProduct[c.Product], c)
	}
	var out []IDE
	for product, list := range byProduct {
		sort.Slice(list, func(i, j int) bool { return list[j].Version.Less(list[i].Version) })
		newest := list[0]
		ide := IDE{Product: product, Config: newest, Older: list[1:]}
		for i := range installs {
			in := installs[i]
			if in.DataDir != filepath.Base(newest.Dir) {
				continue
			}
			if ide.Install == nil || CompareReleases(ide.Install.Version, in.Version) < 0 {
				ide.Install = &in
			}
		}
		out = append(out, ide)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].DisplayName()) < strings.ToLower(out[j].DisplayName())
	})
	return out
}

// CompareReleases compares dotted versions like "2026.2.3" numerically.
func CompareReleases(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Find picks an IDE by name, case-insensitively. It matches the product name,
// the configuration directory name or a unique prefix of either.
func Find(ides []IDE, name string) (IDE, error) {
	n := strings.ToLower(name)
	var prefix []IDE
	for _, i := range ides {
		candidates := []string{strings.ToLower(i.Product), strings.ToLower(i.DisplayName()), strings.ToLower(filepath.Base(i.Config.Dir))}
		for _, c := range candidates {
			if c == n {
				return i, nil
			}
		}
		for _, c := range candidates {
			if strings.HasPrefix(c, n) {
				prefix = append(prefix, i)
				break
			}
		}
	}
	if len(prefix) == 1 {
		return prefix[0], nil
	}
	if len(prefix) > 1 {
		return IDE{}, fmt.Errorf("%q matches more than one IDE", name)
	}
	return IDE{}, fmt.Errorf("no IDE matches %q", name)
}

// ActiveKeymapName reads the keymap selected in the IDE settings.
// It returns "" when the IDE uses its built-in default.
func ActiveKeymapName(configDir string) string {
	osDir := map[string]string{"darwin": "mac", "windows": "windows", "linux": "linux"}[runtime.GOOS]
	candidates := []string{filepath.Join(configDir, "options", "keymap.xml")}
	if osDir != "" {
		candidates = append([]string{filepath.Join(configDir, "options", osDir, "keymap.xml")}, candidates...)
	}
	re := regexp.MustCompile(`<active_keymap\s+name="([^"]*)"`)
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if m := re.FindSubmatch(data); m != nil {
			return unescapeXMLAttr(string(m[1]))
		}
	}
	return ""
}

// DefaultKeymapName is the keymap JetBrains uses when none is selected.
func DefaultKeymapName() string {
	if runtime.GOOS == "darwin" {
		return "Mac OS X 10.5+"
	}
	return "$default"
}

func unescapeXMLAttr(s string) string {
	r := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'")
	return r.Replace(s)
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
