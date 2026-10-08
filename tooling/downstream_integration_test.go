//go:build integration

package tooling_test

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

type peerSelection struct {
	Sources map[string]struct {
		Repository string `json:"repository"`
		Revision   string `json:"revision"`
	} `json:"sources"`
	Published map[string]string `json:"published"`
}

func copyTree(t *testing.T, source, target string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative != "." && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() {
			write(t, filepath.Join(target, relative), read(t, path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationDownstreamSelections(t *testing.T) {
	// Arrange: retain exact source and published peers from the existing compatibility contract.
	root := repoRoot(t)
	var selection peerSelection
	if err := json.Unmarshal(
		read(t, filepath.Join(root, "integration/downstream/conformance.json")),
		&selection,
	); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"sources", "published"} {
		t.Run(mode, func(t *testing.T) { checkIntegrationDownstreamSelections(t, mode, root, selection) })
	}
}

func checkIntegrationDownstreamSelections(t *testing.T, mode string, root string, selection peerSelection) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "consumer")
	copyTree(t, filepath.Join(root, "integration/downstream"), target)
	env := []string{"GOENV=off", "GOFLAGS=", "GOWORK=off"}
	ownSource := ""
	if mode == "sources" {
		ownSource = sourceSnapshot(t, root)
	}
	configureOwnDependencies(t, target, ownSource, env)
	if mode == "sources" {
		for name, peer := range selection.Sources {
			checkout := filepath.Join(t.TempDir(), name)
			command(t, root, env, "git", "clone", "--quiet", "--no-checkout", peer.Repository, checkout)
			command(t, checkout, env, "git", "checkout", "--quiet", "--detach", peer.Revision)
			if actual := command(t, checkout, env, "git", "rev-parse", "HEAD"); actual != peer.Revision {
				t.Fatalf("peer revision: %s", actual)
			}
			data, err := modfile.Parse("go.mod", read(t, filepath.Join(checkout, "go.mod")), nil)
			if err != nil {
				t.Fatal(err)
			}
			command(t, target, env, "go", "mod", "edit", "-replace="+data.Module.Mod.Path+"="+checkout)
			t.Logf("source %s %s", name, peer.Revision)
		}
	} else if err := validatePublishedSelection(read(t, filepath.Join(target, "go.mod")), selection.Published); err != nil {
		t.Fatal(err)
	}
	// Act: both tagged suites must exercise real tool execution and producer lifecycles.
	command(t, target, env, "go", "mod", "download")
	graph := command(t, target, env, "go", "list", "-m", "-json", "all")
	if mode == "published" && strings.Contains(graph, `"Replace"`) {
		t.Fatal("published graph has replacements")
	}
	t.Log(graph)
	// Assert: failures propagate; ordinary unit/example tests do not replace these suites.
	t.Log(
		command(
			t,
			target,
			env,
			"go",
			"test",
			"-mod=readonly",
			"-race",
			"-count=1",
			"-tags=integration,e2e",
			"-run=^Test(Integration|E2E)",
			"-v",
			"./...",
		),
	)
}

// Derive development replacements from the consumer manifest, not an inventory list.
func configureOwnDependencies(t *testing.T, target, source string, env []string) {
	t.Helper()
	manifest, err := modfile.Parse("go.mod", read(t, filepath.Join(target, "go.mod")), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, requirement := range manifest.Require {
		path := requirement.Mod.Path
		if !isOwnModule(path) {
			continue
		}
		command(t, target, env, "go", "mod", "edit", "-dropreplace="+path)
		if source != "" {
			relative := strings.TrimPrefix(strings.TrimPrefix(path, "github.com/skosovsky/guardy"), "/")
			command(t, target, env, "go", "mod", "edit", "-replace="+path+"="+filepath.Join(source, relative))
		}
	}
}
