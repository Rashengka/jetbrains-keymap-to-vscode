// Command gen-vscode-defaults builds internal/convert/vscode-defaults.json from
// the default keyboard shortcuts documented by VS Code:
//
//	go run ./tools/gen-vscode-defaults > internal/convert/vscode-defaults.json
//
// The page lists the shortcuts VS Code itself considers important. They are
// what --keep-recommended leaves to VS Code.
package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const source = "https://code.visualstudio.com/docs/reference/default-keybindings"

type entry struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Mac     string `json:"mac,omitempty"`
	Windows string `json:"windows,omitempty"`
	Linux   string `json:"linux,omitempty"`
}

type output struct {
	Source  string  `json:"source"`
	Fetched string  `json:"fetched"`
	Entries []entry `json:"entries"`
}

func main() {
	resp, err := http.Get(source)
	if err != nil {
		fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fail(fmt.Errorf("%s: %s", source, resp.Status))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fail(err)
	}
	entries, err := parse(string(body))
	if err != nil {
		fail(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(output{Source: source, Fetched: time.Now().Format("2006-01-02"), Entries: entries}); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen-vscode-defaults:", err)
	os.Exit(1)
}

var (
	rowRe  = regexp.MustCompile(`(?s)<tr>(.*?)</tr>`)
	cellRe = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	tagRe  = regexp.MustCompile(`<[^>]+>`)
)

func parse(page string) ([]entry, error) {
	var out []entry
	for _, row := range rowRe.FindAllStringSubmatch(page, -1) {
		var cells []string
		for _, c := range cellRe.FindAllStringSubmatch(row[1], -1) {
			cells = append(cells, strings.TrimSpace(html.UnescapeString(tagRe.ReplaceAllString(c[1], ""))))
		}
		if len(cells) < 3 || cells[1] == "" || cells[2] == "" {
			continue
		}
		mac, win, linux, err := splitPlatforms(cells[1])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", cells[2], err)
		}
		out = append(out, entry{Name: cells[0], Command: cells[2], Mac: mac, Windows: win, Linux: linux})
	}
	if len(out) < 100 {
		return nil, fmt.Errorf("only %d shortcuts found; the page layout probably changed", len(out))
	}
	return out, nil
}

// splitPlatforms parses "⇧⌥↑ (Windows Shift+Alt+Up, Linux Ctrl+Shift+Alt+Up)"
// or "⌘X (Windows, Linux Ctrl+X)" or a plain "F2" valid everywhere.
func splitPlatforms(s string) (mac, win, linux string, err error) {
	macPart, rest, hasRest := strings.Cut(s, " (")
	if mac, err = macKey(strings.TrimSpace(macPart)); err != nil {
		return
	}
	if !hasRest {
		k := otherKey(macPart)
		return mac, k, k, nil
	}
	rest = strings.TrimSuffix(strings.TrimSpace(rest), ")")
	if strings.HasPrefix(rest, "Windows, Linux ") {
		k := otherKey(strings.TrimPrefix(rest, "Windows, Linux "))
		return mac, k, k, nil
	}
	for _, part := range strings.Split(rest, ", ") {
		switch {
		case strings.HasPrefix(part, "Windows "):
			win = otherKey(strings.TrimPrefix(part, "Windows "))
		case strings.HasPrefix(part, "Linux "):
			linux = otherKey(strings.TrimPrefix(part, "Linux "))
		}
	}
	return mac, win, linux, nil
}

var macSymbols = []struct{ sym, name string }{{"⌃", "ctrl"}, {"⇧", "shift"}, {"⌥", "alt"}, {"⌘", "cmd"}}

var keyNames = map[string]string{
	"←": "left", "→": "right", "↑": "up", "↓": "down",
	"Space": "space", "Enter": "enter", "Escape": "escape", "Tab": "tab",
	"Delete": "delete", "Backspace": "backspace", "Home": "home", "End": "end",
	"PageUp": "pageup", "PageDown": "pagedown", "Insert": "insert",
}

// macKey turns "⇧⌘K ⌘C" into "shift+cmd+k cmd+c".
func macKey(s string) (string, error) {
	var strokes []string
	for _, stroke := range strings.Fields(s) {
		has := map[string]bool{}
		for {
			found := false
			for _, m := range macSymbols {
				if strings.HasPrefix(stroke, m.sym) {
					has[m.name] = true
					stroke = strings.TrimPrefix(stroke, m.sym)
					found = true
				}
			}
			if !found {
				break
			}
		}
		if stroke == "" {
			return "", fmt.Errorf("no key in %q", s)
		}
		strokes = append(strokes, join(has, keyName(stroke)))
	}
	return strings.Join(strokes, " "), nil
}

// otherKey turns "Ctrl+Shift+Alt+Up Ctrl+K" into "ctrl+shift+alt+up ctrl+k".
func otherKey(s string) string {
	var strokes []string
	for _, stroke := range strings.Fields(strings.TrimSpace(s)) {
		has := map[string]bool{}
		parts := strings.Split(stroke, "+")
		key := parts[len(parts)-1]
		if strings.HasSuffix(stroke, "++") {
			key = "+"
			parts = parts[:len(parts)-1]
		}
		for _, p := range parts[:len(parts)-1] {
			switch strings.ToLower(p) {
			case "ctrl":
				has["ctrl"] = true
			case "shift":
				has["shift"] = true
			case "alt":
				has["alt"] = true
			case "win", "meta", "super":
				has["cmd"] = true
			}
		}
		strokes = append(strokes, join(has, keyName(key)))
	}
	return strings.Join(strokes, " ")
}

func keyName(k string) string {
	if n, ok := keyNames[k]; ok {
		return n
	}
	return strings.ToLower(k)
}

func join(has map[string]bool, key string) string {
	var out []string
	for _, m := range []string{"ctrl", "shift", "alt", "cmd"} {
		if has[m] {
			out = append(out, m)
		}
	}
	return strings.Join(append(out, key), "+")
}
