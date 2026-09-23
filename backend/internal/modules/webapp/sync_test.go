package webapp

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedSourcesMatchFrontend(t *testing.T) {
	repoFrontend := filepath.Join("..", "..", "..", "..", "frontend")
	files := []string{
		"index.html", "app.js", "workspace.js", "mfa.js", "features.js", "screens.js", "okz.js", "style.css", "theme.css",
		"components/ui.js", "core/router.js", "core/screen-loader.js", "core/store.js", "shell/app-shell.js",
		"screens/dashboard/index.js", "screens/employment-practice/index.js", "screens/internship/index.js",
		"screens/minc-decision/index.js", "screens/ood-rpd/index.js", "screens/partners/index.js",
		"screens/reports/index.js", "screens/schools/index.js", "screens/settings/index.js",
		"screens/shared/activity-screen.js", "screens/teachers/index.js", "screens/top-it/index.js",
	}
	for _, name := range files {
		source, err := os.ReadFile(filepath.Join(repoFrontend, name))
		if err != nil {
			t.Fatalf("read frontend/%s: %v", name, err)
		}
		copy, err := os.ReadFile(filepath.Join("dist", name))
		if err != nil {
			t.Fatalf("read embedded %s: %v; run scripts/sync-embedded-frontend.sh", name, err)
		}
		if !bytes.Equal(source, copy) {
			t.Fatalf("embedded %s is stale; run scripts/sync-embedded-frontend.sh", name)
		}
	}
}
