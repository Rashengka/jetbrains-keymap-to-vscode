// Command jetbrains-keymap-to-vscode copies keyboard shortcuts from an
// installed JetBrains IDE into VS Code's keybindings.json.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/convert"
	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/ide"
	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/keymap"
	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/layout"
	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/release"
	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/vscode"
)

const usage = `jetbrains-keymap-to-vscode copies keyboard shortcuts from a JetBrains IDE to VS Code.

Usage:
  jetbrains-keymap-to-vscode list                 show detected JetBrains IDEs
  jetbrains-keymap-to-vscode convert [flags]      show what would be written (dry run)
  jetbrains-keymap-to-vscode convert --apply      write it (keybindings.json is backed up first)
  jetbrains-keymap-to-vscode restore [<backup>]   show what restoring a backup would change (default: newest)
  jetbrains-keymap-to-vscode restore [<backup>] --apply
                                                  restore it (the current file is backed up first)
  jetbrains-keymap-to-vscode version

Run "jetbrains-keymap-to-vscode <command> -h" for the flags of a command.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "list":
		err = cmdList(args[1:], stdout, stderr)
	case "convert":
		err = cmdConvert(args[1:], stdin, stdout, stderr)
	case "restore":
		err = cmdRestore(args[1:], stdout, stderr)
	case "version", "--version":
		fmt.Fprintln(stdout, versionString())
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// versionString is the version from VERSION plus the commit, when Go recorded it.
func versionString() string {
	v := release.Version()
	if info, ok := debug.ReadBuildInfo(); ok {
		rev, dirty := "", false
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if len(rev) > 12 {
			rev = rev[:12]
		}
		if rev != "" {
			v += "+" + rev
			if dirty {
				v += ".dirty"
			}
		}
	}
	return v
}

type detectFlags struct {
	configRoot string
	ideHome    string
}

func (d *detectFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&d.configRoot, "config-root", "", "directory with JetBrains configuration directories (default: the OS location)")
	fs.StringVar(&d.ideHome, "ide-home", "", "IDE installation directory with lib/ and plugins/ (default: detected)")
}

func (d *detectFlags) detect() ([]ide.IDE, error) {
	root := d.configRoot
	if root == "" {
		var err error
		if root, err = ide.DefaultConfigRoot(); err != nil {
			return nil, err
		}
	}
	configs, err := ide.ScanConfigs(root)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", root, err)
	}
	if len(configs) == 0 {
		return nil, fmt.Errorf("no JetBrains IDE configuration found in %s", root)
	}
	return ide.Detect(configs, ide.ScanInstalls(ide.DefaultInstallGlobs())), nil
}

func cmdList(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var d detectFlags
	d.register(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ides, err := d.detect()
	if err != nil {
		return err
	}
	for _, i := range ides {
		printIDE(stdout, i)
		fmt.Fprintln(stdout)
	}
	return nil
}

func printIDE(w io.Writer, i ide.IDE) {
	fmt.Fprintf(w, "%s %s\n", i.DisplayName(), i.Config.Version)
	fmt.Fprintf(w, "  config:  %s\n", i.Config.Dir)
	if i.Install != nil {
		fmt.Fprintf(w, "  install: %s (%s)\n", i.Install.Home, i.Install.Version)
	} else {
		fmt.Fprintf(w, "  install: not found (only --scope custom is possible, or pass --ide-home)\n")
	}
	km := ide.ActiveKeymapName(i.Config.Dir)
	if km == "" {
		km = ide.DefaultKeymapName() + " (default)"
	}
	fmt.Fprintf(w, "  keymap:  %s\n", km)
	for _, o := range i.Older {
		fmt.Fprintf(w, "  ignored older version: %s\n", o.Dir)
	}
}

func chooseIDE(ides []ide.IDE, name string, stdin io.Reader, stdout io.Writer) (ide.IDE, error) {
	if name != "" {
		return ide.Find(ides, name)
	}
	if len(ides) == 1 {
		return ides[0], nil
	}
	var names []string
	for _, i := range ides {
		names = append(names, i.DisplayName())
	}
	if !isTerminal(stdin) {
		return ide.IDE{}, fmt.Errorf("more than one IDE found, choose one with --ide: %s", strings.Join(names, ", "))
	}
	fmt.Fprintln(stdout, "Which IDE should the shortcuts come from?")
	for n, i := range ides {
		fmt.Fprintf(stdout, "  %d) %s %s\n", n+1, i.DisplayName(), i.Config.Version)
	}
	fmt.Fprint(stdout, "Number: ")
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(ides) {
		return ide.IDE{}, errors.New("no IDE chosen")
	}
	return ides[n-1], nil
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

type targetFlags struct {
	target      string
	keybindings string
}

func (t *targetFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&t.target, "target", "code", "editor to write to: code, insiders, vscodium or cursor")
	fs.StringVar(&t.keybindings, "keybindings", "", "path to keybindings.json (overrides --target)")
}

func (t *targetFlags) path() (string, error) {
	if t.keybindings != "" {
		return t.keybindings, nil
	}
	return vscode.KeybindingsPath(t.target)
}

func cmdConvert(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var d detectFlags
	var t targetFlags
	d.register(fs)
	t.register(fs)
	ideName := fs.String("ide", "", "IDE to read, e.g. PhpStorm or GoLand (asked interactively when omitted)")
	file := fs.String("file", "", "input instead of the IDE's keymap: JetBrains keymap .xml, JetBrains settings export .zip, or a VS Code keybindings .json (copied as it is)")
	scopeName := fs.String("scope", "custom", "custom: only your changes; all: defaults plus your changes; default: defaults without your changes")
	keymapName := fs.String("keymap", "", "keymap to read (default: the one active in the IDE or in --file)")
	platform := fs.String("platform", runtime.GOOS, "modifier names for: darwin, windows or linux")
	layoutName := fs.String("layout", "auto", "keyboard layout for keys like Cmd+ě: auto (ask macOS), none, or "+strings.Join(layout.Names(), ", "))
	var keepKeys, unkeepKeys listFlag
	fs.Var(&keepKeys, "keep-key", "leave this key to VS Code, e.g. cmd+g or cmd+ě (repeatable; remembered in keybindings.json)")
	fs.Var(&unkeepKeys, "unkeep-key", "stop leaving this key to VS Code (repeatable; \"recommended\" turns off --keep-recommended)")
	keepRecommended := fs.Bool("keep-recommended", false, "leave VS Code's documented default shortcuts to VS Code (remembered like --keep-key)")
	apply := fs.Bool("apply", false, "write keybindings.json (without it nothing is written)")
	verbose := fs.Bool("v", false, "show the exact JSON block and every skipped shortcut")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	scope, err := keymap.ParseScope(*scopeName)
	if err != nil {
		return err
	}

	lay, layoutNote, err := chooseLayout(*layoutName, *platform)
	if err != nil {
		return err
	}
	path, err := t.path()
	if err != nil {
		return err
	}
	var (
		chosen               *ide.IDE
		home, source, active string
		sel                  keymap.Selection
		res                  convert.Result
	)
	if *file != "" {
		switch strings.ToLower(filepath.Ext(*file)) {
		case ".json", ".xml", ".zip":
		default:
			return fmt.Errorf("--file %s: unknown type; use a VS Code .json, a JetBrains keymap .xml or a JetBrains settings export .zip", *file)
		}
	}
	vscodeInput := *file != "" && strings.EqualFold(filepath.Ext(*file), ".json")
	if vscodeInput {
		// A keybindings.json prepared elsewhere: copied into the block as it is.
		res, err = bindingsFromFile(*file, path)
		if err != nil {
			return err
		}
		source = "file " + filepath.Base(*file)
		sel = keymap.Selection{Entries: make([]keymap.Entry, len(res.Bindings))}
	} else {
		// The IDE provides the bundled keymaps the user keymap inherits from. With
		// --file and --scope custom it is optional.
		ides, err := d.detect()
		if err == nil {
			var c ide.IDE
			if c, err = chooseIDE(ides, *ideName, stdin, stdout); err == nil {
				chosen = &c
			}
		}
		if err != nil && (*file == "" || *ideName != "") {
			return err
		}
		home = d.ideHome
		if home == "" && chosen != nil && chosen.Install != nil {
			home = chosen.Install.Home
		}

		set := keymap.NewSet()
		if home != "" {
			st, err := keymap.LoadInstall(set, home)
			if err != nil {
				return err
			}
			for _, w := range st.Warnings {
				fmt.Fprintln(stderr, "warning:", w)
			}
		} else if scope != keymap.ScopeCustom {
			return fmt.Errorf("no IDE installation found; --scope %s needs its bundled keymaps (pass --ide or --ide-home)", scope)
		}
		active = *keymapName
		if *file != "" {
			fileActive, err := keymap.LoadFile(set, *file)
			if err != nil {
				return err
			}
			if active == "" {
				active = fileActive
			}
			source = "file " + filepath.Base(*file)
		} else {
			if err := keymap.LoadUserKeymaps(set, chosen.Config.Dir); err != nil {
				return err
			}
			if active == "" {
				active = ide.ActiveKeymapName(chosen.Config.Dir)
			}
			source = fmt.Sprintf("%s %s", chosen.DisplayName(), chosen.Config.Version)
		}
		if active == "" {
			active = ide.DefaultKeymapName()
		}
		sel, err = set.Select(active, scope)
		if err != nil {
			return err
		}
		res = convert.Convert(sel, convert.DefaultTable(), convert.Platform(*platform), lay)
	}

	current, exists, err := vscode.ReadCurrent(path)
	if err != nil {
		return err
	}
	if *keepRecommended {
		keepKeys = append(keepKeys, recommendedToken)
	}
	keep, keepNotes, err := keepList(vscode.ReadKeep(current), keepKeys, unkeepKeys, convert.Platform(*platform), lay)
	if err != nil {
		return err
	}
	var kept, keptRecommended []convert.Binding
	res.Bindings, kept = splitKept(res.Bindings, keep, convert.Platform(*platform), lay)
	if containsString(keep, recommendedToken) {
		res.Bindings, keptRecommended = splitRecommended(res.Bindings, convert.DefaultRecommended(convert.Platform(*platform)))
	}
	if vscodeInput {
		res.Conflict = convert.FindConflicts(res.Bindings)
	}

	var entries []vscode.Entry
	for _, b := range res.Bindings {
		e := vscode.Entry{Key: b.Key, Command: b.Command, When: b.When, Args: b.Args}
		if b.Action != "" {
			e.Comment = fmt.Sprintf("%s (%s)", b.Action, b.Shortcut)
		}
		entries = append(entries, e)
	}
	layoutPart := ""
	if lay != nil {
		layoutPart = ", layout " + lay.Name()
	}
	header := fmt.Sprintf("generated from %s, keymap %q, scope %s%s; edits inside this block are replaced on the next run", source, active, scope, layoutPart)
	if vscodeInput {
		header = fmt.Sprintf("copied from %s; edits inside this block are replaced on the next run", source)
	}
	updated, err := vscode.Apply(current, entries, header, keep)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	outside, _ := vscode.Outside(current)

	if *file != "" {
		fmt.Fprintf(stdout, "File:    %s\n", *file)
	}
	if chosen != nil {
		fmt.Fprintf(stdout, "IDE:     %s %s  (%s)\n", chosen.DisplayName(), chosen.Config.Version, chosen.Config.Dir)
		for _, o := range chosen.Older {
			fmt.Fprintf(stdout, "         ignored older version %s\n", filepath.Base(o.Dir))
		}
	}
	if home != "" {
		fmt.Fprintf(stdout, "Install: %s\n", home)
	}
	if vscodeInput {
		fmt.Fprintf(stdout, "Input:   VS Code keybindings, copied as they are\nTarget:  %s\n\n", path)
	} else {
		fmt.Fprintf(stdout, "Keymap:  %q", sel.Keymap)
		if sel.Base != "" && sel.Base != sel.Keymap {
			fmt.Fprintf(stdout, " (based on %q)", sel.Base)
		}
		fmt.Fprintf(stdout, "\nScope:   %s\nLayout:  %s\nTarget:  %s\n\n", scope, layoutNote, path)
	}

	if *verbose {
		fmt.Fprintln(stdout, "Block in keybindings.json:")
		fmt.Fprintln(stdout, blockPreview(updated))
	} else {
		fmt.Fprint(stdout, bindingTable(res.Bindings))
	}
	fmt.Fprint(stdout, report(res, sel, outside, *verbose))
	if len(keep) > 0 {
		fmt.Fprintf(stdout, "\nLeft to VS Code (--keep-key): %s\n", strings.Join(keep, ", "))
		for _, b := range kept {
			fmt.Fprintf(stdout, "  %-24s not written: %s -> %s\n", b.Key, b.Action, b.Command)
		}
		recommended := convert.DefaultRecommended(convert.Platform(*platform))
		for _, b := range keptRecommended {
			r := recommended[convert.ComparableKey(b.Key)]
			fmt.Fprintf(stdout, "  %-24s not written: %s -> %s  (VS Code default: %s)\n", b.Key, b.Action, b.Command, r.Name)
		}
	}
	for _, n := range keepNotes {
		fmt.Fprintln(stdout, "  note:", n)
	}

	if bytes.Equal(updated, current) {
		fmt.Fprintln(stdout, "\nkeybindings.json is already up to date; nothing to write.")
		return nil
	}
	if !*apply {
		fmt.Fprintln(stdout, "\nDry run: nothing was written. Run again with --apply to write (keybindings.json is backed up first).")
		return nil
	}
	wr, err := vscode.Write(path, current, updated, exists, time.Now())
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout)
	if wr.Backup != "" {
		fmt.Fprintf(stdout, "Backup:  %s\n", wr.Backup)
	}
	if wr.Created {
		fmt.Fprintf(stdout, "Created: %s\n", path)
	} else {
		fmt.Fprintf(stdout, "Written: %s\n", path)
	}
	return nil
}

// bindingsFromFile reads a keybindings.json prepared elsewhere. Keys are taken
// as they are: they are already VS Code keybindings, nothing is converted.
func bindingsFromFile(file, target string) (convert.Result, error) {
	var r convert.Result
	if same, _ := samePath(file, target); same {
		return r, fmt.Errorf("--file %s is the keybindings.json being written; copy it elsewhere first", file)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return r, err
	}
	list, err := vscode.ParseKeybindings(data)
	if err != nil {
		return r, fmt.Errorf("%s: %w", file, err)
	}
	for i, b := range list {
		if b.Key == "" || b.Command == "" {
			return r, fmt.Errorf("%s: entry %d has no key or no command", file, i+1)
		}
		r.Bindings = append(r.Bindings, convert.Binding{Key: b.Key, Command: b.Command, When: b.When, Args: b.Args})
	}
	return r, nil
}

func samePath(a, b string) (bool, error) {
	sa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(sa, sb), nil
}

// listFlag collects a repeatable flag; values may also be comma-separated.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }

func (l *listFlag) Set(v string) error {
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			*l = append(*l, s)
		}
	}
	return nil
}

// keepList merges the keys stored in the block with --keep-key and
// --unkeep-key. Every key is normalized the same way the converter writes
// keys, so "cmd+ě" and "cmd+[Digit2]" are the same entry.
func keepList(stored, add, remove []string, p convert.Platform, lay *layout.Layout) ([]string, []string, error) {
	var notes []string
	set := map[string]bool{}
	var order []string
	put := func(k string) {
		if !set[k] {
			set[k] = true
			order = append(order, k)
		}
	}
	for _, k := range stored {
		put(k)
	}
	for _, k := range add {
		if k == recommendedToken {
			put(k)
			continue
		}
		n, err := convert.NormalizeUserKey(k, p, lay)
		if err != nil {
			return nil, nil, fmt.Errorf("--keep-key: %w", err)
		}
		if n != k {
			notes = append(notes, fmt.Sprintf("--keep-key %s is the key %s", k, n))
		}
		put(n)
	}
	for _, k := range remove {
		if k == recommendedToken {
			delete(set, k)
			continue
		}
		n, err := convert.NormalizeUserKey(k, p, lay)
		if err != nil {
			return nil, nil, fmt.Errorf("--unkeep-key: %w", err)
		}
		if !set[n] {
			notes = append(notes, fmt.Sprintf("--unkeep-key %s: %s was not on the list", k, n))
		}
		delete(set, n)
	}
	var out []string
	for _, k := range order {
		if set[k] {
			out = append(out, k)
		}
	}
	return out, notes, nil
}

// recommendedToken on the keep list stands for --keep-recommended.
const recommendedToken = "recommended"

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// splitRecommended removes bindings that would take a documented VS Code
// default key for a different command. A chord whose first key is such a
// default is removed too, because it would capture that key.
func splitRecommended(bs []convert.Binding, rec map[string]convert.Recommended) (write, kept []convert.Binding) {
	for _, b := range bs {
		key := convert.ComparableKey(b.Key)
		r, ok := rec[key]
		if !ok {
			r, ok = rec[strings.Fields(key)[0]]
		}
		if ok && r.Command != b.Command {
			kept = append(kept, b)
			continue
		}
		write = append(write, b)
	}
	return write, kept
}

// splitKept removes bindings on kept keys. A kept single key also removes
// chords that start with it, because a chord would capture the first key.
func splitKept(bs []convert.Binding, keep []string, p convert.Platform, lay *layout.Layout) (write, kept []convert.Binding) {
	set := map[string]bool{}
	for _, k := range keep {
		set[k] = true
	}
	for _, b := range bs {
		key := b.Key
		// Keys from a prepared keybindings.json may be written differently
		// ("cmd+shift+g", "cmd+ě"); compare them in normalized form.
		if n, err := convert.NormalizeUserKey(b.Key, p, lay); err == nil {
			key = n
		}
		first := strings.Fields(key)[0]
		if set[b.Key] || set[key] || set[first] {
			kept = append(kept, b)
			continue
		}
		write = append(write, b)
	}
	return write, kept
}

// chooseLayout resolves --layout. The tables describe macOS layouts, so they
// are only used for macOS keybindings.
func chooseLayout(name, platform string) (*layout.Layout, string, error) {
	switch name {
	case "none", "":
		return nil, "none (layout-dependent keys are reported, not converted)", nil
	case "auto":
		if platform != string(convert.Mac) || runtime.GOOS != "darwin" {
			return nil, "unknown (tables exist for macOS only; layout-dependent keys are reported)", nil
		}
		id := layout.DetectMac()
		if id == "" {
			return nil, "unknown (macOS did not report it; use --layout)", nil
		}
		lay, err := layout.Get(id)
		if err != nil {
			return nil, fmt.Sprintf("%s from macOS has no table (layout-dependent keys are reported)", id), nil
		}
		return lay, lay.Name() + " (from macOS)", nil
	}
	if platform != string(convert.Mac) {
		return nil, "", fmt.Errorf("--layout %s: layout tables exist for macOS only", name)
	}
	lay, err := layout.Get(name)
	if err != nil {
		return nil, "", err
	}
	return lay, lay.Name(), nil
}

// bindingTable is the short form of the block: one line per keybinding.
func bindingTable(bs []convert.Binding) string {
	var b strings.Builder
	fmt.Fprintln(&b, "Keybindings (use -v for the exact JSON block):")
	for _, x := range bs {
		line := fmt.Sprintf("  %-24s %-44s %s", x.Key, x.Command, x.Action)
		if x.When != "" {
			line += "  [when " + x.When + "]"
		}
		fmt.Fprintln(&b, line)
	}
	return b.String()
}

// blockPreview returns only the managed block of the file.
func blockPreview(data []byte) string {
	s := string(data)
	i := strings.Index(s, vscode.StartMarker)
	j := strings.Index(s, vscode.EndMarker)
	if i < 0 || j < 0 {
		return s
	}
	return strings.TrimRight(s[lineStart(s, i):j+len(vscode.EndMarker)], "\r\n")
}

func lineStart(s string, i int) int {
	for i > 0 && s[i-1] != '\n' {
		i--
	}
	return i
}

func report(r convert.Result, sel keymap.Selection, outside []vscode.RawBinding, verbose bool) string {
	var b strings.Builder
	actions := map[string]bool{}
	for _, x := range r.Bindings {
		actions[x.Action] = true
	}
	fmt.Fprintf(&b, "\nConverted: %d keybindings for %d actions (of %d actions in scope).\n", len(r.Bindings), len(actions), len(sel.Entries))
	section := func(title string, items []convert.Skipped) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s (%d):\n", title, len(items))
		limit := len(items)
		if !verbose && limit > 15 {
			limit = 15
		}
		for _, s := range items[:limit] {
			line := fmt.Sprintf("  %-40s %-28s %s", s.Action, s.Shortcut, s.Reason)
			fmt.Fprintln(&b, strings.TrimRight(line, " "))
		}
		if limit < len(items) {
			fmt.Fprintf(&b, "  ... and %d more (use -v)\n", len(items)-limit)
		}
	}
	var reviewed, open []convert.Skipped
	for _, u := range r.Unmapped {
		if u.Reason != "" {
			reviewed = append(reviewed, u)
		} else {
			open = append(open, u)
		}
	}
	section("Not transferred: action not in the table yet", open)
	section("Not transferred: reviewed, VS Code has no counterpart", reviewed)
	unsafe := make([]convert.Skipped, len(r.Unsafe))
	for i, u := range r.Unsafe {
		unsafe[i] = convert.Skipped{Action: u.Action, Shortcut: u.Shortcut, Reason: "-> " + u.Reason + " would apply everywhere"}
	}
	section("Not transferred: key without Ctrl/Alt/Cmd and no when clause", unsafe)
	section("Not transferred: key cannot be expressed in VS Code", r.BadKeys)
	section("Not transferred: mouse shortcuts (VS Code has none)", r.Mouse)
	section("Default shortcuts you removed in JetBrains (VS Code defaults are left untouched)", r.Removed)
	if len(r.Conflict) > 0 {
		fmt.Fprintf(&b, "\nConflicts: same key and context bound to different commands (%d); the last one wins in VS Code:\n", len(r.Conflict))
		for _, c := range r.Conflict {
			fmt.Fprintf(&b, "  %s", c.Key)
			if c.When != "" {
				fmt.Fprintf(&b, "  when %s", c.When)
			}
			fmt.Fprintln(&b)
			for _, x := range c.Bindings {
				fmt.Fprintf(&b, "      %s -> %s\n", x.Action, x.Command)
			}
		}
	}
	var overlaps []string
	for _, o := range outside {
		for _, x := range r.Bindings {
			if o.Key == x.Key && o.Command != x.Command && !strings.HasPrefix(o.Command, "-") {
				overlaps = append(overlaps, fmt.Sprintf("  %-20s yours: %s  generated: %s", o.Key, o.Command, x.Command))
				break
			}
		}
	}
	if len(overlaps) > 0 {
		fmt.Fprintf(&b, "\nKeys you already use outside the block (%d); check the when clauses:\n%s\n", len(overlaps), strings.Join(overlaps, "\n"))
	}
	return b.String()
}

func cmdRestore(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var t targetFlags
	t.register(fs)
	apply := fs.Bool("apply", false, "restore the backup (without it nothing is written)")
	verbose := fs.Bool("v", false, "list the keybindings that the restore adds and removes")
	// Allow the backup name before or after the flags.
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return err
		}
		args = fs.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	if len(positional) > 1 {
		return errors.New("restore takes one backup name")
	}
	path, err := t.path()
	if err != nil {
		return err
	}
	list, err := vscode.ListBackups(path)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintf(stdout, "No backups of %s.\n", path)
		return nil
	}
	fmt.Fprintf(stdout, "Backups of %s (oldest first):\n", path)
	for _, b := range list {
		fmt.Fprintf(stdout, "  %s\n", filepath.Base(b))
	}
	var backup string
	if len(positional) == 0 {
		// The newest backup is the state before the last write by this tool,
		// so restoring it undoes that write (and a second restore undoes the restore).
		backup = list[len(list)-1]
		fmt.Fprintf(stdout, "\nNo backup given: using the newest one, the state before the last change made by this tool.\n")
	} else if backup, err = vscode.ResolveBackup(path, positional[0]); err != nil {
		return err
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		return err
	}
	fromBackup, err := vscode.ParseKeybindings(data)
	if err != nil {
		return fmt.Errorf("%s is not a valid keybindings file, refusing to restore it: %w", backup, err)
	}
	current, exists, err := vscode.ReadCurrent(path)
	if err != nil {
		return err
	}
	if exists && bytes.Equal(current, data) {
		fmt.Fprintf(stdout, "\n%s already has the content of %s; nothing to do.\n", path, filepath.Base(backup))
		return nil
	}
	var inCurrent []vscode.RawBinding
	if exists {
		inCurrent, _ = vscode.ParseKeybindings(current)
	}
	added, removed := diffBindings(inCurrent, fromBackup)
	fmt.Fprintf(stdout, "\nRestore %s from %s: %d keybindings now, %d after (+%d, -%d).\n",
		filepath.Base(path), filepath.Base(backup), len(inCurrent), len(fromBackup), len(added), len(removed))
	if *verbose {
		for _, b := range removed {
			fmt.Fprintf(stdout, "  - %s\n", describeBinding(b))
		}
		for _, b := range added {
			fmt.Fprintf(stdout, "  + %s\n", describeBinding(b))
		}
	}
	if !*apply {
		fmt.Fprintln(stdout, "Dry run: nothing was written. Add --apply to restore (the current file is backed up first); -v lists the changes.")
		return nil
	}
	wr, err := vscode.Write(path, current, data, exists, time.Now())
	if err != nil {
		return err
	}
	if wr.Backup != "" {
		fmt.Fprintf(stdout, "Backup of the current file: %s\n", wr.Backup)
	}
	fmt.Fprintf(stdout, "Restored %s from %s\n", path, filepath.Base(backup))
	return nil
}

func bindingID(b vscode.RawBinding) string {
	var args bytes.Buffer
	if len(b.Args) > 0 && json.Compact(&args, b.Args) != nil {
		args.Reset()
		args.Write(b.Args)
	}
	return b.Key + "\x00" + b.Command + "\x00" + b.When + "\x00" + args.String()
}

// diffBindings returns bindings present only in after (added) and only in before (removed).
func diffBindings(before, after []vscode.RawBinding) (added, removed []vscode.RawBinding) {
	count := map[string]int{}
	for _, b := range before {
		count[bindingID(b)]++
	}
	for _, b := range after {
		id := bindingID(b)
		if count[id] > 0 {
			count[id]--
			continue
		}
		added = append(added, b)
	}
	left := map[string]int{}
	for _, b := range after {
		left[bindingID(b)]++
	}
	for _, b := range before {
		id := bindingID(b)
		if left[id] > 0 {
			left[id]--
			continue
		}
		removed = append(removed, b)
	}
	return added, removed
}

func describeBinding(b vscode.RawBinding) string {
	s := fmt.Sprintf("%-24s %s", b.Key, b.Command)
	if b.When != "" {
		s += "  [when " + b.When + "]"
	}
	return s
}
