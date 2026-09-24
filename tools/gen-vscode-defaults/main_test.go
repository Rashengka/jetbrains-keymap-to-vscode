package main

import "testing"

func TestSplitPlatforms(t *testing.T) {
	cases := []struct{ in, mac, win, linux string }{
		{"⌘X (Windows, Linux Ctrl+X)", "cmd+x", "ctrl+x", "ctrl+x"},
		{"⇧⌥↑ (Windows Shift+Alt+Up, Linux Ctrl+Shift+Alt+Up)", "shift+alt+up", "shift+alt+up", "ctrl+shift+alt+up"},
		{"⌘K ⌘] (Windows, Linux Ctrl+K Ctrl+])", "cmd+k cmd+]", "ctrl+k ctrl+]", "ctrl+k ctrl+]"},
		{"F2", "f2", "f2", "f2"},
		{"⌃⇧- (Windows Alt+Right, Linux Ctrl+Shift+-)", "ctrl+shift+-", "alt+right", "ctrl+shift+-"},
		{"⌘Numpad0 (Windows, Linux Ctrl+Numpad0)", "cmd+numpad0", "ctrl+numpad0", "ctrl+numpad0"},
		{"⇧⌘Space (Windows, Linux Ctrl+Shift+Space)", "shift+cmd+space", "ctrl+shift+space", "ctrl+shift+space"},
	}
	for _, c := range cases {
		mac, win, linux, err := splitPlatforms(c.in)
		if err != nil || mac != c.mac || win != c.win || linux != c.linux {
			t.Errorf("%q: got %q %q %q %v", c.in, mac, win, linux, err)
		}
	}
}
