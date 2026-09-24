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
	"strconv"
	"strings"
	"time"

	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/convert"
	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/ide"
	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/keymap"
	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/vscode"
)

var version = "dev"

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
		fmt.Fprintln(stdout, version)
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
	scopeName := fs.String("scope", "custom", "custom: only your changes; all: defaults plus your changes; default: defaults without your changes")
	keymapName := fs.String("keymap", "", "keymap to read (default: the one active in the IDE)")
	platform := fs.String("platform", runtime.GOOS, "modifier names for: darwin, windows or linux")
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
	ides, err := d.detect()
	if err != nil {
		return err
	}
	chosen, err := chooseIDE(ides, *ideName, stdin, stdout)
	if err != nil {
		return err
	}
	home := d.ideHome
	if home == "" && chosen.Install != nil {
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
		return fmt.Errorf("installation of %s not found; --scope %s needs its bundled keymaps (pass --ide-home)", chosen.DisplayName(), scope)
	}
	if err := keymap.LoadUserKeymaps(set, chosen.Config.Dir); err != nil {
		return err
	}
	active := *keymapName
	if active == "" {
		active = ide.ActiveKeymapName(chosen.Config.Dir)
	}
	if active == "" {
		active = ide.DefaultKeymapName()
	}
	sel, err := set.Select(active, scope)
	if err != nil {
		return err
	}
	res := convert.Convert(sel, convert.DefaultTable(), convert.Platform(*platform))

	path, err := t.path()
	if err != nil {
		return err
	}
	current, exists, err := vscode.ReadCurrent(path)
	if err != nil {
		return err
	}
	var entries []vscode.Entry
	for _, b := range res.Bindings {
		entries = append(entries, vscode.Entry{
			Comment: fmt.Sprintf("%s (%s)", b.Action, b.Shortcut),
			Key:     b.Key, Command: b.Command, When: b.When, Args: b.Args,
		})
	}
	header := fmt.Sprintf("generated from %s %s, keymap %q, scope %s; edits inside this block are replaced on the next run", chosen.DisplayName(), chosen.Config.Version, active, scope)
	updated, err := vscode.Apply(current, entries, header)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	outside, _ := vscode.Outside(current)

	fmt.Fprintf(stdout, "IDE:     %s %s  (%s)\n", chosen.DisplayName(), chosen.Config.Version, chosen.Config.Dir)
	for _, o := range chosen.Older {
		fmt.Fprintf(stdout, "         ignored older version %s\n", filepath.Base(o.Dir))
	}
	if home != "" {
		fmt.Fprintf(stdout, "Install: %s\n", home)
	}
	fmt.Fprintf(stdout, "Keymap:  %q", sel.Keymap)
	if sel.Base != "" && sel.Base != sel.Keymap {
		fmt.Fprintf(stdout, " (based on %q)", sel.Base)
	}
	fmt.Fprintf(stdout, "\nScope:   %s\nTarget:  %s\n\n", scope, path)

	if *verbose {
		fmt.Fprintln(stdout, "Block in keybindings.json:")
		fmt.Fprintln(stdout, blockPreview(updated))
	} else {
		fmt.Fprint(stdout, bindingTable(res.Bindings))
	}
	fmt.Fprint(stdout, report(res, sel, outside, *verbose))

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
	section("Not transferred: no VS Code command for the action", r.Unmapped)
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
