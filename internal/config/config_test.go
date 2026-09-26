package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := userConfigDir
	userConfigDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userConfigDir = old })
	return dir
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func mustSave(t *testing.T, c Config) {
	t.Helper()
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
}

func TestPathAndMissingFile(t *testing.T) {
	dir := setTestDir(t)
	p, err := Path()
	if err != nil || p != filepath.Join(dir, "metaads", "config.json") {
		t.Fatalf("path %q %v", p, err)
	}
	c, err := Load()
	if err != nil || c.AccessToken != "" || c.DailyCap != nil {
		t.Fatalf("missing file must load as zero: %+v %v", c, err)
	}
}

func TestSaveLoadRoundTripAndPerms(t *testing.T) {
	setTestDir(t)
	in := Config{AccessToken: "tok", AppSecret: "sec", AppID: "42", AdAccountID: "7", Currency: "CHF",
		DailyCap: &Cap{Minor: 3000, Currency: "CHF", Account: "7"}, LifetimeCap: &Cap{Minor: 30000, Currency: "CHF", Account: "7"},
		ReadOnly: true, TokenExpiresAt: "never"}
	mustSave(t, in)
	out, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.AccessToken != "tok" || out.AppSecret != "sec" || out.AppID != "42" || out.AdAccountID != "7" ||
		out.Currency != "CHF" || out.DailyCap == nil || *out.DailyCap != *in.DailyCap ||
		out.LifetimeCap == nil || *out.LifetimeCap != *in.LifetimeCap || !out.ReadOnly || out.TokenExpiresAt != "never" {
		t.Fatalf("round trip: %+v", out)
	}
	if p, _ := Path(); !fileContains(t, p, `"daily_budget_cap":{"minor":3000,"currency":"CHF","ad_account_id":"7"}`) {
		t.Fatal("a cap must be stored with the ad account it was entered for")
	}
	if runtime.GOOS == "windows" {
		return
	}
	p, _ := Path()
	fi, err := os.Stat(p)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v %v", fi.Mode().Perm(), err)
	}
	di, err := os.Stat(filepath.Dir(p))
	if err != nil || di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v %v", di.Mode().Perm(), err)
	}
}

func TestCorruptFileIsAnError(t *testing.T) {
	setTestDir(t)
	p, _ := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("want a corrupt-file error, got %v", err)
	}
}

func TestResolveFileThenEnv(t *testing.T) {
	setTestDir(t)
	mustSave(t, Config{AccessToken: "file-tok", AppID: "42", AdAccountID: "7", Currency: "CHF"})
	r, err := Resolve(env(nil))
	if err != nil || r.AccessToken != "file-tok" || r.TokenFrom != "config" || r.AdAccountID != "7" ||
		r.AppID != "42" || r.APIBase != DefaultAPIBase || r.AllowCustomBase {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = Resolve(env(map[string]string{"META_ADS_ACCESS_TOKEN": " env-tok ", "META_ADS_AD_ACCOUNT_ID": "act_99",
		"META_ADS_APP_ID": "43", "META_ADS_APP_SECRET": "s"}))
	if err != nil || r.AccessToken != "env-tok" || r.TokenFrom != "env" || r.AdAccountID != "99" ||
		r.AppID != "43" || r.AppSecret != "s" || !r.FromEnv["META_ADS_ACCESS_TOKEN"] {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestResolveCapsAndCurrencyOnlyFromFile(t *testing.T) {
	setTestDir(t)
	mustSave(t, Config{Currency: "CHF", DailyCap: &Cap{Minor: 3000, Currency: "CHF", Account: "7"}})
	r, err := Resolve(env(map[string]string{"META_ADS_CURRENCY": "JPY", "META_ADS_DAILY_BUDGET_CAP": "999999",
		"META_ADS_LIFETIME_BUDGET_CAP": "999999"}))
	if err != nil || r.Currency != "CHF" || r.DailyCap == nil || r.DailyCap.Minor != 3000 || r.LifetimeCap != nil {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestResolveRejectsBadIDs(t *testing.T) {
	setTestDir(t)
	if _, err := Resolve(env(map[string]string{"META_ADS_AD_ACCOUNT_ID": "act_x1"})); err == nil {
		t.Fatal("a malformed ad account ID was accepted")
	}
	if _, err := Resolve(env(map[string]string{"META_ADS_APP_ID": "abc"})); err == nil {
		t.Fatal("a malformed app ID was accepted")
	}
}

func TestResolveReadOnlyAndBase(t *testing.T) {
	setTestDir(t)
	r, err := Resolve(env(map[string]string{"META_ADS_READ_ONLY": "true", "META_ADS_API_BASE": "http://127.0.0.1:9/",
		"META_ADS_ALLOW_CUSTOM_BASE": "1"}))
	if err != nil || !r.ReadOnly || r.APIBase != "http://127.0.0.1:9" || !r.AllowCustomBase {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestNormalizeAccountID(t *testing.T) {
	for in, want := range map[string]string{"act_123": "123", "123": "123", " act_42 ": "42"} {
		if got, err := NormalizeAccountID(in); err != nil || got != want {
			t.Errorf("%q: got %q %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "act_", "act_12a", "12-3", "act 12"} {
		if _, err := NormalizeAccountID(in); err == nil {
			t.Errorf("%q was accepted", in)
		}
	}
}

func TestNormalizeAppID(t *testing.T) {
	if got, err := NormalizeAppID(" 1234 "); err != nil || got != "1234" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := NormalizeAppID("12ab"); err == nil {
		t.Fatal("a malformed app ID was accepted")
	}
}

func fileContains(t *testing.T, path, want string) bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(raw), want)
}
