package release

import "testing"

func TestVersionFileIsValid(t *testing.T) {
	if _, err := Parse(Version()); err != nil {
		t.Fatal(err)
	}
}

func TestBump(t *testing.T) {
	cases := []struct{ cur, kind, want string }{
		{"0.1.0", "patch", "0.1.1"},
		{"0.1.9", "patch", "0.1.10"},
		{"0.1.3", "minor", "0.2.0"},
		{"0.9.3", "major", "1.0.0"},
		{"0.1.0", "0.3.0", "0.3.0"},
	}
	for _, c := range cases {
		if got, err := Bump(c.cur, c.kind); err != nil || got != c.want {
			t.Errorf("%s %s: got %q %v", c.cur, c.kind, got, err)
		}
	}
	for _, bad := range [][2]string{{"0.1.0", "0.1.0"}, {"0.2.0", "0.1.9"}, {"0.1.0", "huge"}, {"x", "patch"}, {"0.1.0", "01.2.3"}} {
		if _, err := Bump(bad[0], bad[1]); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestMessage(t *testing.T) {
	if m, _ := Message("0.2.0", " --file reads keybindings.json "); m != "v0.2.0: --file reads keybindings.json" {
		t.Errorf("got %q", m)
	}
	if _, err := Message("0.2.0", "  "); err == nil {
		t.Error("empty description accepted")
	}
}

func TestCheck(t *testing.T) {
	ok := func(tag, ver, typ, ann string) {
		t.Helper()
		if err := Check(tag, ver, typ, ann); err != nil {
			t.Errorf("%s: unexpected error %v", tag, err)
		}
	}
	bad := func(tag, ver, typ, ann string) {
		t.Helper()
		if err := Check(tag, ver, typ, ann); err == nil {
			t.Errorf("%s %q %s %q: accepted", tag, ver, typ, ann)
		}
	}
	ok("v0.2.0", "0.2.0\n", "tag", "v0.2.0: keep keys for VS Code")
	bad("v0.2.1", "0.2.0", "tag", "v0.2.1: x")    // tag does not match VERSION
	bad("v0.2.0", "0.2.0", "commit", "v0.2.0: x") // lightweight tag
	bad("v0.2.0", "0.2.0", "tag", "")             // lost annotation
	bad("v0.2.0", "0.2.0", "tag", "v0.2.0")       // only the number
	bad("v0.2.0", "0.2.0", "tag", "v0.2.0:   ")   // only the number and a colon
	bad("v0.2.0", "broken", "tag", "v0.2.0: x")   // unreadable VERSION
}
