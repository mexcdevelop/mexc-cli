package framework

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Property 15: the shared layer never reads the outbound rate budget from the
// environment.
//
// Edition.RateLimit takes a POLICY VALUE; deciding where its numbers come from is a
// product's business. The distinction is not tidiness — the shared default
// guards against an upstream per-IP ban, so if this module read an environment
// variable, a user of any locally invoked product could raise their own ceiling and
// earn that ban. Only a deployed face whose operator owns the budget should be able
// to tune it, and only that face reads the variable.
//
// Asserted over source text because that is the only way to state "nobody, anywhere
// in this module" — a behavioural test can only cover the call sites it thinks of.
//
// _Validates: R2.9, R13.4_
func TestSharedLayerDoesNotReadRateEnvironmentVariables(t *testing.T) {
	// Names a product might plausibly use for this knob. The point is not to
	// enumerate every possibility but to fail if this module starts reading ANY of
	// them, which is what a well-meaning "make it configurable" change would do.
	banned := []string{
		"OUTBOUND_RATE",
		"RATE_PER_SEC",
		"RATE_LIMIT",
		"ORDER_RATE",
	}

	root := repoRoot(t)
	var offenders []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			// vendor/ is other people's code; editions/ is where a PRODUCT lives, and
			// a product reading its own environment is exactly the correct design.
			case "vendor", "editions", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(src)
		for _, name := range banned {
			if strings.Contains(text, name) {
				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, rel+" mentions "+name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf("the shared layer reads an outbound-rate environment variable:\n  %s\n\n"+
			"The budget must arrive as a value on Edition.RateLimit. Reading it "+
			"here would let a user of a locally invoked product raise the ceiling that "+
			"exists to prevent an upstream IP ban.",
			strings.Join(offenders, "\n  "))
	}
}

// repoRoot walks up from the test's working directory to the module root, so the
// assertion covers the whole module rather than one package.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find the module root (no go.mod above the test's directory)")
		}
		dir = parent
	}
}
