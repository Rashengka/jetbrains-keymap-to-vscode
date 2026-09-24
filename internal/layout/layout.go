// Package layout maps characters to physical keys for keyboard layouts where
// JetBrains stores a shortcut as a character (for example "meta #100011b",
// which is Cmd+ě on a Czech keyboard).
//
// VS Code can bind physical keys regardless of layout ("cmd+[Digit2]"), so a
// character shortcut converts reliably once we know which key types the
// character. The tables in mac.json are generated from the layouts installed
// in macOS by tools/gen-mac-layouts.swift, not written by hand.
package layout

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

//go:embed mac.json
var macJSON []byte

// Layout is one keyboard layout.
type Layout struct {
	ID   string
	keys map[string][2]string // KeyboardEvent.code -> [unshifted, shifted]
}

// Key is a physical key, named like KeyboardEvent.code (e.g. "Digit2").
type Key struct {
	Code  string
	Shift bool // the character needs Shift
}

var aliases = map[string]string{
	"cz":        "com.apple.keylayout.Czech",
	"cz-qwertz": "com.apple.keylayout.Czech",
	"cz-qwerty": "com.apple.keylayout.Czech-QWERTY",
	"sk":        "com.apple.keylayout.Slovak",
	"sk-qwertz": "com.apple.keylayout.Slovak",
	"sk-qwerty": "com.apple.keylayout.Slovak-QWERTY",
}

func macLayouts() map[string]map[string][]string {
	var m map[string]map[string][]string
	if err := json.Unmarshal(macJSON, &m); err != nil {
		panic(err) // embedded data, covered by tests
	}
	return m
}

// Names lists the layouts that can be passed to --layout.
func Names() []string {
	var out []string
	for a := range aliases {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// Get returns a layout by alias ("cz-qwerty") or macOS input source id.
func Get(name string) (*Layout, error) {
	id := name
	if a, ok := aliases[strings.ToLower(name)]; ok {
		id = a
	}
	keys, ok := macLayouts()[id]
	if !ok {
		return nil, fmt.Errorf("no table for keyboard layout %q (known: %s)", name, strings.Join(Names(), ", "))
	}
	l := &Layout{ID: id, keys: map[string][2]string{}}
	for code, v := range keys {
		if len(v) == 2 {
			l.keys[code] = [2]string{v[0], v[1]}
		}
	}
	return l, nil
}

// Name is a short human-readable name, e.g. "Czech-QWERTY".
func (l *Layout) Name() string { return strings.TrimPrefix(l.ID, "com.apple.keylayout.") }

// Find returns the key that types r. A key typing r without Shift wins over
// one that needs Shift. Letters and digits are not looked up: VS Code already
// resolves them through the active layout.
func (l *Layout) Find(r rune) (Key, bool) {
	ch := string(r)
	var codes []string
	for code := range l.keys {
		codes = append(codes, code)
	}
	sort.Strings(codes) // deterministic when a character is on more than one key
	for _, code := range codes {
		if l.keys[code][0] == ch {
			return Key{Code: code}, true
		}
	}
	for _, code := range codes {
		if l.keys[code][1] == ch {
			return Key{Code: code, Shift: true}, true
		}
	}
	return Key{}, false
}

// DetectMac asks macOS for the current keyboard layout. It returns "" when it
// cannot tell (the caller then treats layout-dependent keys as unconvertible).
func DetectMac() string {
	out, err := exec.Command("defaults", "read", "com.apple.HIToolbox", "AppleCurrentKeyboardLayoutInputSourceID").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
