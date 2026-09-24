package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/keymap"
	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/layout"
)

func TestKey(t *testing.T) {
	cases := []struct {
		in   string
		p    Platform
		want string
		err  string
	}{
		{"shift meta z", Mac, "shift+cmd+z", ""},
		{"control alt l", Mac, "ctrl+alt+l", ""},
		{"meta back_space", Mac, "cmd+backspace", ""},
		{"meta open_bracket", Mac, "cmd+[", ""},
		{"control shift f12", Windows, "ctrl+shift+f12", ""},
		{"meta e", Windows, "win+e", ""},
		{"meta e", Linux, "meta+e", ""},
		{"alt page_down", Linux, "alt+pagedown", ""},
		{"control numpad5", Linux, "ctrl+numpad5", ""},
		{"control divide", Linux, "ctrl+numpad_divide", ""},
		{"meta #1000161", Mac, "", "U+0161 'š'"},
		{"meta plus", Mac, "", "layout-dependent"},
		{"altgraph q", Linux, "", "AltGr"},
		{"f24", Mac, "", "F1-F19"},
	}
	for _, c := range cases {
		got, err := Key(keymap.NormalizeKeystroke(c.in), c.p, nil)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: want error containing %q, got %q, %v", c.in, c.err, got, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

func TestKeyWithLayout(t *testing.T) {
	cz, err := layout.Get("cz-qwerty")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"meta #100011b":             "cmd+[Digit2]",       // ě
		"shift meta #1000161":       "shift+cmd+[Digit3]", // š
		"meta plus":                 "cmd+[Digit1]",       // + is unshifted on the Czech layout
		"meta #10000a7":             "cmd+[Quote]",        // §
		"shift meta dead_diaeresis": "shift+cmd+[Backslash]",
		"meta right_parenthesis":    "cmd+[BracketRight]",
		"meta exclamation_mark":     "shift+cmd+[Quote]", // ! needs Shift
		"meta z":                    "cmd+z",             // letters stay labels, VS Code maps them itself
		"control minus":             "ctrl+-",            // keys VS Code knows stay as they were
	}
	for in, want := range cases {
		got, err := Key(keymap.NormalizeKeystroke(in), Mac, cz)
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := Key(keymap.NormalizeKeystroke("meta #1000444"), Mac, cz); err == nil || !strings.Contains(err.Error(), "Czech-QWERTY") {
		t.Errorf("character missing on the layout must be reported, got %v", err)
	}
}

func TestChord(t *testing.T) {
	got, err := Chord(keymap.Shortcut{First: "meta k", Second: "meta c"}, Mac, nil)
	if err != nil || got != "cmd+k cmd+c" {
		t.Errorf("got %q %v", got, err)
	}
}

func TestDefaultTableIsValid(t *testing.T) {
	tab := DefaultTable()
	if len(tab) < 100 {
		t.Errorf("table unexpectedly small: %d actions", len(tab))
	}
	for action, maps := range tab {
		for _, m := range maps {
			if strings.TrimSpace(m.Command) != m.Command || strings.Contains(m.Command, " ") {
				t.Errorf("%s: suspicious command %q", action, m.Command)
			}
			if len(m.Args) > 0 && !json.Valid(m.Args) {
				t.Errorf("%s: invalid args", action)
			}
		}
	}
}

func TestConvertSortsIntoReportBuckets(t *testing.T) {
	sel := keymap.Selection{Entries: []keymap.Entry{
		{Action: "EditorDuplicate", Shortcuts: keymap.Shortcuts{Keys: []keymap.Shortcut{{First: "meta d"}, {First: "shift meta #1000161"}}}},
		{Action: "SomethingUnknown", Shortcuts: keymap.Shortcuts{Keys: []keymap.Shortcut{{First: "meta u"}}}},
		{Action: "GotoDeclaration", Shortcuts: keymap.Shortcuts{Keys: []keymap.Shortcut{{First: "meta b"}}, Mouse: []string{"meta button1"}}},
		{Action: "ReformatCode", Removed: []keymap.Shortcut{{First: "alt meta l"}}},
		{Action: "GotoClass", Shortcuts: keymap.Shortcuts{Keys: []keymap.Shortcut{{First: "meta d"}}}},
	}}
	tab := Table{
		"EditorDuplicate": {{Action: "EditorDuplicate", Command: "editor.action.copyLinesDownAction", When: "editorTextFocus"}},
		"GotoDeclaration": {{Action: "GotoDeclaration", Command: "editor.action.revealDefinition"}},
		"GotoClass":       {{Action: "GotoClass", Command: "workbench.action.showAllSymbols"}},
	}
	r := Convert(sel, tab, Mac, nil)
	if len(r.Bindings) != 3 {
		t.Errorf("bindings: %+v", r.Bindings)
	}
	if len(r.BadKeys) != 1 || len(r.Unmapped) != 1 || len(r.Mouse) != 1 || len(r.Removed) != 1 {
		t.Errorf("report: bad %v unmapped %v mouse %v removed %v", r.BadKeys, r.Unmapped, r.Mouse, r.Removed)
	}
	// cmd+d with different when clauses is not a conflict.
	if len(r.Conflict) != 0 {
		t.Errorf("unexpected conflicts: %+v", r.Conflict)
	}
	tab["GotoClass"][0].When = "editorTextFocus"
	r = Convert(sel, tab, Mac, nil)
	if len(r.Conflict) != 1 || r.Conflict[0].Key != "cmd+d" {
		t.Errorf("conflict not found: %+v", r.Conflict)
	}
}
