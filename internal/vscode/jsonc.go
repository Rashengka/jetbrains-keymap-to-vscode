// Package vscode edits VS Code's keybindings.json without destroying what the
// user wrote there. The file is JSONC (comments and trailing commas are allowed),
// so it is never re-serialized: the tool only replaces its own marked block.
package vscode

import (
	"encoding/json"
	"errors"
	"fmt"
)

// maskJSONC returns a copy of src in which every comment is replaced by spaces
// (newlines are kept), so byte offsets stay the same and structural characters
// outside strings and comments can be found with plain scanning.
func maskJSONC(src []byte) ([]byte, error) {
	out := make([]byte, len(src))
	copy(out, src)
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '"':
			i++
			for i < len(src) && src[i] != '"' {
				if src[i] == '\\' {
					i++
				}
				if i < len(src) && src[i] == '\n' {
					return nil, errors.New("newline inside a string")
				}
				i++
			}
			if i >= len(src) {
				return nil, errors.New("unterminated string")
			}
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				out[i] = ' '
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			start := i
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			if i+1 >= len(src) {
				return nil, errors.New("unterminated block comment")
			}
			i += 2
			for j := start; j < i; j++ {
				if out[j] != '\n' && out[j] != '\r' {
					out[j] = ' '
				}
			}
		default:
			i++
		}
	}
	return out, nil
}

// stripTrailingCommas removes commas that are followed only by whitespace and
// a closing bracket. The input must already be masked.
func stripTrailingCommas(masked []byte) []byte {
	out := make([]byte, 0, len(masked))
	inString := false
	for i := 0; i < len(masked); i++ {
		c := masked[i]
		if inString {
			out = append(out, c)
			if c == '\\' && i+1 < len(masked) {
				i++
				out = append(out, masked[i])
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
		}
		if c == ',' {
			j := i + 1
			for j < len(masked) && isSpace(masked[j]) {
				j++
			}
			if j < len(masked) && (masked[j] == ']' || masked[j] == '}') {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// RawBinding is a keybinding as found in keybindings.json.
type RawBinding struct {
	Key     string          `json:"key"`
	Command string          `json:"command"`
	When    string          `json:"when"`
	Args    json.RawMessage `json:"args"`
}

// ParseKeybindings parses a JSONC keybindings file. An empty file is an empty list.
func ParseKeybindings(src []byte) ([]RawBinding, error) {
	masked, err := maskJSONC(src)
	if err != nil {
		return nil, err
	}
	if first, _ := firstSignificant(masked); first < 0 {
		return nil, nil
	}
	var list []RawBinding
	if err := json.Unmarshal(stripTrailingCommas(masked), &list); err != nil {
		return nil, fmt.Errorf("keybindings.json is not a JSON array of keybindings: %w", err)
	}
	return list, nil
}

func firstSignificant(masked []byte) (int, byte) {
	for i, c := range masked {
		if !isSpace(c) {
			return i, c
		}
	}
	return -1, 0
}

func lastSignificant(masked []byte) (int, byte) {
	for i := len(masked) - 1; i >= 0; i-- {
		if !isSpace(masked[i]) {
			return i, masked[i]
		}
	}
	return -1, 0
}
