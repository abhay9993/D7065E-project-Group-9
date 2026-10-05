package buildingsim

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContainerPackagingKeepsHostPortLocalAndContainerPortReachable(t *testing.T) {
	dockerfile := readLocalText(t, "Dockerfile")
	compose := readLocalText(t, "compose.yaml")
	for _, marker := range []string{
		`USER 65532:65532`,
		`"--host", "0.0.0.0"`,
		`/buildsim", "health"`,
	} {
		if !strings.Contains(dockerfile, marker) {
			t.Errorf("Dockerfile is missing %q", marker)
		}
	}
	for _, marker := range []string{
		`"127.0.0.1:9090:9090"`,
		`["start", "--host", "0.0.0.0"`,
		`http://127.0.0.1:9090/healthz`,
	} {
		if !strings.Contains(compose, marker) {
			t.Errorf("compose.yaml is missing %q", marker)
		}
	}
}

func TestDistributedSourceContainsNoRemovedIntegrations(t *testing.T) {
	banned := []string{
		"rae" + "kta",
		"sty" + "rops",
		"colony" + "os",
		"chat" + "-panel",
		"/api/" + "chat",
		"cla" + "ude",
		"co" + "dex",
	}
	extensions := map[string]bool{".go": true, ".html": true, ".js": true, ".css": true}
	err := fs.WalkDir(os.DirFS("."), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (path == ".git" || path == "web/vendor" || path == "bin" || path == "dist") {
			return fs.SkipDir
		}
		if entry.IsDir() || !extensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		data, readErr := fs.ReadFile(os.DirFS("."), path)
		if readErr != nil {
			return readErr
		}
		lower := strings.ToLower(string(data))
		for _, term := range banned {
			if strings.Contains(lower, term) {
				t.Errorf("%s contains removed integration marker %q", path, term)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readLocalText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
