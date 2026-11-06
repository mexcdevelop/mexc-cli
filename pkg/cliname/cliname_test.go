package cliname

import "testing"

// Each test restores the package value: it is process-global by design, so a
// test that leaked its value would silently change what every later test sees.
func withName(t *testing.T, n string) {
	t.Helper()
	prev := name
	t.Cleanup(func() { name = prev })
	Set(n)
}

func TestName_DefaultBeforeAnySet(t *testing.T) {
	if got := Name(); got != defaultName {
		t.Fatalf("Name() = %q, want the default %q", got, defaultName)
	}
}

func TestSet_RecordsTheInvokedName(t *testing.T) {
	withName(t, "mexc")
	if got := Name(); got != "mexc" {
		t.Fatalf("Name() = %q, want %q", got, "mexc")
	}
}

// A path-like or empty argv[0] is ignored rather than stored. Set is fed
// filepath.Base(os.Args[0]), so these are the shapes that arrive when the caller
// is a shell doing something unusual — and a hint reading "run `/usr/local/bin/x
// schema`" or "run `. schema`" is worse than one naming the default.
func TestSet_RejectsUnusableNames(t *testing.T) {
	for _, in := range []string{"", "   ", "/usr/local/bin/mexc", `C:\bin\mexc.exe`, "./mexc"} {
		t.Run(in, func(t *testing.T) {
			withName(t, in)
			if got := Name(); got != defaultName {
				t.Fatalf("Set(%q) stored %q; an unusable name must leave the default %q", in, got, defaultName)
			}
		})
	}
}

func TestSet_TrimsSurroundingSpace(t *testing.T) {
	withName(t, "  mexc  ")
	if got := Name(); got != "mexc" {
		t.Fatalf("Name() = %q, want it trimmed to %q", got, "mexc")
	}
}
