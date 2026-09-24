// Package release holds the version number and the rules for publishing it.
//
// The version has exactly one source: the VERSION file next to this package,
// embedded into the binary. A release is an annotated tag vX.Y.Z whose message
// describes the release; CI creates the GitHub release only when Check passes.
package release

import (
	_ "embed"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

//go:embed VERSION
var versionFile string

// Version is the version of this build, read from the VERSION file.
func Version() string { return strings.TrimSpace(versionFile) }

var semverRe = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`)

// Parse validates an X.Y.Z version.
func Parse(v string) ([3]int, error) {
	m := semverRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return [3]int{}, fmt.Errorf("%q is not a version in X.Y.Z form", v)
	}
	var out [3]int
	for i := range out {
		out[i], _ = strconv.Atoi(m[i+1])
	}
	return out, nil
}

// Bump returns the next version: kind is patch, minor, major or an explicit X.Y.Z,
// which must be greater than the current one.
func Bump(current, kind string) (string, error) {
	cur, err := Parse(current)
	if err != nil {
		return "", fmt.Errorf("VERSION: %w", err)
	}
	next := cur
	switch kind {
	case "patch":
		next[2]++
	case "minor":
		next = [3]int{cur[0], cur[1] + 1, 0}
	case "major":
		next = [3]int{cur[0] + 1, 0, 0}
	default:
		if next, err = Parse(kind); err != nil {
			return "", fmt.Errorf("use patch, minor, major or X.Y.Z: %w", err)
		}
		if !less(cur, next) {
			return "", fmt.Errorf("%s is not newer than %s", kind, current)
		}
	}
	return fmt.Sprintf("%d.%d.%d", next[0], next[1], next[2]), nil
}

func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// Message is the commit and tag message of a release.
func Message(version, description string) (string, error) {
	d := strings.TrimSpace(description)
	if d == "" {
		return "", fmt.Errorf("a release needs a description")
	}
	return "v" + version + ": " + d, nil
}

// Check decides whether CI may publish tag. versionFile is the content of
// VERSION in the tagged commit, annotation the tag message ("" for a
// lightweight tag), objectType the git object type of the tag ref.
func Check(tag, versionFile, objectType, annotation string) error {
	v := strings.TrimSpace(versionFile)
	if _, err := Parse(v); err != nil {
		return fmt.Errorf("VERSION in the tagged commit: %w", err)
	}
	if tag != "v"+v {
		return fmt.Errorf("tag %s does not match VERSION %s (expected v%s)", tag, v, v)
	}
	if objectType != "tag" {
		return fmt.Errorf("tag %s is not annotated; create it with: go run ./tools/release", tag)
	}
	a := strings.TrimSpace(annotation)
	if a == "" {
		return fmt.Errorf("tag %s has an empty message (a shallow clone loses it: fetch tags with --force)", tag)
	}
	rest := strings.TrimSpace(strings.TrimPrefix(a, tag))
	rest = strings.TrimSpace(strings.TrimPrefix(rest, ":"))
	if rest == "" {
		return fmt.Errorf("tag %s message is only the version number; describe the release", tag)
	}
	return nil
}
