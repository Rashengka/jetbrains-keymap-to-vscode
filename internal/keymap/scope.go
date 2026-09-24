package keymap

import (
	"fmt"
	"sort"
)

// Scope selects which shortcuts are transferred.
type Scope string

const (
	// ScopeCustom transfers only the actions changed in the user's own keymaps.
	ScopeCustom Scope = "custom"
	// ScopeAll transfers the complete effective keymap: defaults plus changes.
	ScopeAll Scope = "all"
	// ScopeDefault transfers the bundled keymap the user's keymap is based on, without changes.
	ScopeDefault Scope = "default"
)

// ParseScope validates a scope name.
func ParseScope(s string) (Scope, error) {
	switch Scope(s) {
	case ScopeCustom, ScopeAll, ScopeDefault:
		return Scope(s), nil
	}
	return "", fmt.Errorf("unknown scope %q (use custom, all or default)", s)
}

// Entry is the resolved shortcut list of one action.
type Entry struct {
	Action    string
	Shortcuts Shortcuts
	// Removed lists default shortcuts the user removed from this action (custom scope only).
	Removed []Shortcut
}

// Selection is the result of applying a scope to a keymap.
type Selection struct {
	Keymap  string // keymap the shortcuts come from
	Base    string // bundled keymap the user's keymap is based on
	Entries []Entry
}

// Select resolves the shortcuts of the active keymap for the given scope.
func (s *Set) Select(active string, scope Scope) (Selection, error) {
	s.apply()
	chain, err := s.Chain(active)
	if err != nil {
		// Without the IDE installation the bundled parent is unknown. The
		// user's own changes can still be transferred, nothing else.
		if scope != ScopeCustom || len(chain) == 0 {
			return Selection{}, err
		}
	}
	base := ""
	for _, k := range chain {
		if !k.User {
			base = k.Name
			break
		}
	}
	sel := Selection{Keymap: active, Base: base}

	switch scope {
	case ScopeCustom:
		changed := map[string]bool{}
		for _, k := range chain {
			if !k.User {
				break
			}
			for id := range k.Actions {
				changed[id] = true
			}
		}
		for _, id := range sortedKeys(changed) {
			sc, _ := s.Effective(active, id)
			e := Entry{Action: id, Shortcuts: sc}
			if base != "" {
				def, _ := s.Effective(base, id)
				for _, d := range def.Keys {
					if !containsShortcut(sc.Keys, d) {
						e.Removed = append(e.Removed, d)
					}
				}
			}
			sel.Entries = append(sel.Entries, e)
		}
	case ScopeAll, ScopeDefault:
		from := active
		if scope == ScopeDefault {
			if base == "" {
				return Selection{}, fmt.Errorf("keymap %q has no bundled ancestor", active)
			}
			from = base
			sel.Keymap = base
		}
		for _, id := range s.AllActions(from) {
			sc, _ := s.Effective(from, id)
			if !sc.Empty() {
				sel.Entries = append(sel.Entries, Entry{Action: id, Shortcuts: sc})
			}
		}
	default:
		return Selection{}, fmt.Errorf("unknown scope %q", scope)
	}
	sort.Slice(sel.Entries, func(i, j int) bool { return sel.Entries[i].Action < sel.Entries[j].Action })
	return sel, nil
}

func containsShortcut(list []Shortcut, x Shortcut) bool {
	for _, s := range list {
		if s == x {
			return true
		}
	}
	return false
}
