package vscode

import (
	"bytes"
	"testing"
)

// FuzzApply checks the promises the tool makes about keybindings.json for any
// input: it either refuses, or the result parses, every binding outside the
// block is unchanged, and a second run changes nothing.
func FuzzApply(f *testing.F) {
	for _, seed := range []string{
		"", "[]", "// c\n", userFile,
		"[\n  {\"key\": \"a\", \"command\": \"x\"}\n]\n",
		"[ /* a */ {\"key\": \"a\", \"command\": \"x\", \"when\": \"// not a comment\"}, ]",
		"[\r\n  {\"key\": \"a\", \"command\": \"x\"}\r\n]\r\n",
		"[\n  " + StartMarker + "\n  " + EndMarker + "\n]\n",
		`[{"key":"a","command":"x","args":{"text":"]"}}]`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		before, err := Outside(src)
		if err != nil {
			return // unreadable input must be refused, which Apply also does
		}
		out, err := Apply(src, sample, "h", []string{"cmd+g"})
		if err != nil {
			return
		}
		if _, err := ParseKeybindings(out); err != nil {
			t.Fatalf("result does not parse: %v\n%s", err, out)
		}
		after, err := Outside(out)
		if err != nil || !sameBindings(before, after) {
			t.Fatalf("bindings outside the block changed: %v\n%s", err, out)
		}
		again, err := Apply(out, sample, "h", []string{"cmd+g"})
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("second run changed the file: %v\n%s\n---\n%s", err, out, again)
		}
		if got := ReadKeep(out); len(got) != 1 || got[0] != "cmd+g" {
			t.Fatalf("keep line lost: %v", got)
		}
	})
}

func FuzzParseKeybindings(f *testing.F) {
	for _, seed := range []string{"", "[]", userFile, "[/*", `["\`, "[{\"key\":\"\\\"\"}]"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		ParseKeybindings(src) // must not panic
	})
}
