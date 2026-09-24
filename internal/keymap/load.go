package keymap

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// LoadUserKeymaps reads the user's keymaps from <config>/keymaps/*.xml.
// A missing directory is not an error: the user may never have customized a keymap.
func LoadUserKeymaps(s *Set, configDir string) error {
	files, err := filepath.Glob(filepath.Join(configDir, "keymaps", "*.xml"))
	if err != nil {
		return err
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		k, err := ParseKeymap(data, f)
		if err != nil {
			return err
		}
		k.User = true
		s.AddKeymap(k)
	}
	return nil
}

// LoadStats describes what was read from an IDE installation.
type LoadStats struct {
	Jars        int
	Keymaps     int
	Descriptors int
	Warnings    []string
}

const maxDescriptorSize = 4 << 20

// LoadInstall reads bundled keymaps and plugin shortcuts from every jar under
// <home>/lib and <home>/plugins.
func LoadInstall(s *Set, home string) (LoadStats, error) {
	var st LoadStats
	var jars []string
	for _, sub := range []string{"lib", "plugins"} {
		root := filepath.Join(home, sub)
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && d.Name() == "jbr" {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(p, ".jar") {
				jars = append(jars, p)
			}
			return nil
		})
		if err != nil {
			return st, err
		}
	}
	if len(jars) == 0 {
		return st, fmt.Errorf("no jars found under %s", home)
	}
	for _, j := range jars {
		if err := loadJar(s, j, &st); err != nil {
			st.Warnings = append(st.Warnings, err.Error())
		}
		st.Jars++
	}
	return st, nil
}

func loadJar(s *Set, path string, st *LoadStats) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	defer r.Close()
	for _, f := range r.File {
		if !strings.HasSuffix(f.Name, ".xml") || f.UncompressedSize64 > maxDescriptorSize {
			continue
		}
		// Descriptors are not only in META-INF: action groups are often
		// xi:included from other paths (e.g. idea/PlatformActions.xml).
		isKeymap := strings.HasPrefix(f.Name, "keymaps/")
		data, err := readZipFile(f)
		if err != nil {
			return fmt.Errorf("%s!%s: %w", path, f.Name, err)
		}
		source := path + "!" + f.Name
		if isKeymap {
			k, err := ParseKeymap(data, source)
			if err != nil {
				st.Warnings = append(st.Warnings, err.Error())
				continue
			}
			s.AddKeymap(k)
			st.Keymaps++
			continue
		}
		if !bytes.Contains(data, []byte("<keyboard-shortcut")) && !bytes.Contains(data, []byte("use-shortcut-of")) {
			continue
		}
		d, err := ParseDescriptor(data)
		if err != nil {
			st.Warnings = append(st.Warnings, fmt.Sprintf("%s: %v", source, err))
		}
		s.AddDescriptor(d)
		st.Descriptors++
	}
	return nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// LoadFile reads keymaps from a file the user prepared: a single keymap .xml,
// or a JetBrains settings export (.zip, File | Manage IDE Settings | Export
// Settings). It returns the keymap to use: the one selected in the export, or
// the only keymap in the file. The keymaps are treated as the user's own.
func LoadFile(s *Set, path string) (string, error) {
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		return loadSettingsZip(s, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	k, err := ParseKeymap(data, path)
	if err != nil {
		return "", err
	}
	k.User = true
	s.AddKeymap(k)
	return k.Name, nil
}

var activeKeymapRe = regexp.MustCompile(`<active_keymap\s+name="([^"]*)"`)

func loadSettingsZip(s *Set, path string) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	var names []string
	active := map[string]string{}
	for _, f := range r.File {
		switch {
		case strings.HasPrefix(f.Name, "keymaps/") && strings.HasSuffix(f.Name, ".xml"):
			data, err := readZipFile(f)
			if err != nil {
				return "", err
			}
			k, err := ParseKeymap(data, path+"!"+f.Name)
			if err != nil {
				return "", err
			}
			k.User = true
			s.AddKeymap(k)
			names = append(names, k.Name)
		case strings.HasPrefix(f.Name, "options/") && strings.HasSuffix(f.Name, "/keymap.xml") || f.Name == "options/keymap.xml":
			data, err := readZipFile(f)
			if err != nil {
				return "", err
			}
			if m := activeKeymapRe.FindSubmatch(data); m != nil {
				active[f.Name] = html.UnescapeString(string(m[1]))
			}
		}
	}
	osDir := map[string]string{"darwin": "mac", "windows": "windows", "linux": "linux"}[runtime.GOOS]
	for _, candidate := range []string{"options/" + osDir + "/keymap.xml", "options/keymap.xml"} {
		if a := active[candidate]; a != "" {
			return a, nil
		}
	}
	if len(names) == 1 {
		return names[0], nil
	}
	if len(names) == 0 {
		return "", fmt.Errorf("%s contains no keymaps (keymaps/*.xml)", path)
	}
	return "", fmt.Errorf("%s contains several keymaps (%s); choose one with --keymap", path, strings.Join(names, ", "))
}
