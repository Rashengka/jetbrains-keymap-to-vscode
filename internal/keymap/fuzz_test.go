package keymap

import "testing"

// The parsers read files from IDE installations and from --file; they must
// return errors, never panic.
func FuzzParsers(f *testing.F) {
	for _, seed := range []string{defaultXML, macXML, userXML, pluginXML, "<keymap/>", "<idea-plugin><action id=\"a\"><keyboard-shortcut keymap=\"$default\"/></action></idea-plugin>"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if k, err := ParseKeymap(data, "fuzz"); err == nil {
			s := NewSet()
			s.AddKeymap(k)
			s.Select(k.Name, ScopeAll)
		}
		if d, err := ParseDescriptor(data); err == nil {
			s := NewSet()
			s.AddDescriptor(d)
		}
		NormalizeKeystroke(string(data))
	})
}
