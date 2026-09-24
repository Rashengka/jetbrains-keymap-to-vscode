// Package keymap reads JetBrains keymaps and resolves the effective shortcuts
// of every action, following the same rules as the IDE:
//
//   - a keymap that lists an action owns its complete shortcut list for it
//     (an empty <action/> element removes all shortcuts);
//   - an action that is not listed inherits the shortcuts of the parent keymap;
//   - bundled "Mac OS X*" keymaps swap control and meta on shortcuts inherited
//     from a non-mac parent ($default);
//   - plugin descriptors add default shortcuts to named keymaps
//     (<keyboard-shortcut keymap="$default" .../>), optionally with
//     remove="true" or replace-all="true";
//   - use-shortcut-of makes an action reuse the shortcuts of another one.
package keymap

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Keystroke is a normalized IntelliJ keystroke: lower-case modifiers in a fixed
// order followed by the key, e.g. "shift meta z" or "control #1000161".
type Keystroke string

// Shortcut is a keyboard shortcut; Second is set for two-keystroke chords.
type Shortcut struct {
	First  Keystroke
	Second Keystroke
}

func (s Shortcut) String() string {
	if s.Second == "" {
		return string(s.First)
	}
	return string(s.First) + ", " + string(s.Second)
}

// Shortcuts is the shortcut list of one action.
type Shortcuts struct {
	Keys  []Shortcut
	Mouse []string
}

func (s Shortcuts) clone() Shortcuts {
	return Shortcuts{Keys: append([]Shortcut(nil), s.Keys...), Mouse: append([]string(nil), s.Mouse...)}
}

// Empty reports whether there are no shortcuts at all.
func (s Shortcuts) Empty() bool { return len(s.Keys) == 0 && len(s.Mouse) == 0 }

// Keymap is one keymap file.
type Keymap struct {
	Name    string
	Parent  string
	User    bool   // true for keymaps from the user's configuration directory
	Source  string // file or jar entry it was read from
	Actions map[string]Shortcuts
}

var modifierOrder = []string{"control", "shift", "alt", "altgraph", "meta"}

// NormalizeKeystroke turns an IntelliJ keystroke string into canonical form.
func NormalizeKeystroke(s string) Keystroke {
	var mods = map[string]bool{}
	var key string
	for _, tok := range strings.Fields(s) {
		t := strings.ToLower(tok)
		switch t {
		case "pressed", "released", "typed":
			continue
		case "ctrl":
			t = "control"
		case "cmd", "command":
			t = "meta"
		case "alt_graph":
			t = "altgraph"
		}
		if isModifier(t) {
			mods[t] = true
			continue
		}
		key = t
	}
	var parts []string
	for _, m := range modifierOrder {
		if mods[m] {
			parts = append(parts, m)
		}
	}
	if key != "" {
		parts = append(parts, key)
	}
	return Keystroke(strings.Join(parts, " "))
}

func isModifier(t string) bool {
	for _, m := range modifierOrder {
		if m == t {
			return true
		}
	}
	return false
}

// Modifiers and key of a normalized keystroke.
func (k Keystroke) Split() (mods []string, key string) {
	f := strings.Fields(string(k))
	if len(f) == 0 {
		return nil, ""
	}
	last := f[len(f)-1]
	if isModifier(last) {
		return f, ""
	}
	return f[:len(f)-1], last
}

func swapControlMeta(k Keystroke) Keystroke {
	mods, key := k.Split()
	var out []string
	for _, m := range mods {
		switch m {
		case "control":
			out = append(out, "meta")
		case "meta":
			out = append(out, "control")
		default:
			out = append(out, m)
		}
	}
	return NormalizeKeystroke(strings.Join(append(out, key), " "))
}

func swapMouse(m string) string {
	f := strings.Fields(m)
	for i, t := range f {
		switch strings.ToLower(t) {
		case "control", "ctrl":
			f[i] = "meta"
		case "meta":
			f[i] = "control"
		}
	}
	return strings.Join(f, " ")
}

type xmlKeymap struct {
	XMLName xml.Name    `xml:"keymap"`
	Name    string      `xml:"name,attr"`
	Parent  string      `xml:"parent,attr"`
	Actions []xmlAction `xml:"action"`
}

type xmlAction struct {
	ID    string        `xml:"id,attr"`
	Keys  []xmlKeyboard `xml:"keyboard-shortcut"`
	Mouse []xmlMouse    `xml:"mouse-shortcut"`
}

type xmlKeyboard struct {
	First  string `xml:"first-keystroke,attr"`
	Second string `xml:"second-keystroke,attr"`
}

type xmlMouse struct {
	Keystroke string `xml:"keystroke,attr"`
}

// ParseKeymap parses one keymap XML file.
func ParseKeymap(data []byte, source string) (*Keymap, error) {
	var x xmlKeymap
	if err := xml.Unmarshal(data, &x); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	if x.Name == "" {
		return nil, fmt.Errorf("%s: keymap has no name", source)
	}
	km := &Keymap{Name: x.Name, Parent: x.Parent, Source: source, Actions: map[string]Shortcuts{}}
	for _, a := range x.Actions {
		sc := km.Actions[a.ID]
		for _, k := range a.Keys {
			s := Shortcut{First: NormalizeKeystroke(k.First), Second: NormalizeKeystroke(k.Second)}
			if s.First != "" {
				sc.Keys = appendUnique(sc.Keys, s)
			}
		}
		for _, m := range a.Mouse {
			if m.Keystroke != "" {
				sc.Mouse = append(sc.Mouse, m.Keystroke)
			}
		}
		km.Actions[a.ID] = sc
	}
	return km, nil
}

// PluginShortcut is a default shortcut declared on an action in a plugin descriptor.
type PluginShortcut struct {
	Keymap     string
	Action     string
	Shortcut   Shortcut
	Remove     bool
	ReplaceAll bool
}

// PluginDescriptor holds what one plugin descriptor contributes to keymaps.
type PluginDescriptor struct {
	Shortcuts []PluginShortcut
	Bindings  map[string]string // action id -> action whose shortcuts it reuses
}

// ParseDescriptor reads keyboard shortcuts and use-shortcut-of bindings from a
// plugin descriptor (plugin.xml or a module XML). Actions may be nested in groups.
func ParseDescriptor(data []byte) (PluginDescriptor, error) {
	d := PluginDescriptor{Bindings: map[string]string{}}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	var stack []string // ids of open <action> elements ("" for other elements)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return d, nil
		}
		if err != nil {
			return d, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			id := ""
			if t.Name.Local == "action" {
				id = attr(t, "id")
				if of := attr(t, "use-shortcut-of"); id != "" && of != "" {
					d.Bindings[id] = of
				}
			}
			if t.Name.Local == "keyboard-shortcut" {
				action := ""
				if len(stack) > 0 {
					action = stack[len(stack)-1]
				}
				km := attr(t, "keymap")
				if action != "" && km != "" {
					d.Shortcuts = append(d.Shortcuts, PluginShortcut{
						Keymap:     km,
						Action:     action,
						Shortcut:   Shortcut{First: NormalizeKeystroke(attr(t, "first-keystroke")), Second: NormalizeKeystroke(attr(t, "second-keystroke"))},
						Remove:     attr(t, "remove") == "true",
						ReplaceAll: attr(t, "replace-all") == "true",
					})
				}
			}
			stack = append(stack, id)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
}

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func appendUnique(list []Shortcut, s Shortcut) []Shortcut {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

// Set is every keymap and plugin contribution known for one IDE.
type Set struct {
	Keymaps  map[string]*Keymap
	Plugin   []PluginShortcut
	Bindings map[string]string

	applied  bool
	resolved map[string]map[string]Shortcuts
}

// NewSet creates an empty set.
func NewSet() *Set {
	return &Set{Keymaps: map[string]*Keymap{}, Bindings: map[string]string{}}
}

// AddKeymap adds a keymap. A user keymap replaces a bundled one with the same name.
func (s *Set) AddKeymap(k *Keymap) {
	if old, ok := s.Keymaps[k.Name]; ok && old.User && !k.User {
		return
	}
	s.Keymaps[k.Name] = k
	s.applied = false
}

// AddDescriptor adds plugin shortcuts and bindings.
func (s *Set) AddDescriptor(d PluginDescriptor) {
	s.Plugin = append(s.Plugin, d.Shortcuts...)
	for k, v := range d.Bindings {
		s.Bindings[k] = v
	}
	s.applied = false
}

// Chain returns the keymap and its ancestors, starting with name.
func (s *Set) Chain(name string) ([]*Keymap, error) {
	var out []*Keymap
	seen := map[string]bool{}
	for name != "" {
		if seen[name] {
			return nil, fmt.Errorf("keymap %q inherits from itself", name)
		}
		seen[name] = true
		k, ok := s.Keymaps[name]
		if !ok {
			return out, fmt.Errorf("keymap %q not found", name)
		}
		out = append(out, k)
		name = k.Parent
	}
	return out, nil
}

// apply merges plugin shortcuts into bundled keymaps, parents before children.
func (s *Set) apply() {
	if s.applied {
		return
	}
	s.applied = true
	s.resolved = map[string]map[string]Shortcuts{}
	byKeymap := map[string][]PluginShortcut{}
	seen := map[PluginShortcut]bool{}
	for _, p := range s.Plugin {
		if seen[p] {
			continue // the same descriptor is often packed in more than one jar
		}
		seen[p] = true
		byKeymap[p.Keymap] = append(byKeymap[p.Keymap], p)
	}
	names := make([]string, 0, len(s.Keymaps))
	for n := range s.Keymaps {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return s.depth(names[i]) < s.depth(names[j]) })
	for _, name := range names {
		k := s.Keymaps[name]
		if k.User {
			continue
		}
		for _, p := range byKeymap[name] {
			sc, own := k.Actions[p.Action]
			if !own {
				sc = s.parentShortcuts(k, p.Action).clone()
			}
			switch {
			case p.ReplaceAll:
				sc.Keys = nil
				if !p.Remove {
					sc.Keys = appendUnique(sc.Keys, p.Shortcut)
				}
			case p.Remove:
				sc.Keys = removeShortcut(sc.Keys, p.Shortcut)
			default:
				sc.Keys = appendUnique(sc.Keys, p.Shortcut)
			}
			k.Actions[p.Action] = sc
			s.resolved = map[string]map[string]Shortcuts{}
		}
	}
}

func (s *Set) depth(name string) int {
	c, _ := s.Chain(name)
	return len(c)
}

func removeShortcut(list []Shortcut, x Shortcut) []Shortcut {
	var out []Shortcut
	for _, s := range list {
		if s != x {
			out = append(out, s)
		}
	}
	return out
}

func isMacKeymap(name string) bool { return strings.HasPrefix(name, "Mac OS X") }

// parentShortcuts returns what keymap k inherits for action from its parent.
func (s *Set) parentShortcuts(k *Keymap, action string) Shortcuts {
	if k.Parent == "" {
		return Shortcuts{}
	}
	parent, ok := s.Keymaps[k.Parent]
	if !ok {
		return Shortcuts{}
	}
	sc := s.effective(parent, action, map[string]bool{})
	if !k.User && isMacKeymap(k.Name) && !isMacKeymap(parent.Name) {
		conv := Shortcuts{}
		for _, x := range sc.Keys {
			y := Shortcut{First: swapControlMeta(x.First)}
			if x.Second != "" {
				y.Second = swapControlMeta(x.Second)
			}
			conv.Keys = append(conv.Keys, y)
		}
		for _, m := range sc.Mouse {
			conv.Mouse = append(conv.Mouse, swapMouse(m))
		}
		return conv
	}
	return sc
}

func (s *Set) effective(k *Keymap, action string, visiting map[string]bool) Shortcuts {
	if cache, ok := s.resolved[k.Name]; ok {
		if sc, ok := cache[action]; ok {
			return sc
		}
	}
	key := k.Name + "\x00" + action
	if visiting[key] {
		return Shortcuts{}
	}
	visiting[key] = true
	defer delete(visiting, key)

	var sc Shortcuts
	if own, ok := k.Actions[action]; ok {
		sc = own
	} else if src, ok := s.Bindings[action]; ok && src != action {
		sc = s.effective(k, src, visiting)
		if sc.Empty() {
			sc = s.parentShortcuts(k, action)
		}
	} else {
		sc = s.parentShortcuts(k, action)
	}
	if s.resolved[k.Name] == nil {
		s.resolved[k.Name] = map[string]Shortcuts{}
	}
	s.resolved[k.Name][action] = sc
	return sc
}

// Effective returns the shortcuts of action in the named keymap.
func (s *Set) Effective(keymap, action string) (Shortcuts, error) {
	s.apply()
	k, ok := s.Keymaps[keymap]
	if !ok {
		return Shortcuts{}, fmt.Errorf("keymap %q not found", keymap)
	}
	return s.effective(k, action, map[string]bool{}), nil
}

// AllActions lists every action id mentioned by the keymap chain, plugin
// shortcuts and bindings, sorted.
func (s *Set) AllActions(keymap string) []string {
	s.apply()
	ids := map[string]bool{}
	chain, _ := s.Chain(keymap)
	for _, k := range chain {
		for id := range k.Actions {
			ids[id] = true
		}
	}
	for id := range s.Bindings {
		ids[id] = true
	}
	return sortedKeys(ids)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
