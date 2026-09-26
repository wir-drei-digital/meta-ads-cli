// Package config resolves the CLI's settings from the config file and the
// environment. Environment variables win, except for the budget caps and the
// currency, which only the config file can set: an agent's environment must
// not be able to raise a limit a person chose.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultAPIBase is the Graph API host.
const DefaultAPIBase = "https://graph.facebook.com"

// DefaultAPIVersion is the Graph API version a path without its own version uses.
const DefaultAPIVersion = "v26.0"

// userConfigDir is swapped in tests.
var userConfigDir = os.UserConfigDir

// Cap is a budget cap in the minor unit of the currency it was entered for,
// bound to the ad account it was entered for. A cap whose account or
// currency differs from the effective one refuses every budget it would
// check, until a person sets it again: META_ADS_AD_ACCOUNT_ID can point the
// CLI at another account, but never carry a cap across to it.
type Cap struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
	Account  string `json:"ad_account_id"` // digits, without act_
}

// Config is the on-disk configuration.
type Config struct {
	AccessToken    string `json:"access_token,omitempty"`
	AppSecret      string `json:"app_secret,omitempty"`
	AppID          string `json:"app_id,omitempty"`
	AdAccountID    string `json:"ad_account_id,omitempty"` // digits, without act_
	Currency       string `json:"currency,omitempty"`
	DailyCap       *Cap   `json:"daily_budget_cap,omitempty"`
	LifetimeCap    *Cap   `json:"lifetime_budget_cap,omitempty"`
	ReadOnly       bool   `json:"read_only,omitempty"`
	TokenExpiresAt string `json:"token_expires_at,omitempty"` // RFC 3339, "never", or ""
}

// Path returns <os.UserConfigDir()>/metaads/config.json.
func Path() (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "metaads", "config.json"), nil
}

// Load reads the config file. A missing file is the zero Config; a corrupt
// file is an error.
func Load() (Config, error) {
	p, err := Path()
	if err != nil {
		return Config{}, err
	}
	raw, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return Config{}, fmt.Errorf("%s is corrupt, repair or delete it: %w", p, err)
	}
	return c, nil
}

// Save writes the config file with 0600 perms inside a 0700 directory,
// through a temp file and a rename, so a reader never sees a partial file.
func Save(c Config) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	// Same directory as the target: rename is atomic only within a
	// filesystem, and CreateTemp already makes the file 0600.
	tmp, err := os.CreateTemp(filepath.Dir(p), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	// fsync before the rename: rename is atomic, not durable.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return err
	}
	// Best effort: make the rename itself durable. Windows cannot open a
	// directory for this, which is fine.
	if d, err := os.Open(filepath.Dir(p)); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// Resolved is the effective configuration after the environment overlay.
type Resolved struct {
	AccessToken     string
	TokenFrom       string // "env", "config" or ""
	AppSecret       string
	AppID           string
	AdAccountID     string // digits, or ""
	Currency        string // config file only
	DailyCap        *Cap   // config file only
	LifetimeCap     *Cap   // config file only
	ReadOnly        bool
	TokenExpiresAt  string
	APIBase         string
	AllowCustomBase bool
	// FromEnv names the META_ADS_* variables that were set non-empty.
	// Names only, never values.
	FromEnv map[string]bool
}

// Resolve loads the config file and overlays the environment.
func Resolve(getenv func(string) string) (Resolved, error) {
	c, err := Load()
	if err != nil {
		return Resolved{}, err
	}
	r := Resolved{
		AccessToken: c.AccessToken, AppSecret: c.AppSecret, AppID: c.AppID, Currency: c.Currency,
		DailyCap: c.DailyCap, LifetimeCap: c.LifetimeCap, ReadOnly: c.ReadOnly,
		TokenExpiresAt: c.TokenExpiresAt, APIBase: DefaultAPIBase,
	}
	if r.AccessToken != "" {
		r.TokenFrom = "config"
	}
	env := func(name string) string {
		v := strings.TrimSpace(getenv(name))
		if v != "" {
			if r.FromEnv == nil {
				r.FromEnv = map[string]bool{}
			}
			r.FromEnv[name] = true
		}
		return v
	}
	if v := env("META_ADS_ACCESS_TOKEN"); v != "" {
		r.AccessToken, r.TokenFrom = v, "env"
	}
	if v := env("META_ADS_APP_SECRET"); v != "" {
		r.AppSecret = v
	}
	if v := env("META_ADS_APP_ID"); v != "" {
		r.AppID = v
	}
	if r.AppID != "" {
		if r.AppID, err = NormalizeAppID(r.AppID); err != nil {
			return Resolved{}, fmt.Errorf("app ID: %w", err)
		}
	}
	account := c.AdAccountID
	if v := env("META_ADS_AD_ACCOUNT_ID"); v != "" {
		account = v
	}
	if account != "" {
		if r.AdAccountID, err = NormalizeAccountID(account); err != nil {
			return Resolved{}, fmt.Errorf("ad account ID: %w", err)
		}
	}
	if v := env("META_ADS_READ_ONLY"); v == "1" || strings.EqualFold(v, "true") {
		r.ReadOnly = true
	}
	if v := env("META_ADS_API_BASE"); v != "" {
		r.APIBase = strings.TrimRight(v, "/")
	}
	if env("META_ADS_ALLOW_CUSTOM_BASE") == "1" {
		r.AllowCustomBase = true
	}
	return r, nil
}

var digits = regexp.MustCompile(`^[0-9]{1,20}$`)

// NormalizeAccountID accepts an ad account ID as Ads Manager shows it
// ("act_1234567890") or as bare digits, and returns the digits.
func NormalizeAccountID(s string) (string, error) {
	d := strings.TrimPrefix(strings.TrimSpace(s), "act_")
	if !digits.MatchString(d) {
		return "", fmt.Errorf("want an ad account ID such as act_1234567890 or 1234567890, got %q", s)
	}
	return d, nil
}

// NormalizeAppID accepts a Meta app ID: digits only.
func NormalizeAppID(s string) (string, error) {
	d := strings.TrimSpace(s)
	if !digits.MatchString(d) {
		return "", fmt.Errorf("want an app ID of digits, got %q", s)
	}
	return d, nil
}
