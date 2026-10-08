//go:build integration

package tooling_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
)

func artifactProxy(t *testing.T, source, version, proxy string) []string {
	t.Helper()
	dirs := strings.Fields(command(t, source, nil, "make", "--no-print-directory", "-s", "modules"))
	paths := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		base := filepath.Join(source, dir)
		data := read(t, filepath.Join(base, "go.mod"))
		manifest, err := modfile.Parse("go.mod", data, nil)
		if err != nil {
			t.Fatal(err)
		}
		path := manifest.Module.Mod.Path
		paths = append(paths, path)
		for _, r := range manifest.Replace {
			if isOwnModule(r.Old.Path) {
				t.Fatalf("development replacement in %s", path)
			}
		}
		for _, r := range manifest.Require {
			if isOwnModule(r.Mod.Path) && r.Mod.Version != version {
				t.Fatalf("wrong candidate dependency %s", r.Mod)
			}
		}
		escaped, err := module.EscapePath(path)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(proxy, escaped, "@v")
		var archive bytes.Buffer
		if err := modzip.CreateFromDir(&archive, module.Version{Path: path, Version: version}, base); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(target, version+".zip"), archive.Bytes())
		write(t, filepath.Join(target, version+".mod"), data)
		write(
			t,
			filepath.Join(target, version+".info"),
			fmt.Appendf(nil, `{"Version":%q,"Time":"2026-01-01T00:00:00Z"}`, version),
		)
		write(t, filepath.Join(target, "list"), []byte(version+"\n"))
	}
	return paths
}

func consume(t *testing.T, source, version string, env []string, paths []string) {
	t.Helper()
	dir := t.TempDir()
	command(t, dir, env, "go", "mod", "init", "example.invalid/releaseconsumer")
	for _, path := range paths {
		command(t, dir, env, "go", "mod", "edit", "-require="+path+"@"+version)
	}
	imports := map[string]bool{}
	for _, path := range paths {
		// Compile library, executable and test-only modules without running repository tests.
		command(t, dir, env, "go", "test", "-mod=mod", "-run=^$", path+"/...")
		packages := strings.FieldsSeq(
			command(
				t,
				dir,
				env,
				"go",
				"list",
				"-mod=mod",
				"-f",
				`{{if and (ne .Name "main") (or .GoFiles .CgoFiles)}}{{.ImportPath}}{{end}}`,
				path+"/...",
			),
		)
		for p := range packages {
			if !strings.Contains(p, "/internal/") {
				imports[p] = true
			}
		}
	}
	var code strings.Builder
	code.WriteString("package consumer\nimport (\n")
	for path := range imports {
		fmt.Fprintf(&code, "_ %q\n", path)
	}
	code.WriteString(")\n")
	write(t, filepath.Join(dir, "consumer.go"), []byte(code.String()))
	command(t, dir, env, "go", "mod", "tidy")
	// Tidy removes modules containing only commands/tests; still verify their exact versions.
	for _, path := range paths {
		command(t, dir, env, "go", "mod", "edit", "-require="+path+"@"+version)
	}
	command(t, dir, env, "go", "mod", "download")
	checkResolvedModules(t, dir, env, paths, version)
	command(t, dir, env, "go", "test", "-race", "-count=1", "./...")
	command(t, dir, env, "go", "build", "./...")
	// Execute the repository onboarding against artifacts, not local source replacements.
	example := read(t, filepath.Join(source, "examples/input_guard/main.go"))
	write(t, filepath.Join(dir, "cmd/demo/main.go"), example)
	demo := exec.CommandContext(t.Context(), "go", "run", "./cmd/demo")
	demo.Dir = dir
	demo.Env = append(append(os.Environ(), "GOWORK=off"), env...)
	demo.Stdin = strings.NewReader("hello guardy\n")
	output, err := demo.CombinedOutput()
	if err != nil || string(output) != "OK\nhello guardy\n" {
		t.Fatalf("example: %v %s", err, output)
	}
	// Execute the adapter's semantic tests from its module ZIP, not a local checkout.
	t.Log(
		command(
			t,
			dir,
			env,
			"go",
			"test",
			"-mod=mod",
			"-race",
			"-count=1",
			"-tags=integration,e2e",
			"-run=^Test(Integration|E2E)",
			"-v",
			"github.com/skosovsky/guardy/integration/downstream/...",
		),
	)
}

func checkResolvedModules(t *testing.T, dir string, env []string, paths []string, version string) {
	t.Helper()
	listed := command(t, dir, env, "go", "list", "-m", "-json", "all")
	decoder := json.NewDecoder(strings.NewReader(listed))
	seen := map[string]bool{}
	for decoder.More() {
		var m struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
			Replace any    `json:"Replace"`
		}
		if err := decoder.Decode(&m); err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			if m.Path == path {
				if m.Version != version || m.Replace != nil {
					t.Fatalf("wrong resolved dependency %+v", m)
				}
				seen[path] = true
			}
		}
	}
	if len(seen) != len(paths) {
		t.Fatal("missing published modules")
	}
}

func TestIntegrationReleaseArtifacts(t *testing.T) {
	// Arrange: prepare current source in an isolated directory.
	source := sourceCandidate(t, repoRoot(t))
	version := "v0.0.999"
	proxy := filepath.Join(t.TempDir(), "proxy")
	// Arrange: standard x/mod artifact construction; no homemade ZIP format.
	paths := artifactProxy(t, source, version, proxy)
	internal := http.FileServer(http.Dir(proxy))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/github.com/skosovsky/guardy/") {
			internal.ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, "https://proxy.golang.org"+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)
	env := []string{
		"GOENV=off",
		"GOFLAGS=-modcacherw",
		"GOPROXY=" + server.URL,
		"GONOPROXY=none",
		"GONOSUMDB=github.com/skosovsky/guardy,github.com/skosovsky/guardy/*",
		"GOSUMDB=sum.golang.org",
		"GOMODCACHE=" + filepath.Join(t.TempDir(), "modcache"),
	}
	// Act / Assert: actual external Go resolution, compilation and executable example.
	consume(t, source, version, env, paths)
}

func sourceSnapshot(t *testing.T, root string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "source")
	// Export tracked and nonignored current source; hidden metadata is excluded below.
	for name := range strings.SplitSeq(command(t, root, nil, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"), "\x00") {
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(target, name), data)
	}
	return target
}

func sourceCandidate(t *testing.T, root string) string {
	t.Helper()
	target := sourceSnapshot(t, root)
	for dir := range strings.FieldsSeq(command(t, target, nil, "make", "--no-print-directory", "-s", "modules")) {
		rewriteFixtureManifest(t, filepath.Join(target, dir))
	}

	return target
}

func rewriteFixtureManifest(t *testing.T, base string) {
	t.Helper()
	path := filepath.Join(base, "go.mod")
	m, err := modfile.Parse(path, read(t, path), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range m.Require {
		if isOwnModule(r.Mod.Path) {
			if err = m.AddRequire(r.Mod.Path, "v0.0.999"); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, r := range append([]*modfile.Replace(nil), m.Replace...) {
		if isOwnModule(r.Old.Path) {
			if err = m.DropReplace(r.Old.Path, r.Old.Version); err != nil {
				t.Fatal(err)
			}
		}
	}
	data, err := m.Format()
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, data)
}

func isOwnModule(path string) bool {
	return path == "github.com/skosovsky/guardy" || strings.HasPrefix(path, "github.com/skosovsky/guardy/")
}
