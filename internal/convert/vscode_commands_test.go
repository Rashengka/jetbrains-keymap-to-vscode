package convert

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommandsExistInVSCode checks every command of the action table against an
// installed VS Code: the workbench bundle and the built-in extensions. It needs
// VS Code on the machine (set VSCODE_APP to the "app" directory, or have it in
// /Applications on macOS) and is skipped elsewhere, e.g. in CI.
func TestCommandsExistInVSCode(t *testing.T) {
	app := os.Getenv("VSCODE_APP")
	if app == "" {
		app = "/Applications/Visual Studio Code.app/Contents/Resources/app"
	}
	bundle, err := os.ReadFile(filepath.Join(app, "out", "vs", "workbench", "workbench.desktop.main.js"))
	if err != nil {
		t.Skip("VS Code not found; set VSCODE_APP to check commands:", err)
	}
	js := string(bundle)
	ext := map[string]bool{}
	manifests, _ := filepath.Glob(filepath.Join(app, "extensions", "*", "package.json"))
	for _, m := range manifests {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var pkg struct {
			Contributes struct {
				Commands []struct {
					Command string `json:"command"`
				} `json:"commands"`
			} `json:"contributes"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			for _, c := range pkg.Contributes.Commands {
				ext[c.Command] = true
			}
		}
	}
	for action, maps := range DefaultTable() {
		for _, m := range maps {
			if !ext[m.Command] && !strings.Contains(js, `"`+m.Command+`"`) {
				t.Errorf("%s: command %q not found in VS Code", action, m.Command)
			}
		}
	}
}
