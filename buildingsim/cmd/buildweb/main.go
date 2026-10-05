package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var includePattern = regexp.MustCompile(`<include\s+file="([^"]+)"\s*/>`)

func main() {
	source := flag.String("src", "web/modern/src", "directory containing index.html and JavaScript fragments")
	output := flag.String("out", "web/modern/index.html", "generated HTML file")
	flag.Parse()
	if err := build(*source, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func build(sourceDir, outputPath string) error {
	templatePath := filepath.Join(sourceDir, "index.html")
	template, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read template: %w", err)
	}

	var includeErr error
	result := includePattern.ReplaceAllStringFunc(string(template), func(marker string) string {
		if includeErr != nil {
			return ""
		}
		match := includePattern.FindStringSubmatch(marker)
		name := match[1]
		if filepath.Base(name) != name || filepath.Ext(name) != ".js" {
			includeErr = fmt.Errorf("invalid include %q", name)
			return ""
		}
		content, readErr := os.ReadFile(filepath.Join(sourceDir, name))
		if readErr != nil {
			includeErr = fmt.Errorf("read include %q: %w", name, readErr)
			return ""
		}
		return "\n// ---- " + name + " ----\n" + strings.TrimSpace(string(content)) + "\n"
	})
	if includeErr != nil {
		return includeErr
	}
	if includePattern.MatchString(result) {
		return errors.New("unresolved include marker")
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".buildweb-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.WriteString(result); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write output: %w", err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set output mode: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("replace output: %w", err)
	}
	return nil
}
