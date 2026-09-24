package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/")

// TestGolden runs the whole pipeline on synthetic data and compares the
// written keybindings.json with testdata/. Any change in what the tool writes
// shows up here as a diff. After an intended change: go test -run Golden -update
func TestGolden(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"custom", []string{"--scope", "custom", "--layout", "cz-qwerty"}},
		{"all", []string{"--scope", "all", "--layout", "cz-qwerty"}},
		{"all-keep", []string{"--scope", "all", "--layout", "cz-qwerty", "--keep-recommended", "--keep-key", "cmd+ě"}},
		{"windows", []string{"--scope", "all", "--layout", "none", "--platform", "windows"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			args := append(f.convertArgs(), c.args...)
			args = append(args, "--apply")
			if out, code := f.run(t, args...); code != 0 {
				t.Fatalf("exit %d: %s", code, out)
			}
			got, err := os.ReadFile(f.keybindings)
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", "golden-"+c.name+".json")
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if string(got) != string(want) {
				t.Errorf("output differs from %s (run with -update if the change is intended)\n--- got ---\n%s", golden, got)
			}
		})
	}
}
