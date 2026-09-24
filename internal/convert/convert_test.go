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

func TestPlainKeyWithoutWhenIsNotWritten(t *testing.T) {
	sel := keymap.Selection{Entries: []keymap.Entry{
		{Action: "A", Shortcuts: keymap.Shortcuts{Keys: []keymap.Shortcut{{First: "escape"}, {First: "shift f6"}, {First: "meta escape"}}}},
		{Action: "B", Shortcuts: keymap.Shortcuts{Keys: []keymap.Shortcut{{First: "delete"}}}},
	}}
	tab := Table{
		"A": {{Action: "A", Command: "a.global"}},
		"B": {{Action: "B", Command: "b.inView", When: "focusedView == 'x'"}},
	}
	r := Convert(sel, tab, Mac, nil)
	if len(r.Unsafe) != 2 {
		t.Errorf("escape and shift+f6 without when must be refused: %+v", r.Unsafe)
	}
	if len(r.Bindings) != 2 || r.Bindings[0].Key != "cmd+escape" || r.Bindings[1].Key != "delete" {
		t.Errorf("cmd+escape and delete with a when clause are fine: %+v", r.Bindings)
	}
}

func TestReviewedListIsConsistent(t *testing.T) {
	var r Reviewed
	if err := json.Unmarshal(reviewedJSON, &r); err != nil {
		t.Fatal(err)
	}
	if r.VSCode == "" || r.Date == "" || len(r.Actions) == 0 {
		t.Fatalf("reviewed.json needs version, date and actions: %+v", r)
	}
	tab := DefaultTable()
	seen := map[string]bool{}
	for _, a := range r.Actions {
		if a.Reason == "" {
			t.Errorf("%s: no reason", a.Action)
		}
		if seen[a.Action] {
			t.Errorf("%s: listed twice", a.Action)
		}
		seen[a.Action] = true
		if _, ok := tab[a.Action]; ok {
			t.Errorf("%s is both mapped and reviewed as having no counterpart", a.Action)
		}
	}
	type key struct{ action, command, when string }
	dup := map[key]bool{}
	for _, maps := range tab {
		for _, m := range maps {
			k := key{m.Action, m.Command, m.When}
			if dup[k] {
				t.Errorf("duplicate mapping %+v", k)
			}
			dup[k] = true
		}
	}
}

func TestReviewedReasonReachesReport(t *testing.T) {
	sel := keymap.Selection{Entries: []keymap.Entry{
		{Action: "Console.Jdbc.Execute", Shortcuts: keymap.Shortcuts{Keys: []keymap.Shortcut{{First: "meta enter"}}}},
		{Action: "NeverSeenAction", Shortcuts: keymap.Shortcuts{Keys: []keymap.Shortcut{{First: "meta j"}}}},
	}}
	r := Convert(sel, Table{}, Mac, nil)
	if len(r.Unmapped) != 2 || r.Unmapped[0].Reason == "" || r.Unmapped[1].Reason != "" {
		t.Errorf("reviewed action must carry its reason, unknown one must not: %+v", r.Unmapped)
	}
}

func TestNormalizeUserKey(t *testing.T) {
	cz, _ := layout.Get("cz-qwerty")
	cases := map[string]string{
		"cmd+g":           "cmd+g",
		"Command+Shift+G": "shift+cmd+g",
		"shift+cmd+g":     "shift+cmd+g",
		"cmd+ě":           "cmd+[Digit2]",
		"cmd+[Digit2]":    "cmd+[Digit2]",
		"cmd++":           "cmd+[Digit1]", // + is the Digit1 key on the Czech layout
		"cmd+!":           "shift+cmd+[Quote]",
		"ctrl+-":          "ctrl+-",
		"cmd+k cmd+ř":     "cmd+k cmd+[Digit5]",
		"option+enter":    "alt+enter",
	}
	for in, want := range cases {
		got, err := NormalizeUserKey(in, Mac, cz)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	// The same physical key must match what the converter generates for JetBrains' "meta #100011b".
	gen, _ := Key(keymap.NormalizeKeystroke("meta #100011b"), Mac, cz)
	if user, _ := NormalizeUserKey("cmd+ě", Mac, cz); user != gen {
		t.Errorf("user %q != generated %q", user, gen)
	}
	if _, err := NormalizeUserKey("cmd+ě", Mac, nil); err == nil {
		t.Error("a layout character without a layout must be an error, not a silent mismatch")
	}
	if _, err := NormalizeUserKey("hyper+x", Mac, cz); err == nil {
		t.Error("unknown modifier accepted")
	}
}

func TestComparableKeyAndRecommended(t *testing.T) {
	if got := ComparableKey("shift+cmd+[Digit2] cmd+[KeyK]"); got != "shift+cmd+2 cmd+k" {
		t.Errorf("got %q", got)
	}
	if got := ComparableKey("cmd+[Quote]"); got != "cmd+[Quote]" {
		t.Errorf("other scan codes stay: %q", got)
	}
	mac := DefaultRecommended(Mac)
	if r := mac["cmd+f"]; r.Command != "actions.find" {
		t.Errorf("cmd+f: %+v", r)
	}
	if r := mac[ComparableKey("cmd+[Digit2]")]; r.Command != "workbench.action.focusSecondEditorGroup" {
		t.Errorf("cmd+[Digit2] must hit the editor-group default: %+v", r)
	}
	if r := DefaultRecommended(Windows)["ctrl+f"]; r.Command != "actions.find" {
		t.Errorf("windows ctrl+f: %+v", r)
	}
	if len(mac) < 100 {
		t.Errorf("too few defaults: %d", len(mac))
	}
}
