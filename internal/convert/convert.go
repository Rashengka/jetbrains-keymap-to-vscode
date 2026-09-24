// Package convert turns resolved JetBrains shortcuts into VS Code keybindings.
package convert

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/keymap"
	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/layout"
)

//go:embed actions.json
var actionsJSON []byte

//go:embed reviewed.json
var reviewedJSON []byte

// Reviewed lists JetBrains actions that were checked and have no VS Code
// counterpart, with the reason. A later review only has to re-check the reason.
type Reviewed struct {
	VSCode  string `json:"vscode"`
	Date    string `json:"reviewed"`
	Note    string `json:"note"`
	Actions []struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	} `json:"actions"`
}

// DefaultReviewed returns the built-in list of reviewed actions, keyed by action id.
func DefaultReviewed() map[string]string {
	var r Reviewed
	if err := json.Unmarshal(reviewedJSON, &r); err != nil {
		panic(err) // embedded data, covered by tests
	}
	m := map[string]string{}
	for _, a := range r.Actions {
		m[a.Action] = a.Reason
	}
	return m
}

// Mapping maps one IntelliJ action to one VS Code command.
type Mapping struct {
	Action  string          `json:"action"`
	Command string          `json:"command"`
	When    string          `json:"when,omitempty"`
	Args    json.RawMessage `json:"args,omitempty"`
}

// Table maps action ids to VS Code commands. One action may map to several commands.
type Table map[string][]Mapping

// DefaultTable returns the built-in action table.
func DefaultTable() Table {
	t, err := ParseTable(actionsJSON)
	if err != nil {
		panic(err) // the embedded table is covered by tests
	}
	return t
}

// ParseTable parses a table in the actions.json format.
func ParseTable(data []byte) (Table, error) {
	var list []Mapping
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	t := Table{}
	for _, m := range list {
		if m.Action == "" || m.Command == "" {
			return nil, fmt.Errorf("mapping without action or command: %+v", m)
		}
		t[m.Action] = append(t[m.Action], m)
	}
	return t, nil
}

// Platform selects modifier names used in VS Code keybindings.
type Platform string

const (
	Mac     Platform = "darwin"
	Windows Platform = "windows"
	Linux   Platform = "linux"
)

var namedKeys = map[string]string{
	"enter": "enter", "escape": "escape", "tab": "tab", "space": "space",
	"back_space": "backspace", "delete": "delete", "insert": "insert",
	"home": "home", "end": "end", "page_up": "pageup", "page_down": "pagedown",
	"up": "up", "down": "down", "left": "left", "right": "right",
	"minus": "-", "equals": "=", "open_bracket": "[", "close_bracket": "]",
	"back_slash": "\\", "semicolon": ";", "quote": "'", "back_quote": "`",
	"comma": ",", "period": ".", "slash": "/",
	"multiply": "numpad_multiply", "add": "numpad_add", "subtract": "numpad_subtract",
	"divide": "numpad_divide", "decimal": "numpad_decimal", "separator": "numpad_separator",
	"pause": "pausebreak", "caps_lock": "capslock", "num_lock": "numlock",
	"scroll_lock": "scrolllock", "context_menu": "contextmenu",
	"kp_up": "up", "kp_down": "down", "kp_left": "left", "kp_right": "right",
}

// KeyError explains why a keystroke cannot be written for VS Code.
type KeyError struct {
	Keystroke keymap.Keystroke
	Reason    string
}

func (e *KeyError) Error() string { return fmt.Sprintf("%s: %s", e.Keystroke, e.Reason) }

// charKeys are JetBrains key names that stand for a character whose position
// depends on the keyboard layout.
var charKeys = map[string]rune{
	"plus": '+', "left_parenthesis": '(', "right_parenthesis": ')', "quotedbl": '"',
	"colon": ':', "exclamation_mark": '!', "at": '@', "number_sign": '#', "dollar": '$',
	"circumflex": '^', "ampersand": '&', "asterisk": '*', "underscore": '_', "less": '<',
	"greater": '>', "braceleft": '{', "braceright": '}', "euro_sign": '€',
	"inverted_exclamation_mark": '¡',
	"dead_diaeresis":            '¨', "dead_acute": '´', "dead_caron": 'ˇ', "dead_grave": '`',
	"dead_circumflex": '^', "dead_tilde": '~', "dead_cedilla": '¸', "dead_abovering": '°',
}

// Key converts one normalized IntelliJ keystroke to VS Code syntax, e.g. "shift meta z" -> "shift+cmd+z".
// When lay is not nil, characters that depend on the keyboard layout are bound
// to the physical key that types them, e.g. "meta #100011b" (ě) -> "cmd+[Digit2]".
func Key(k keymap.Keystroke, p Platform, lay *layout.Layout) (string, error) {
	mods, key := k.Split()
	if key == "" {
		return "", &KeyError{k, "no key"}
	}
	has := map[string]bool{}
	for _, m := range mods {
		has[m] = true
	}
	vsKey, err := convertKey(key)
	if err != nil && lay != nil {
		if r, ok := keyChar(key); ok {
			if pk, found := lay.Find(r); found {
				vsKey, err = "["+pk.Code+"]", nil
				if pk.Shift {
					has["shift"] = true
				}
			} else {
				err = fmt.Errorf("%q is not on the %s keyboard layout", string(r), lay.Name())
			}
		}
	}
	if err != nil {
		return "", &KeyError{k, err.Error()}
	}
	var out []string
	if has["altgraph"] {
		return "", &KeyError{k, "AltGr is not supported by VS Code keybindings"}
	}
	if has["control"] {
		out = append(out, "ctrl")
	}
	if has["shift"] {
		out = append(out, "shift")
	}
	if has["alt"] {
		out = append(out, "alt")
	}
	if has["meta"] {
		switch p {
		case Mac:
			out = append(out, "cmd")
		case Windows:
			out = append(out, "win")
		default:
			out = append(out, "meta")
		}
	}
	return strings.Join(append(out, vsKey), "+"), nil
}

func convertKey(key string) (string, error) {
	if len(key) == 1 && (key[0] >= 'a' && key[0] <= 'z' || key[0] >= '0' && key[0] <= '9') {
		return key, nil
	}
	if v, ok := namedKeys[key]; ok {
		return v, nil
	}
	if strings.HasPrefix(key, "f") {
		var n int
		if _, err := fmt.Sscanf(key, "f%d", &n); err == nil && fmt.Sprintf("f%d", n) == key {
			if n >= 1 && n <= 19 {
				return key, nil
			}
			return "", fmt.Errorf("VS Code supports F1-F19 only")
		}
	}
	if strings.HasPrefix(key, "numpad") {
		var n int
		if _, err := fmt.Sscanf(key, "numpad%d", &n); err == nil && n >= 0 && n <= 9 {
			return key, nil
		}
	}
	if strings.HasPrefix(key, "#") {
		return "", fmt.Errorf("layout-dependent character %s", describeCodepoint(key))
	}
	return "", fmt.Errorf("layout-dependent or unsupported key %q", key)
}

// keyChar returns the character a layout-dependent JetBrains key stands for.
func keyChar(key string) (rune, bool) {
	if r, ok := charKeys[key]; ok {
		return r, true
	}
	var v int
	if strings.HasPrefix(key, "#") {
		if _, err := fmt.Sscanf(key, "#%x", &v); err == nil && v >= 0x1000000 {
			return rune(v - 0x1000000), true
		}
	}
	return 0, false
}

// describeCodepoint turns "#1000161" into "U+0161 'š'". IntelliJ stores characters
// without their own key code as 0x1000000 + code point.
func describeCodepoint(key string) string {
	var v int
	if _, err := fmt.Sscanf(key, "#%x", &v); err != nil || v < 0x1000000 {
		return key
	}
	cp := rune(v - 0x1000000)
	return fmt.Sprintf("U+%04X '%c'", cp, cp)
}

// Chord converts a shortcut including an optional second keystroke.
func Chord(s keymap.Shortcut, p Platform, lay *layout.Layout) (string, error) {
	first, err := Key(s.First, p, lay)
	if err != nil {
		return "", err
	}
	if s.Second == "" {
		return first, nil
	}
	second, err := Key(s.Second, p, lay)
	if err != nil {
		return "", err
	}
	return first + " " + second, nil
}

// Binding is one VS Code keybinding plus where it came from.
type Binding struct {
	Key      string          `json:"key"`
	Command  string          `json:"command"`
	When     string          `json:"when,omitempty"`
	Args     json.RawMessage `json:"args,omitempty"`
	Action   string          `json:"-"`
	Shortcut keymap.Shortcut `json:"-"`
}

// Skipped is a shortcut that was not converted. Reason is set when the
// report section alone does not explain it.
type Skipped struct {
	Action   string
	Shortcut string
	Reason   string
}

// Result is the outcome of converting a selection.
type Result struct {
	Bindings []Binding
	Unmapped []Skipped // action not in the table; Reason is set when it was reviewed and has no counterpart
	BadKeys  []Skipped // key cannot be expressed in VS Code
	Unsafe   []Skipped // key without Ctrl/Alt/Cmd and no when clause: it would replace the key everywhere
	Mouse    []Skipped // mouse shortcuts, not supported by VS Code
	Removed  []Skipped // default shortcuts the user removed in JetBrains
	Conflict []Conflict
}

// Conflict is one key bound to more than one command in the same context.
type Conflict struct {
	Key      string
	When     string
	Bindings []Binding
}

// Convert builds VS Code keybindings for the selection. lay may be nil when
// the keyboard layout is unknown; layout-dependent keys are then reported.
func Convert(sel keymap.Selection, t Table, p Platform, lay *layout.Layout) Result {
	var r Result
	reviewed := DefaultReviewed()
	for _, e := range sel.Entries {
		for _, m := range e.Shortcuts.Mouse {
			r.Mouse = append(r.Mouse, Skipped{e.Action, m, ""})
		}
		for _, rm := range e.Removed {
			r.Removed = append(r.Removed, Skipped{e.Action, rm.String(), ""})
		}
		maps := t[e.Action]
		for _, s := range e.Shortcuts.Keys {
			if len(maps) == 0 {
				r.Unmapped = append(r.Unmapped, Skipped{e.Action, s.String(), reviewed[e.Action]})
				continue
			}
			key, err := Chord(s, p, lay)
			if err != nil {
				r.BadKeys = append(r.BadKeys, Skipped{e.Action, s.String(), err.(*KeyError).Reason})
				continue
			}
			for _, m := range maps {
				if m.When == "" && plainKey(key) {
					r.Unsafe = append(r.Unsafe, Skipped{e.Action, s.String(), m.Command})
					continue
				}
				r.Bindings = append(r.Bindings, Binding{Key: key, Command: m.Command, When: m.When, Args: m.Args, Action: e.Action, Shortcut: s})
			}
		}
	}
	r.Conflict = FindConflicts(r.Bindings)
	return r
}

// plainKey reports whether every keystroke of a VS Code key lacks Ctrl, Alt,
// Cmd, Win and Meta. Such keys (Escape, Delete, arrows, Tab, F-keys with only
// Shift) are typed everywhere, so binding them without a when clause would
// take them away from every other widget.
func plainKey(key string) bool {
	for _, stroke := range strings.Fields(key) {
		for _, part := range strings.Split(stroke, "+") {
			switch part {
			case "ctrl", "alt", "cmd", "win", "meta":
				return false
			}
		}
	}
	return true
}

// FindConflicts lists keys bound to different commands under the same when clause.
func FindConflicts(bs []Binding) []Conflict {
	groups := map[string][]Binding{}
	var order []string
	for _, b := range bs {
		k := b.Key + "\x00" + b.When
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], b)
	}
	var out []Conflict
	for _, k := range order {
		g := groups[k]
		cmds := map[string]bool{}
		for _, b := range g {
			cmds[b.Command+string(b.Args)] = true
		}
		if len(cmds) > 1 {
			out = append(out, Conflict{Key: g[0].Key, When: g[0].When, Bindings: g})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// usKeys are keys VS Code accepts by name; everything else is a character
// that depends on the keyboard layout.
var usKeys = map[string]bool{
	"-": true, "=": true, "[": true, "]": true, "\\": true, ";": true, "'": true,
	"`": true, ",": true, ".": true, "/": true,
}

// NormalizeUserKey turns a key typed by the user (for --keep-key) into the form
// the converter generates, so both can be compared. It accepts VS Code syntax
// ("shift+cmd+g", "cmd+[Digit2]"), common modifier names ("command", "option")
// and layout characters ("cmd+ě"), which are resolved to the physical key
// through lay, the same way shortcuts from JetBrains are.
func NormalizeUserKey(s string, p Platform, lay *layout.Layout) (string, error) {
	var strokes []string
	for _, stroke := range strings.Fields(s) {
		has := map[string]bool{}
		key := ""
		parts := strings.Split(stroke, "+")
		// "cmd++" means Cmd and the + key.
		if strings.HasSuffix(stroke, "++") {
			parts = append(strings.Split(strings.TrimSuffix(stroke, "++"), "+"), "+")
		}
		for i, part := range parts {
			low := strings.ToLower(part)
			switch low {
			case "ctrl", "control":
				has["ctrl"] = true
				continue
			case "shift":
				has["shift"] = true
				continue
			case "alt", "option", "opt":
				has["alt"] = true
				continue
			case "cmd", "command", "meta", "win", "super":
				has["cmd"] = true
				continue
			}
			if i != len(parts)-1 || key != "" {
				return "", fmt.Errorf("%q: unknown modifier %q", s, part)
			}
			key = part
		}
		if key == "" {
			return "", fmt.Errorf("%q: no key", s)
		}
		switch r := []rune(key); {
		case strings.HasPrefix(key, "[") && strings.HasSuffix(key, "]"):
			// scan code, keep as written
		case len(r) == 1 && (r[0] >= 'a' && r[0] <= 'z' || r[0] >= 'A' && r[0] <= 'Z' || r[0] >= '0' && r[0] <= '9'):
			key = strings.ToLower(key)
		case len(r) == 1 && usKeys[key]:
		case len(r) == 1:
			if lay == nil {
				return "", fmt.Errorf("%q: %q depends on the keyboard layout; pass --layout or write the physical key, e.g. [Digit2]", s, key)
			}
			pk, ok := lay.Find(r[0])
			if !ok {
				return "", fmt.Errorf("%q: %q is not on the %s keyboard layout", s, key, lay.Name())
			}
			key = "[" + pk.Code + "]"
			if pk.Shift {
				has["shift"] = true
			}
		default:
			key = strings.ToLower(key)
			if v, ok := namedKeys[key]; ok {
				key = v
			}
		}
		var out []string
		for _, m := range []string{"ctrl", "shift", "alt", "cmd"} {
			if !has[m] {
				continue
			}
			if m == "cmd" {
				switch p {
				case Mac:
					m = "cmd"
				case Windows:
					m = "win"
				default:
					m = "meta"
				}
			}
			out = append(out, m)
		}
		strokes = append(strokes, strings.Join(append(out, key), "+"))
	}
	if len(strokes) == 0 {
		return "", fmt.Errorf("empty key")
	}
	return strings.Join(strokes, " "), nil
}
