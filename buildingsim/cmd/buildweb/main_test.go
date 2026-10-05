package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildExpandsIncludes(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "index.html"), `<script><include file="one.js"/><include file="two.js"/></script>`)
	mustWrite(t, filepath.Join(dir, "one.js"), "const one = 1;")
	mustWrite(t, filepath.Join(dir, "two.js"), "const two = 2;")
	output := filepath.Join(dir, "out", "index.html")

	if err := build(dir, output); err != nil {
		t.Fatalf("build: %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"const one = 1;", "const two = 2;", "// ---- one.js ----"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output does not contain %q", want)
		}
	}
	if includePattern.Match(data) {
		t.Fatal("output still contains an include marker")
	}
}

func TestBuildRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "index.html"), `<include file="../secret.js"/>`)
	if err := build(dir, filepath.Join(dir, "out.html")); err == nil {
		t.Fatal("expected traversal include to fail")
	}
}

func TestBuildReportsMissingInclude(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "index.html"), `<include file="missing.js"/>`)
	if err := build(dir, filepath.Join(dir, "out.html")); err == nil {
		t.Fatal("expected missing include to fail")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
