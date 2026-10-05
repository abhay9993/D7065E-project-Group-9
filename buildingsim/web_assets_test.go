package buildingsim

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedViewersAndVendorAssets(t *testing.T) {
	classic := embeddedText(t, "web/index.html")
	modern := embeddedText(t, "web/modern/index.html")
	for name, page := range map[string]string{"classic": classic, "modern": modern} {
		for _, marker := range []string{
			"<title>BuildSim</title>",
			"/vendor/three.module.js",
			`"three/addons/controls/OrbitControls.js": "/vendor/OrbitControls.js"`,
			"/api/sessions",
			"/api/occupancy",
			"globalOccupancyState",
			"sessionCoverageState",
		} {
			if !strings.Contains(page, marker) {
				t.Errorf("%s viewer is missing %q", name, marker)
			}
		}
		if strings.Contains(page, "<include ") {
			t.Errorf("%s viewer contains an unresolved include", name)
		}
		for _, marker := range []string{
			"window._viewerSessionId",
			"window._buildsimDiagnostics",
			"EQUIPMENT_REFRESH_INTERVAL_MS",
			"scheduleEquipmentRefresh",
			"equipmentRefreshPending",
			"from_level=",
			"to_level=",
			"dataset.routeLevel",
		} {
			if !strings.Contains(page, marker) {
				t.Errorf("%s viewer is missing live-update safeguard %q", name, marker)
			}
		}
	}
	if !strings.Contains(classic, `id="info-panel"`) || !strings.Contains(classic, `id="session-chip"`) ||
		!strings.Contains(modern, `id="boot-loader"`) || !strings.Contains(modern, `id="sessionChip"`) {
		t.Fatal("viewer modes are not distinct")
	}
	for _, marker := range []string{`id="roomLayerButtons"`, "renderRoomLayerButtons", "room-layer-btn"} {
		if !strings.Contains(modern, marker) {
			t.Errorf("modern viewer is missing clickable room-layer control %q", marker)
		}
	}
	for _, removed := range []string{`id="showExtWalls"`, `id="showSun"`, `id="sunSliders"`, "// ---- sun.js ----"} {
		if strings.Contains(modern, removed) {
			t.Errorf("modern viewer still contains removed exterior/sun control %q", removed)
		}
	}
	for _, filename := range []string{
		"web/vendor/three.module.js",
		"web/vendor/OrbitControls.js",
		"web/vendor/jspdf.umd.min.js",
		"web/vendor/README.md",
	} {
		if _, err := fs.Stat(WebFS, filename); err != nil {
			t.Errorf("embedded asset %s: %v", filename, err)
		}
	}
}

func TestModernGeneratedPageContainsEverySourceFragment(t *testing.T) {
	page := embeddedText(t, "web/modern/index.html")
	entries, err := fs.ReadDir(WebFS, "web/modern/src")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".js") {
			continue
		}
		marker := "// ---- " + entry.Name() + " ----"
		if !strings.Contains(page, marker) {
			t.Errorf("generated modern viewer is missing %s", entry.Name())
		}
		content, readErr := fs.ReadFile(WebFS, "web/modern/src/"+entry.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !strings.Contains(page, strings.TrimSpace(string(content))) {
			t.Errorf("generated modern viewer contains stale content for %s; run go generate ./...", entry.Name())
		}
	}
}

func TestViewerHasNoRemoteRuntimeOrEmbeddedCredentials(t *testing.T) {
	banned := []string{
		"cdn.jsdelivr", "unpkg.com", "cdnjs.cloudflare",
		"private key",
	}
	err := fs.WalkDir(WebFS, "web", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path == "web/vendor" {
			return fs.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		data, readErr := fs.ReadFile(WebFS, path)
		if readErr != nil {
			return readErr
		}
		lower := strings.ToLower(string(data))
		for _, term := range banned {
			if strings.Contains(lower, term) {
				t.Errorf("%s contains removed or remote integration %q", path, term)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func embeddedText(t *testing.T, path string) string {
	t.Helper()
	data, err := fs.ReadFile(WebFS, path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
