package config

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// Global settings are CLI-wide, identity-agnostic configuration persisted under
// the config file in this product's state dir (see statedir.go) as key=value
// lines (mode 0600). They are a CLOSED set: an unknown key is refused, never
// silently stored, so the config surface stays typed and documented.
//
// The set is REGISTERED, not compiled in, because what is configurable differs
// per product. It starts empty; the composition root registers each key the
// product actually offers. `env` is registered only where more than one env is
// served — a product with a single deployment has no env to choose, and
// offering the setting would be offering a choice that does not exist.
//
// Credentials do NOT live here — those are a Profile (api key + secret), because
// a credential is identity-scoped while env is a global routing axis shared by
// every identity (the api-key and login-token identities alike). Per-invocation
// flags (--dry-run, --base-url, --read-only) are not persisted here either.

// settingKeys is the closed set of valid global setting keys, filled by
// RegisterSetting at composition time.
var settingKeys = map[string]bool{}

// RegisterSetting declares a global setting key this product offers. Called by
// the composition root before the command tree is built; the `config` command is
// mounted only when at least one key is registered, so a product that configures
// nothing does not carry a command that can configure nothing.
func RegisterSetting(key string) { settingKeys[key] = true }

// SettingEnv is the deployment-environment key. It is named here rather than
// spelled as a literal at each site so the registration, the lookup and the
// engine's precedence chain cannot disagree about its spelling.
const SettingEnv = "env"

// SettingKeys returns every valid setting key, sorted.
func SettingKeys() []string {
	ks := make([]string, 0, len(settingKeys))
	for k := range settingKeys {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// ValidSettingKey reports whether key is a defined global setting.
func ValidSettingKey(key string) bool { return settingKeys[key] }

func unknownSettingKey(key string) error {
	return errs.Validation(errs.SubtypeInvalidArgument,
		"unknown setting %q; valid keys: %s", key, strings.Join(SettingKeys(), ", ")).
		WithHint("run `%s config list` to see the settable keys", cliname.Name())
}

func settingsPath() (string, error) {
	d, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config"), nil
}

// LoadSettings reads all persisted settings (key -> value). A missing file is not
// an error — it yields an empty map. Unknown keys present in the file are ignored,
// so a stale key from a newer/older build never leaks into behavior.
func LoadSettings() (map[string]string, error) {
	path, err := settingsPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, errs.Internal("cannot read config: %v", err).WithCause(err)
	}
	defer func() { _ = f.Close() }()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if k = strings.TrimSpace(k); settingKeys[k] {
			out[k] = strings.TrimSpace(v)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, errs.Internal("cannot read config: %v", err).WithCause(err)
	}
	return out, nil
}

// GetSetting returns the persisted value of key ("" if unset). An unknown key is
// a structured error.
func GetSetting(key string) (string, error) {
	if !settingKeys[key] {
		return "", unknownSettingKey(key)
	}
	m, err := LoadSettings()
	if err != nil {
		return "", err
	}
	return m[key], nil
}

// SetSetting persists key=value. An unknown key, or a value containing a newline
// (which would be re-parsed on load as an extra key=value line), is refused.
func SetSetting(key, value string) error {
	if !settingKeys[key] {
		return unknownSettingKey(key)
	}
	if strings.ContainsAny(value, "\r\n") {
		return errs.Validation(errs.SubtypeInvalidArgument,
			"setting %q value must not contain a newline", key)
	}
	m, err := LoadSettings()
	if err != nil {
		return err
	}
	m[key] = value
	return writeSettings(m)
}

// UnsetSetting removes key from the persisted settings (a no-op if it was unset).
func UnsetSetting(key string) error {
	if !settingKeys[key] {
		return unknownSettingKey(key)
	}
	m, err := LoadSettings()
	if err != nil {
		return err
	}
	delete(m, key)
	return writeSettings(m)
}

// writeSettings rewrites the whole config file, keys sorted, empty values dropped.
func writeSettings(m map[string]string) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		if m[k] == "" {
			continue
		}
		b.WriteString(k + "=" + m[k] + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), stateFilePerm); err != nil {
		return errs.Internal("cannot write config: %v", err).WithCause(err)
	}
	return nil
}

// GlobalEnv is the persisted env setting ("" if unset or not offered by this
// product). resolveConfig consumes it as the lowest-precedence env source, under
// MEXC_ENV and --env.
//
// A product that never registered the key gets "" from GetSetting's
// unknown-key path, so a value left in the file by another product — or by this
// one before it dropped to a single env — is inert rather than silently routing.
func GlobalEnv() string {
	v, _ := GetSetting(SettingEnv)
	return v
}
