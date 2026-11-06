package config

import (
	"errors"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// The settable key set is REGISTERED, not compiled in, so these tests register
// their own key rather than leaning on a product's. That is the honest shape for
// a registry — it must work for whatever a composition root declares — and it
// keeps this package from asserting that any particular product exists.
const testKey = "settings-test-key"

func withTestKey(t *testing.T) {
	t.Helper()
	prev := settingKeys
	t.Cleanup(func() { settingKeys = prev })
	settingKeys = map[string]bool{}
	RegisterSetting(testKey)
}

func TestSettings_RoundTrip(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	withTestKey(t)

	if v, err := GetSetting(testKey); err != nil || v != "" {
		t.Fatalf("fresh %s = %q, err = %v; want empty", testKey, v, err)
	}
	if err := SetSetting(testKey, "example-value"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v, _ := GetSetting(testKey); v != "example-value" {
		t.Fatalf("%s = %q, want example-value", testKey, v)
	}
	if err := UnsetSetting(testKey); err != nil {
		t.Fatalf("unset: %v", err)
	}
	if v, _ := GetSetting(testKey); v != "" {
		t.Fatalf("after unset %s = %q, want empty", testKey, v)
	}
}

// A key nobody registered is refused on every path. This is what makes the set
// closed: an unknown key is never silently stored.
func TestSettings_UnknownKeyRejected(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	withTestKey(t)

	err := SetSetting("bogus", "x")
	var e *errs.Error
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeInvalidArgument {
		t.Fatalf("SetSetting(bogus) err = %v, want invalid_argument", err)
	}
	if _, err := GetSetting("bogus"); err == nil {
		t.Fatalf("GetSetting(bogus) should error")
	}
	if ValidSettingKey("bogus") || !ValidSettingKey(testKey) {
		t.Fatalf("ValidSettingKey wrong: %s=%v bogus=%v",
			testKey, ValidSettingKey(testKey), ValidSettingKey("bogus"))
	}
}

// Nothing is settable until a composition root says so. A product that
// configures nothing gets an empty set — which is what framework reads to decide
// the `config` command has no reason to exist.
func TestSettings_EmptyUntilRegistered(t *testing.T) {
	prev := settingKeys
	t.Cleanup(func() { settingKeys = prev })
	settingKeys = map[string]bool{}

	if got := SettingKeys(); len(got) != 0 {
		t.Fatalf("a bare registry has keys %v, want none", got)
	}
	if ValidSettingKey(SettingEnv) {
		t.Fatalf("%q is valid before any product registered it", SettingEnv)
	}
	RegisterSetting(SettingEnv)
	if !ValidSettingKey(SettingEnv) {
		t.Fatalf("%q is not valid after registration", SettingEnv)
	}
	if got := SettingKeys(); len(got) != 1 || got[0] != SettingEnv {
		t.Fatalf("SettingKeys = %v, want [%s]", got, SettingEnv)
	}
}

// GlobalEnv reads through GetSetting, so a product that never registered env
// gets "" — a value another product left in the shared file shape cannot route
// a request here.
func TestGlobalEnv_InertWhenUnregistered(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	prev := settingKeys
	t.Cleanup(func() { settingKeys = prev })

	settingKeys = map[string]bool{SettingEnv: true}
	if err := SetSetting(SettingEnv, "left-behind"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if GlobalEnv() != "left-behind" {
		t.Fatalf("GlobalEnv = %q, want the stored value", GlobalEnv())
	}

	settingKeys = map[string]bool{} // a product that does not offer env
	if got := GlobalEnv(); got != "" {
		t.Fatalf("GlobalEnv = %q with env unregistered, want empty", got)
	}
}

func TestSettings_RejectsNewline(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	withTestKey(t)
	if err := SetSetting(testKey, "a\nx=y"); err == nil {
		t.Fatalf("a newline in a value must be rejected")
	}
}

func TestSettings_MissingFileIsEmpty(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	withTestKey(t)
	m, err := LoadSettings()
	if err != nil || len(m) != 0 {
		t.Fatalf("LoadSettings on a fresh home = %v, err = %v; want empty", m, err)
	}
	if GlobalEnv() != "" {
		t.Fatalf("GlobalEnv on a fresh home = %q, want empty", GlobalEnv())
	}
}
