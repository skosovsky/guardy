package tooling_test

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

func TestRepositoryModulePaths(t *testing.T) {
	// Arrange: use the same discovery as Make and release, without a directory registry.
	root := repoRoot(t)
	modules := strings.Fields(command(t, root, nil, "make", "--no-print-directory", "-s", "modules"))
	if len(modules) == 0 || modules[0] != "." {
		t.Fatal("missing root module")
	}
	// Act / Assert.
	for _, dir := range modules {
		path := filepath.Join(root, dir, "go.mod")
		manifest, err := modfile.Parse(path, read(t, path), nil)
		if err != nil {
			t.Fatal(err)
		}
		expected := "github.com/skosovsky/guardy"
		if dir != "." {
			expected += "/" + dir
		}
		if manifest.Module.Mod.Path != expected {
			t.Fatalf("%s: %s != %s", dir, manifest.Module.Mod.Path, expected)
		}
	}
}
