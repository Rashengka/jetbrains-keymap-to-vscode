// Command release makes and checks releases.
//
//	go run ./tools/release <patch|minor|major|X.Y.Z> "<description>"
//
// writes the new version to internal/release/VERSION, commits it and creates
// the annotated tag vX.Y.Z, both with the message "vX.Y.Z: <description>".
// Nothing is pushed. Push the branch and the tag yourself; the tag starts the
// release workflow.
//
//	go run ./tools/release check <tag> [notes-file]
//
// is what CI runs before publishing: the tag must match VERSION in the tagged
// commit and carry a real description. With notes-file it writes the release
// notes (the tag message).
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Rashengka/jetbrains-keymap-to-vscode/internal/release"
)

const versionPath = "internal/release/VERSION"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) >= 2 && args[0] == "check" {
		notes := ""
		if len(args) == 3 {
			notes = args[2]
		}
		return check(args[1], notes)
	}
	if len(args) != 2 {
		return fmt.Errorf(`usage: release <patch|minor|major|X.Y.Z> "<description>" | release check <tag> [notes-file]`)
	}
	return bump(args[0], args[1])
}

func git(args ...string) (string, error) {
	var out, errOut bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errOut.String()))
	}
	return out.String(), nil
}

func bump(kind, description string) error {
	status, err := git("status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("the working tree has uncommitted changes; commit or move them first:\n%s", status)
	}
	current, err := os.ReadFile(versionPath)
	if err != nil {
		return err
	}
	next, err := release.Bump(strings.TrimSpace(string(current)), kind)
	if err != nil {
		return err
	}
	msg, err := release.Message(next, description)
	if err != nil {
		return err
	}
	tag := "v" + next
	if _, err := git("rev-parse", "-q", "--verify", "refs/tags/"+tag); err == nil {
		return fmt.Errorf("tag %s already exists", tag)
	}
	if err := os.WriteFile(versionPath, []byte(next+"\n"), 0o644); err != nil {
		return err
	}
	if _, err := git("commit", "-m", msg, "--", versionPath); err != nil {
		return err
	}
	if _, err := git("tag", "-a", tag, "-m", msg); err != nil {
		return err
	}
	branch, _ := git("rev-parse", "--abbrev-ref", "HEAD")
	fmt.Printf("%s\n\nNothing was pushed. To publish:\n  git push origin %s\n  git push origin %s\n", msg, strings.TrimSpace(branch), tag)
	return nil
}

func check(tag, notesFile string) error {
	versionFile, err := git("show", tag+":"+versionPath)
	if err != nil {
		return err
	}
	objectType, err := git("cat-file", "-t", "refs/tags/"+tag)
	if err != nil {
		return err
	}
	annotation := ""
	if strings.TrimSpace(objectType) == "tag" {
		if annotation, err = git("tag", "-l", "--format=%(contents)", tag); err != nil {
			return err
		}
	}
	if err := release.Check(tag, versionFile, strings.TrimSpace(objectType), annotation); err != nil {
		return err
	}
	if notesFile != "" {
		if err := os.WriteFile(notesFile, []byte(strings.TrimSpace(annotation)+"\n"), 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("%s ok: %s\n", tag, strings.SplitN(strings.TrimSpace(annotation), "\n", 2)[0])
	return nil
}
