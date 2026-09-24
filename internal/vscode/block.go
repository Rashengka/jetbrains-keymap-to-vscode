package vscode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Markers delimit the block managed by this tool. Everything outside is left byte for byte.
const (
	StartMarker = "// >>> jetbrains-keymap-to-vscode"
	EndMarker   = "// <<< jetbrains-keymap-to-vscode"
)

// KeepPrefix starts the line inside the block that lists keys left to VS Code.
// It lives in the block so the tool can read it back on the next run.
const KeepPrefix = "// keep for VS Code: "

// ReadKeep returns the keys listed on the keep line of the managed block.
func ReadKeep(src []byte) []string {
	masked, err := maskJSONC(src)
	if err != nil {
		return nil
	}
	start, end, found, err := blockRange(src, masked)
	if err != nil || !found {
		return nil
	}
	for _, line := range strings.Split(string(src[start:end]), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, KeepPrefix) {
			var out []string
			for _, k := range strings.Split(strings.TrimPrefix(line, KeepPrefix), ",") {
				if k = strings.TrimSpace(k); k != "" {
					out = append(out, k)
				}
			}
			return out
		}
	}
	return nil
}

// Entry is one keybinding written into the block, with an optional comment above it.
type Entry struct {
	Comment string
	Key     string
	Command string
	When    string
	Args    json.RawMessage
}

type entryJSON struct {
	Key     string          `json:"key"`
	Command string          `json:"command"`
	When    string          `json:"when,omitempty"`
	Args    json.RawMessage `json:"args,omitempty"`
}

// blockRange finds the managed block: from the start of the start-marker line
// to the end of the end-marker line (including its newline).
func blockRange(src, masked []byte) (start, end int, found bool, err error) {
	starts := markerLines(src, masked, StartMarker)
	ends := markerLines(src, masked, EndMarker)
	switch {
	case len(starts) == 0 && len(ends) == 0:
		return 0, 0, false, nil
	case len(starts) != 1 || len(ends) != 1:
		return 0, 0, false, fmt.Errorf("expected one %q and one %q line, found %d and %d; fix keybindings.json by hand", StartMarker, EndMarker, len(starts), len(ends))
	case ends[0] < starts[0]:
		return 0, 0, false, errors.New("end marker comes before start marker; fix keybindings.json by hand")
	}
	start = lineStart(src, starts[0])
	end = bytes.IndexByte(src[ends[0]:], '\n')
	if end < 0 {
		end = len(src)
	} else {
		end = ends[0] + end + 1
	}
	return start, end, true, nil
}

// markerLines returns offsets of marker comments that are real comments (not inside strings).
func markerLines(src, masked []byte, marker string) []int {
	var out []int
	for off := 0; ; {
		i := bytes.Index(src[off:], []byte(marker))
		if i < 0 {
			return out
		}
		pos := off + i
		if masked[pos] == ' ' { // masked means it is inside a comment
			out = append(out, pos)
		}
		off = pos + len(marker)
	}
}

func lineStart(src []byte, pos int) int {
	for pos > 0 && src[pos-1] != '\n' {
		pos--
	}
	return pos
}

func renderEntries(entries []Entry, indent, nl string) (string, error) {
	var parts []string
	for _, e := range entries {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(entryJSON{Key: e.Key, Command: e.Command, When: e.When, Args: e.Args}); err != nil {
			return "", err
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, bytes.TrimSpace(buf.Bytes()), indent, "  "); err != nil {
			return "", err
		}
		s := indent + strings.ReplaceAll(pretty.String(), "\n", nl)
		if e.Comment != "" {
			s = indent + "// " + strings.ReplaceAll(e.Comment, "\n", " ") + nl + s
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ","+nl), nil
}

// Apply returns src with the managed block replaced by entries (or inserted
// before the closing bracket when there is no block yet). header is written
// after the start marker. The result is verified to parse and to keep every
// keybinding outside the block unchanged.
func Apply(src []byte, entries []Entry, header string, keep []string) ([]byte, error) {
	nl := "\n"
	if bytes.Contains(src, []byte("\r\n")) {
		nl = "\r\n"
	}
	const indent = "  "
	body, err := renderEntries(entries, indent, nl)
	if err != nil {
		return nil, err
	}
	startLine := indent + StartMarker
	if header != "" {
		startLine += " " + strings.ReplaceAll(header, "\n", " ")
	}
	if len(keep) > 0 {
		startLine += nl + indent + KeepPrefix + strings.Join(keep, ", ")
	}

	masked, err := maskJSONC(src)
	if err != nil {
		return nil, fmt.Errorf("cannot read keybindings.json: %w", err)
	}
	outside, err := withoutBlock(src)
	if err != nil {
		return nil, err
	}

	var out []byte
	if first, _ := firstSignificant(masked); first < 0 {
		// Empty file, or only comments: keep the comments, add a fresh array.
		b := startLine + nl
		if body != "" {
			b += body + nl
		}
		b += indent + EndMarker + nl
		head := bytes.TrimRight(src, " \t\r\n")
		if len(head) > 0 {
			head = append(append([]byte{}, head...), []byte(nl)...)
		}
		out = append(append([]byte{}, head...), []byte("["+nl+b+"]"+nl)...)
	} else {
		if _, c := firstSignificant(masked); c != '[' {
			return nil, errors.New("keybindings.json does not contain a JSON array")
		}
		closeIdx, c := lastSignificant(masked)
		if c != ']' {
			return nil, errors.New("keybindings.json does not end with ']'")
		}
		start, end, found, err := blockRange(src, masked)
		if err != nil {
			return nil, err
		}
		if !found {
			start, end = closeIdx, closeIdx
		}
		// Comma handling: the element before the block needs a trailing comma
		// when entries follow; the block needs one when elements follow it.
		prefix := src[:start]
		suffix := src[end:]
		prefixMasked := masked[:start]
		suffixMasked := masked[end:]
		prevIdx, prev := lastSignificant(prefixMasked)
		_, next := firstSignificant(suffixMasked)

		var head []byte
		if !found {
			head = bytes.TrimRight(prefix, " \t\r\n")
			prevIdx, prev = lastSignificant(masked[:len(head)])
		} else {
			head = prefix
		}
		if body != "" && prev != '[' && prev != ',' {
			head = append(append(append([]byte{}, head[:prevIdx+1]...), ','), head[prevIdx+1:]...)
		}
		b := startLine + nl
		if body != "" {
			b += body
			if next != ']' {
				b += ","
			}
			b += nl
		}
		b += indent + EndMarker + nl
		if !found {
			out = append(append(append([]byte{}, head...), []byte(nl+b)...), suffix...)
		} else {
			out = append(append(append([]byte{}, head...), []byte(b)...), suffix...)
		}
	}

	// Verify: the result must parse, and everything outside the block must be unchanged.
	if _, err := ParseKeybindings(out); err != nil {
		return nil, fmt.Errorf("internal error, generated file does not parse: %w", err)
	}
	after, err := withoutBlock(out)
	if err != nil {
		return nil, err
	}
	if !sameBindings(outside, after) {
		return nil, errors.New("internal error, keybindings outside the managed block would change")
	}
	return out, nil
}

// withoutBlock parses the bindings of src that lie outside the managed block.
func withoutBlock(src []byte) ([]RawBinding, error) {
	masked, err := maskJSONC(src)
	if err != nil {
		return nil, fmt.Errorf("cannot read keybindings.json: %w", err)
	}
	start, end, found, err := blockRange(src, masked)
	if err != nil {
		return nil, err
	}
	rest := src
	if found {
		rest = append(append([]byte{}, src[:start]...), src[end:]...)
	}
	return ParseKeybindings(rest)
}

// Outside returns the keybindings the user maintains outside the managed block.
func Outside(src []byte) ([]RawBinding, error) { return withoutBlock(src) }

func sameBindings(a, b []RawBinding) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Key != b[i].Key || a[i].Command != b[i].Command || a[i].When != b[i].When || !bytes.Equal(compact(a[i].Args), compact(b[i].Args)) {
			return false
		}
	}
	return true
}

func compact(m json.RawMessage) []byte {
	if len(m) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, m); err != nil {
		return m
	}
	return buf.Bytes()
}
