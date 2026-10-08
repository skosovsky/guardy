package tooling_test

import (
	"errors"
	"fmt"
	"testing"

	"golang.org/x/mod/modfile"
)

func validatePublishedSelection(data []byte, expected map[string]string) error {
	manifest, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return err
	}
	if len(manifest.Replace) != 0 {
		return errors.New("published consumer must not have replacements")
	}
	selected := make(map[string]string)
	for _, requirement := range manifest.Require {
		selected[requirement.Mod.Path] = requirement.Mod.Version
	}
	for path, version := range expected {
		if selected[path] != version {
			return fmt.Errorf("published selection differs: %s = %s, expected %s", path, selected[path], version)
		}
	}
	return nil
}

func TestPublishedSelection(t *testing.T) {
	for _, tc := range []struct {
		name, extra, peer string
		invalid           bool
	}{
		{"valid", "", "v0.18.0", false},
		{"own candidate", "\nrequire github.com/skosovsky/guardy v0.0.999\n", "v0.18.0", false},
		{"local replace", "\nreplace github.com/skosovsky/toolsy => /tmp/toolsy\n", "v0.18.0", true},
		{"wrong peer", "", "v0.17.0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange.
			data := []byte(
				"module example.invalid/consumer\n\ngo 1.27.1\n\nrequire github.com/skosovsky/toolsy " + tc.peer + "\n" + tc.extra,
			)
			// Act.
			err := validatePublishedSelection(data, map[string]string{"github.com/skosovsky/toolsy": "v0.18.0"})
			// Assert.
			if (err != nil) != tc.invalid {
				t.Fatalf("selection: %v", err)
			}
		})
	}
}
