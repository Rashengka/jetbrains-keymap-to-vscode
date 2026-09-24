package vscode

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

var sample = []Entry{
	{Comment: "EditorDuplicate: meta d", Key: "cmd+d", Command: "editor.action.copyLinesDownAction", When: "editorTextFocus && !editorReadonly"},
	{Key: "cmd+e", Command: "workbench.action.showAllEditorsByMostRecentlyUsed"},
}

const userFile = `// Place your key bindings in this file to override the defaults
[
  // my own binding, keep it
  {
    "key": "ctrl+alt+x",
    "command": "workbench.action.terminal.new", /* inline comment */
    "when": "terminalFocus"
  },
  {
    "key": "ctrl+alt+y",
    "command": "-editor.action.foo",
  },
]
`

func TestApplyInsertsBlockAndKeepsUserContent(t *testing.T) {
	out, err := Apply([]byte(userFile), sample, "test header", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"// my own binding, keep it", "/* inline comment */", StartMarker + " test header", EndMarker, `"when": "editorTextFocus && !editorReadonly"`, "// EditorDuplicate: meta d"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	list, err := ParseKeybindings(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 {
		t.Fatalf("want 4 bindings, got %d", len(list))
	}
	if !strings.HasPrefix(s, userFile[:strings.Index(userFile, "  },\n]")]) {
		t.Errorf("text before the block changed:\n%s", s)
	}
}

func TestApplyIsIdempotentAndReplacesBlock(t *testing.T) {
	once, err := Apply([]byte(userFile), sample, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Apply(once, sample, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(once, twice) {
		t.Errorf("second run changed the file:\n%s\n---\n%s", once, twice)
	}
	smaller, err := Apply(once, sample[:1], "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	list, _ := ParseKeybindings(smaller)
	if len(list) != 3 {
		t.Errorf("want 3 bindings after replacing block, got %d", len(list))
	}
	empty, err := Apply(once, nil, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	list, _ = ParseKeybindings(empty)
	if len(list) != 2 {
		t.Errorf("want 2 bindings after emptying block, got %d", len(list))
	}
}

func TestApplyBlockInTheMiddle(t *testing.T) {
	once, err := Apply([]byte("[\n  {\"key\": \"a\", \"command\": \"x\"}\n]\n"), sample, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The user adds a binding after the block by hand.
	withAfter := strings.Replace(string(once), EndMarker+"\n", EndMarker+"\n  {\"key\": \"b\", \"command\": \"y\"}\n", 1)
	again, err := Apply([]byte(withAfter), sample, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	list, err := ParseKeybindings(again)
	if err != nil || len(list) != 4 {
		t.Fatalf("got %d bindings, err %v:\n%s", len(list), err, again)
	}
	if list[3].Key != "b" {
		t.Errorf("binding after the block moved: %+v", list)
	}
}

func TestApplyEmptyAndCommentOnlyFiles(t *testing.T) {
	for _, src := range []string{"", "  \n", "// only a comment\n"} {
		out, err := Apply([]byte(src), sample, "", nil)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		list, err := ParseKeybindings(out)
		if err != nil || len(list) != 2 {
			t.Errorf("%q: got %d bindings, err %v", src, len(list), err)
		}
		if strings.Contains(src, "comment") && !strings.Contains(string(out), "// only a comment") {
			t.Errorf("comment lost: %s", out)
		}
	}
}

func TestApplyKeepsCRLF(t *testing.T) {
	src := strings.ReplaceAll(userFile, "\n", "\r\n")
	out, err := Apply([]byte(src), sample, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ReplaceAll(string(out), "\r\n", ""), "\n") {
		t.Error("mixed line endings")
	}
}

func TestApplyRefusesBrokenInput(t *testing.T) {
	bad := []string{
		`{"key": "a"}`,                                       // not an array
		"[\n  " + StartMarker + "\n]\n",                      // start without end
		"[\n  " + EndMarker + "\n  " + StartMarker + "\n]\n", // reversed
		`[ {"key": "a", "command": "b" `,                     // truncated
	}
	for _, src := range bad {
		if _, err := Apply([]byte(src), sample, "", nil); err == nil {
			t.Errorf("expected error for %q", src)
		}
	}
}

func TestMarkerInsideStringIsIgnored(t *testing.T) {
	src := `[ {"key": "a", "command": "b", "when": "` + StartMarker + `"} ]`
	out, err := Apply([]byte(src), sample, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	list, _ := ParseKeybindings(out)
	if len(list) != 3 || list[0].When != StartMarker {
		t.Errorf("got %+v", list)
	}
}

var now = time.Date(2026, 9, 24, 11, 33, 5, 0, time.UTC)

func TestWriteBacksUpBeforeOverwriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	orig := []byte(userFile)
	if err := os.WriteFile(path, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Write(path, orig, []byte("[]\n"), true, now)
	if err != nil {
		t.Fatal(err)
	}
	want := path + ".2026-09-24T11-33-05.bak"
	if res.Backup != want {
		t.Errorf("backup name %q, want %q", res.Backup, want)
	}
	if b, _ := os.ReadFile(res.Backup); !bytes.Equal(b, orig) {
		t.Error("backup content differs from the original")
	}
	// Windows has no Unix permission bits to keep.
	if st, _ := os.Stat(path); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("permissions not kept: %v", st.Mode())
	}
	// A second write in the same second must not overwrite the first backup.
	res2, err := Write(path, []byte("[]\n"), []byte("[ ]\n"), true, now)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Backup == res.Backup {
		t.Error("backup overwritten")
	}
	if b, _ := os.ReadFile(res.Backup); !bytes.Equal(b, orig) {
		t.Error("first backup changed")
	}
	list, _ := ListBackups(path)
	if len(list) != 2 {
		t.Errorf("want 2 backups, got %v", list)
	}
}

func TestWriteRefusesWhenFileChangedMeanwhile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	if err := os.WriteFile(path, []byte("[ /* edited in VS Code */ ]"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Write(path, []byte("[]"), []byte("[1]"), true, now)
	if err == nil {
		t.Fatal("expected refusal")
	}
	if b, _ := os.ReadFile(path); string(b) != "[ /* edited in VS Code */ ]" {
		t.Error("file was overwritten")
	}
}

func TestWriteRefusesWhenBackupCannotBeCreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	orig := []byte("[]")
	if err := os.WriteFile(path, orig, 0o644); err != nil {
		t.Fatal(err)
	}
	// Occupy every possible backup name with a directory.
	base := path + "." + now.Format(BackupSuffix)
	os.Mkdir(base+".bak", 0o755)
	for i := 2; i < 100; i++ {
		os.Mkdir(base+"-"+strconv.Itoa(i)+".bak", 0o755)
	}
	if _, err := Write(path, orig, []byte("[1]"), true, now); err == nil {
		t.Fatal("expected failure")
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, orig) {
		t.Error("file was overwritten without a backup")
	}
}

func TestWriteFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "keybindings.json")
	os.MkdirAll(filepath.Dir(real), 0o755)
	os.WriteFile(real, []byte("[]"), 0o644)
	userDir := filepath.Join(dir, "User")
	os.MkdirAll(userDir, 0o755)
	link := filepath.Join(userDir, "keybindings.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks not available:", err)
	}
	res, err := Write(link, []byte("[]"), []byte("[ ]"), true, now)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Lstat(link); st.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink replaced by a regular file")
	}
	if b, _ := os.ReadFile(real); string(b) != "[ ]" {
		t.Error("target not updated")
	}
	if list, _ := ListBackups(link); len(list) != 1 || list[0] != res.Backup {
		t.Errorf("backup not listed via the link: %v %s", list, res.Backup)
	}
}

func TestWriteCreatesMissingFileOnlyInExistingDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	res, err := Write(path, nil, []byte("[]"), false, now)
	if err != nil || !res.Created || res.Backup != "" {
		t.Fatalf("res %+v err %v", res, err)
	}
	if _, err := Write(filepath.Join(dir, "missing", "keybindings.json"), nil, []byte("[]"), false, now); err == nil {
		t.Error("expected error for missing directory")
	}
}

func TestResolveBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	os.WriteFile(path, []byte("[]"), 0o644)
	b := path + ".2026-09-24T11-33-05.bak"
	os.WriteFile(b, []byte("[]"), 0o644)
	os.WriteFile(path+".notes.bak", []byte("x"), 0o644)
	list, _ := ListBackups(path)
	if len(list) != 1 {
		t.Errorf("got %v", list)
	}
	if got, err := ResolveBackup(path, filepath.Base(b)); err != nil || got != b {
		t.Errorf("got %q %v", got, err)
	}
	if _, err := ResolveBackup(path, "/etc/passwd"); err == nil {
		t.Error("accepted a foreign file")
	}
}

// sameBindings is the last guard before writing; it must notice any change.
func TestSameBindingsDetectsChanges(t *testing.T) {
	a := []RawBinding{{Key: "a", Command: "x", Args: []byte(`{"k": 1}`)}}
	for _, b := range [][]RawBinding{
		nil,
		{{Key: "b", Command: "x", Args: []byte(`{"k": 1}`)}},
		{{Key: "a", Command: "y", Args: []byte(`{"k": 1}`)}},
		{{Key: "a", Command: "x", When: "w", Args: []byte(`{"k": 1}`)}},
		{{Key: "a", Command: "x", Args: []byte(`{"k": 2}`)}},
	} {
		if sameBindings(a, b) {
			t.Errorf("not detected: %+v", b)
		}
	}
	if !sameBindings(a, []RawBinding{{Key: "a", Command: "x", Args: []byte(`{ "k":1 }`)}}) {
		t.Error("formatting of args must not matter")
	}
}

func TestKeepLineRoundTrip(t *testing.T) {
	out, err := Apply([]byte(userFile), sample, "h", []string{"cmd+g", "cmd+[Digit2]"})
	if err != nil {
		t.Fatal(err)
	}
	if got := ReadKeep(out); len(got) != 2 || got[0] != "cmd+g" || got[1] != "cmd+[Digit2]" {
		t.Errorf("got %v", got)
	}
	again, _ := Apply(out, sample, "h", nil)
	if ReadKeep(again) != nil {
		t.Error("an empty keep list removes the line")
	}
	if list, _ := ParseKeybindings(out); len(list) != 4 {
		t.Errorf("keep line must stay a comment: %d bindings", len(list))
	}
}
