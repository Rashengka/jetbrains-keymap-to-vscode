// Package convert turns resolved JetBrains shortcuts into VS Code keybindings.
package convert

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/keymap"
)

//go:embed actions.json
var actionsJSON []byte

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

// Key converts one normalized IntelliJ keystroke to VS Code syntax, e.g. "shift meta z" -> "shift+cmd+z".
func Key(k keymap.Keystroke, p Platform) (string, error) {
	mods, key := k.Split()
	if key == "" {
		return "", &KeyError{k, "no key"}
	}
	vsKey, err := convertKey(key)
	if err != nil {
		return "", &KeyError{k, err.Error()}
	}
	var out []string
	has := map[string]bool{}
	for _, m := range mods {
		has[m] = true
	}
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
func Chord(s keymap.Shortcut, p Platform) (string, error) {
	first, err := Key(s.First, p)
	if err != nil {
		return "", err
	}
	if s.Second == "" {
		return first, nil
	}
	second, err := Key(s.Second, p)
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
	Unmapped []Skipped // action has no VS Code counterpart in the table
	BadKeys  []Skipped // key cannot be expressed in VS Code
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

// Convert builds VS Code keybindings for the selection.
func Convert(sel keymap.Selection, t Table, p Platform) Result {
	var r Result
	for _, e := range sel.Entries {
		for _, m := range e.Shortcuts.Mouse {
			r.Mouse = append(r.Mouse, Skipped{e.Action, m, ""})
		}
		for _, rm := range e.Removed {
			r.Removed = append(r.Removed, Skipped{e.Action, rm.String(), ""})
		}
		maps := t[e.Action]
		for _, s := range e.Shortcuts.Keys {
			key, err := Chord(s, p)
			if err != nil {
				r.BadKeys = append(r.BadKeys, Skipped{e.Action, s.String(), err.(*KeyError).Reason})
				continue
			}
			if len(maps) == 0 {
				r.Unmapped = append(r.Unmapped, Skipped{e.Action, s.String(), ""})
				continue
			}
			for _, m := range maps {
				r.Bindings = append(r.Bindings, Binding{Key: key, Command: m.Command, When: m.When, Args: m.Args, Action: e.Action, Shortcut: s})
			}
		}
	}
	r.Conflict = findConflicts(r.Bindings)
	return r
}

func findConflicts(bs []Binding) []Conflict {
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
