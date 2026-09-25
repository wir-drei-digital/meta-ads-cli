# metaads (Meta Ads CLI) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `metaads`, a single static Go binary that reaches the whole Meta Graph and Marketing API through `get`, `post` and `delete` on Graph paths, authenticates with a system user token, and refuses to start delivery, change a budget or exceed a budget cap without an explicit, visible decision.

**Architecture:** `internal/route` parses Graph paths (`[vNN.N/]<node>[/<edge>]`, `act` for the configured ad account). `internal/guard` classifies every request by verb and path, then reads every field that can start delivery or move money, including nested specs, before anything is sent. `internal/api` sends form-encoded (or multipart) requests to `graph.facebook.com` with the token in the Authorization header and `appsecret_proof` in the query, maps Meta's error codes to error kinds, and retries rate limits using Meta's usage headers. `internal/cli` wires it together with cobra.

**Tech Stack:** Go 1.26, `github.com/spf13/cobra` v1.10.2 (brings `github.com/spf13/pflag`), `golang.org/x/term` v0.45.0, goreleaser, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-09-25-meta-ads-cli-design.md`. Read it before your task; this plan argues from it.

**Reference implementation:** `/Users/daniel/Development/google-ads-cli` (and `/Users/daniel/Development/bexio-cli`), sibling CLIs by the same author. Where a step says "copy X from google-ads-cli", copy the file and make exactly the listed changes. Never import anything from those repositories: they share no module with this one.

## Global Constraints

- Repo root: `/Users/daniel/Development/meta-ads-cli`. Run every command from there. Work happens directly on `main`.
- Module `github.com/wir-drei-digital/meta-ads-cli`; binary `metaads`; `go 1.26` in `go.mod`; must build with `CGO_ENABLED=0`.
- Dependencies: only `github.com/spf13/cobra v1.10.2` (added in Task 4), `golang.org/x/term v0.45.0` (added in Task 5), and their requirements (`github.com/spf13/pflag` is imported directly by the catalog). Add them with `go get <module>@<version>`, then `go mod tidy`.
- Default Graph API version `v26.0` (`config.DefaultAPIVersion`). A path may start with its own version.
- API host `graph.facebook.com` over HTTPS (`config.DefaultAPIBase = "https://graph.facebook.com"`). Any other host only with `META_ADS_ALLOW_CUSTOM_BASE=1`; non-HTTPS refused except on loopback (`localhost`, `127.0.0.1`, `::1`).
- stdout carries only the API response (or a config/auth command's own result); every failure is exactly one line of JSON on stderr with keys `kind`, `error`, `status` (omitted when no response), `details`, `request_id` (omitted when unknown). Exit codes: `0` success, `1` API or network error, `2` usage error, which includes every guardrail refusal.
- Secrets never come from argv. The access token and the app secret come from stdin (`config set access-token`, `config set app-secret`), from `META_ADS_ACCESS_TOKEN` / `META_ADS_APP_SECRET`, or from a hidden prompt in `init`.
- The access token travels only in `Authorization: Bearer <token>`, never in a URL. Error messages and `--verbose` output print paths without query strings.
- `--force` is a per-call flag only. The budget caps and the currency live only in the config file: no environment variable, no flag.
- Risk classes are exactly `read`, `write`, `delete`, `spend`, `admin`; `delete`, `spend` and `admin` need `--force`.
- `go vet ./...` clean; `go test ./...` passes on Linux, macOS and Windows without network access. The only exception is `e2e/live_test.go`, which skips unless `META_ADS_LIVE=1`.
- All code, identifiers, comments and docs in English. Prose in `README.md`, `SECURITY.md`, `docs/agents.md` and in every message the CLI prints contains no em dash character; use a colon, a semicolon, a comma or a new sentence.
- Commit at the end of every task. Every commit message ends with a blank line followed by `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

These inputs are implied by the spec but not spelled out there. Each has a test in the task that owns the code.

1. **A nested spec or `adset_budgets` written as a JSON string**, the way Meta's curl examples write every nested value (`"adset_spec": "{\"status\":\"ACTIVE\"}"`): the guard parses the string and applies the same checks as to an object; a string that does not parse refuses the request. Tests: Task 3 (`TestNestedSpecs`, `TestAdsetBudgets`).
2. **Budget amounts written loosely**: `" 3000"`, `"+3000"`, `3e3`, `"3000.0"`, `"03000"`, `3000.0` are refused with a message to write a whole number in the minor unit, never compared loosely against a cap. `"3000"` and `3000` are accepted. Test: Task 3 (`TestAmountForms`).
3. **Paths pasted from Meta's docs**: a leading `/` and a leading version (`v24.0/act_1/campaigns`) are accepted; a query string (`act/campaigns?fields=name`) is refused with a message pointing at `--fields`, `--param` and `--data`, and nothing is sent. Tests: Task 1 (`TestParse`), Task 4 (`TestPostPathRefusals`).
4. **Swiss input habits**: `config set daily-budget-cap 29,50` is refused with a message to use a dot (never read as 2950 or 29); `config set ad-account-id act_123` (as Ads Manager shows it) is stored as `123`. Tests: Task 1 (`TestParseMajor`, `TestNormalizeAccountID`), Task 5 (`TestConfigSetCapComma`, `TestConfigSetAndUnset`).
5. **Secrets in transport errors**: a connection that fails mid-request must not print the request URL, whose query carries `appsecret_proof`, `debug_token`'s `input_token` or the token exchange's `client_secret`. Test: Task 2 (`TestTransportFailures`).

## File Structure

```
meta-ads-cli/
├── go.mod, go.sum, LICENSE, Makefile, .gitignore, .gitattributes, .goreleaser.yaml
├── .github/workflows/ci.yml, .github/workflows/release.yml
├── README.md, SECURITY.md, docs/agents.md
├── cmd/metaads/main.go                 # hands argv to internal/cli
├── internal/config/config.go           # config file, Resolve, NormalizeAccountID, NormalizeAppID
├── internal/config/money.go            # Meta's currency table, ParseMajor, FormatMinor
├── internal/route/route.go             # Parse: version prefix, act alias, node and edge, per-verb shape
├── internal/api/errors.go              # Error, kinds, Usagef, kindForStatus
├── internal/api/meta.go                # Meta error parsing, kind by code, summary, hints
├── internal/api/retry.go               # classifyTransport, retryAfter, jitter, usageWait
├── internal/api/encode.go              # EncodeForm, EncodeMultipart, File
├── internal/api/proof.go               # Proof (appsecret_proof)
├── internal/api/client.go              # Client.Do: host guard, credentials, retries
├── internal/guard/json.go              # ParseBody: strict JSON (duplicate keys, trailing data)
├── internal/guard/rules.go             # classes, edge table, refusals, owned params, Rules()
├── internal/guard/guard.go             # Check: classification, field checks, caps, gates
├── internal/auth/auth.go               # DebugToken, Exchange
├── internal/cli/app.go                 # app, Execute, configure, prepare, renderError
├── internal/cli/root.go                # command tree root, persistent flags
├── internal/cli/util.go                # flagString, groupRunE, jsonCompact
├── internal/cli/body.go                # --data reading, UTF-8 BOM / UTF-16 handling
├── internal/cli/output.go              # --output file, writeResponse
├── internal/cli/verbs.go               # get, post, delete
├── internal/cli/all.go                 # --all cursor walk
├── internal/cli/catalog.go             # commands [--json]
├── internal/cli/version.go             # version
├── internal/cli/configcmd.go           # config set/unset/path
├── internal/cli/authcmd.go             # auth status [--check], auth refresh
├── internal/cli/initcmd.go             # init
├── internal/cli/prompt.go              # prompter, terminal detection
└── e2e/helpers_test.go, e2e/smoke_test.go, e2e/live_test.go
```

---

### Task 1: Scaffold, config and route

**Files:**
- Create: `go.mod`, `LICENSE`, `Makefile`, `.gitignore`, `.gitattributes`
- Create: `internal/config/config.go`, `internal/config/money.go`, `internal/route/route.go`
- Test: `internal/config/config_test.go`, `internal/config/money_test.go`, `internal/route/route_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `config.DefaultAPIBase = "https://graph.facebook.com"`, `config.DefaultAPIVersion = "v26.0"`.
  - `type config.Cap struct { Minor int64; Currency string }`.
  - `type config.Config struct { AccessToken, AppSecret, AppID, AdAccountID, Currency string; DailyCap, LifetimeCap *Cap; ReadOnly bool; TokenExpiresAt string }`.
  - `config.Path() (string, error)`, `config.Load() (Config, error)`, `config.Save(Config) error`.
  - `type config.Resolved struct { AccessToken, TokenFrom, AppSecret, AppID, AdAccountID, Currency string; DailyCap, LifetimeCap *Cap; ReadOnly bool; TokenExpiresAt, APIBase string; AllowCustomBase bool; FromEnv map[string]bool }`; `TokenFrom` is `"env"`, `"config"` or `""`.
  - `config.Resolve(getenv func(string) string) (Resolved, error)`.
  - `config.NormalizeAccountID(string) (string, error)` (returns digits), `config.NormalizeAppID(string) (string, error)`.
  - `config.NormalizeCurrency(string) (string, error)`, `config.Offset(string) (int64, bool)`, `config.ParseMajor(amount, currency string) (int64, error)`, `config.FormatMinor(minor int64, currency string) string`.
  - `type route.Route struct { Version, Node, Edge string }` with `Path() string` and `IsAccount() bool`; `route.Parse(path, verb, account, defaultVersion string) (Route, error)`.

- [ ] **Step 1: Scaffold the repository files**

`go.mod`:

```
module github.com/wir-drei-digital/meta-ads-cli

go 1.26
```

`LICENSE`: copy `/Users/daniel/Development/google-ads-cli/LICENSE` verbatim (MIT, "Copyright (c) 2026 wir drei digital").

`Makefile`:

```make
.PHONY: build test check

build:
	go build -o bin/metaads ./cmd/metaads

test:
	go test ./...

check:
	go vet ./...
	go test ./...
```

`.gitignore`:

```
bin/
dist/
*.test
coverage.out

# `go build ./cmd/metaads` drops the binary here; never commit it.
/metaads
/metaads.exe

# Superpowers working artifacts: process scratch, not repo content.
.superpowers/
```

`.gitattributes`:

```
# Text files are LF in the repository and on checkout, on every platform.
* text=auto eol=lf
```

- [ ] **Step 2: Write the failing config tests**

`internal/config/config_test.go`:

```go
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
		DailyCap: &Cap{Minor: 3000, Currency: "CHF"}, LifetimeCap: &Cap{Minor: 30000, Currency: "CHF"},
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
	mustSave(t, Config{Currency: "CHF", DailyCap: &Cap{Minor: 3000, Currency: "CHF"}})
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
```

`internal/config/money_test.go`:

```go
package config

import (
	"sort"
	"strings"
	"testing"
)

func TestOffsetTable(t *testing.T) {
	if len(offsets) != 66 {
		t.Fatalf("Meta's table lists 66 currencies, got %d", len(offsets))
	}
	var one []string
	for c, o := range offsets {
		if o == 1 {
			one = append(one, c)
		} else if o != 100 {
			t.Errorf("%s has offset %d", c, o)
		}
	}
	sort.Strings(one)
	if got := strings.Join(one, ","); got != "CLP,COP,CRC,HUF,IDR,ISK,JPY,KRW,PYG,TWD,VND" {
		t.Fatalf("offset-1 currencies: %s", got)
	}
	if o, ok := Offset("BHD"); !ok || o != 100 {
		t.Fatalf("BHD: %d %v (Meta uses 100, not ISO's 1000)", o, ok)
	}
}

func TestNormalizeCurrency(t *testing.T) {
	if c, err := NormalizeCurrency(" chf "); err != nil || c != "CHF" {
		t.Fatalf("got %q %v", c, err)
	}
	if _, err := NormalizeCurrency("XYZ"); err == nil {
		t.Fatal("an unknown currency was accepted")
	}
}

func TestParseMajor(t *testing.T) {
	for _, c := range []struct {
		in, cur string
		want    int64
		err     string
	}{
		{"30", "CHF", 3000, ""},
		{" 30 ", "CHF", 3000, ""},
		{"29.5", "CHF", 2950, ""},
		{"29.50", "CHF", 2950, ""},
		{"0.01", "CHF", 1, ""},
		{"3000", "JPY", 3000, ""},
		{"29,50", "CHF", 0, "dot"},
		{"1,000", "CHF", 0, "dot"},
		{"29.505", "CHF", 0, "two decimal"},
		{"30.", "CHF", 0, "such as"},
		{"3e3", "CHF", 0, "such as"},
		{"-5", "CHF", 0, "such as"},
		{"0", "CHF", 0, "greater than 0"},
		{"30.5", "JPY", 0, "whole amount"},
		{"30", "XXX", 0, "unknown currency"},
		{"99999999999", "CHF", 0, "too large"},
	} {
		got, err := ParseMajor(c.in, c.cur)
		if c.err == "" {
			if err != nil || got != c.want {
				t.Errorf("%q %s: got %d %v, want %d", c.in, c.cur, got, err, c.want)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%q %s: want an error containing %q, got %d %v", c.in, c.cur, c.err, got, err)
		}
	}
}

func TestFormatMinor(t *testing.T) {
	for _, c := range []struct {
		minor int64
		cur   string
		want  string
	}{
		{3000, "CHF", "30.00 CHF"},
		{5, "CHF", "0.05 CHF"},
		{30050, "EUR", "300.50 EUR"},
		{5000, "JPY", "5000 JPY"},
	} {
		if got := FormatMinor(c.minor, c.cur); got != c.want {
			t.Errorf("%d %s: got %q, want %q", c.minor, c.cur, got, c.want)
		}
	}
}
```

- [ ] **Step 3: Run the config tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL (build errors: `userConfigDir`, `Save`, `offsets` and the rest are undefined).

- [ ] **Step 4: Implement the config package**

`internal/config/config.go`:

```go
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

// Cap is a budget cap in the minor unit of the currency it was entered for.
// A cap entered for another currency than the account's refuses every budget
// it would check, until a person sets it again.
type Cap struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
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
```

`internal/config/money.go`:

```go
package config

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// offsets is Meta's currency table: every ad account currency and how many
// minor units make one major unit. Budgets are written in the minor unit.
// Source: Marketing API, "Currency codes"
// (developers.facebook.com/documentation/ads-commerce/marketing-api/currencies),
// read on 2026-09-25. Meta's offsets differ from ISO 4217 for some currencies
// (HUF, ISK and TWD have offset 1, BHD has 100), so never derive them from ISO.
var offsets = map[string]int64{
	"AED": 100, "ARS": 100, "AUD": 100, "BDT": 100, "BGN": 100, "BHD": 100, "BOB": 100, "BRL": 100,
	"CAD": 100, "CHF": 100, "CLP": 1, "CNY": 100, "COP": 1, "CRC": 1, "CZK": 100, "DKK": 100,
	"DZD": 100, "EGP": 100, "EUR": 100, "FBZ": 100, "GBP": 100, "GTQ": 100, "HKD": 100, "HNL": 100,
	"HRK": 100, "HUF": 1, "IDR": 1, "ILS": 100, "INR": 100, "ISK": 1, "JOD": 100, "JPY": 1,
	"KES": 100, "KRW": 1, "LTL": 100, "LVL": 100, "MOP": 100, "MXN": 100, "MYR": 100, "NGN": 100,
	"NIO": 100, "NOK": 100, "NZD": 100, "PEN": 100, "PHP": 100, "PKR": 100, "PLN": 100, "PYG": 1,
	"QAR": 100, "RON": 100, "RSD": 100, "RUB": 100, "SAR": 100, "SEK": 100, "SGD": 100, "SKK": 100,
	"THB": 100, "TRY": 100, "TWD": 1, "UAH": 100, "USD": 100, "UYU": 100, "VEF": 100, "VES": 100,
	"VND": 1, "ZAR": 100,
}

// NormalizeCurrency upper-cases a currency code and checks it against
// Meta's table.
func NormalizeCurrency(s string) (string, error) {
	c := strings.ToUpper(strings.TrimSpace(s))
	if _, ok := offsets[c]; !ok {
		return "", fmt.Errorf("%q is not an ad account currency Meta supports (for example CHF, EUR, USD)", s)
	}
	return c, nil
}

// Offset returns how many minor units make one major unit: 100 for CHF, 1
// for JPY.
func Offset(currency string) (int64, bool) {
	o, ok := offsets[currency]
	return o, ok
}

var majorPattern = regexp.MustCompile(`^([0-9]+)(?:\.([0-9]+))?$`)

// maxMajor bounds an amount far below int64 overflow.
const maxMajor = 1_000_000_000

// ParseMajor reads an amount in major units ("30", "29.50") for currency and
// returns it in the minor unit Meta uses (3000, 2950). Only a dot separates
// decimals, with at most as many places as the currency has (two for CHF,
// none for JPY); zero, negatives, exponents and thousands separators are
// refused.
func ParseMajor(s, currency string) (int64, error) {
	off, ok := offsets[currency]
	if !ok {
		return 0, fmt.Errorf("unknown currency %q; set the currency first", currency)
	}
	s = strings.TrimSpace(s)
	if strings.Contains(s, ",") {
		return 0, fmt.Errorf("use a dot as the decimal separator and no thousands separator (29.50, not 29,50), got %q", s)
	}
	places := 0
	if off == 100 {
		places = 2
	}
	m := majorPattern.FindStringSubmatch(s)
	switch {
	case m == nil:
		if places == 0 {
			return 0, fmt.Errorf("want a whole amount in %s such as 3000, got %q", currency, s)
		}
		return 0, fmt.Errorf("want an amount in %s such as 30 or 29.50, got %q", currency, s)
	case len(m[2]) > places && places == 0:
		return 0, fmt.Errorf("want a whole amount in %s such as 3000, got %q", currency, s)
	case len(m[2]) > places:
		return 0, fmt.Errorf("want an amount in %s with at most two decimal places, got %q", currency, s)
	}
	units, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || units > maxMajor {
		return 0, fmt.Errorf("amount %q is too large", s)
	}
	var frac int64
	if places == 2 {
		frac, _ = strconv.ParseInt(m[2]+strings.Repeat("0", 2-len(m[2])), 10, 64)
	}
	minor := units*off + frac
	if minor == 0 {
		return 0, errors.New("amount must be greater than 0")
	}
	return minor, nil
}

// FormatMinor renders a minor-unit amount in major units with the currency:
// 3000 CHF becomes "30.00 CHF", 5000 JPY "5000 JPY".
func FormatMinor(minor int64, currency string) string {
	if offsets[currency] == 100 {
		return fmt.Sprintf("%d.%02d %s", minor/100, minor%100, currency)
	}
	return fmt.Sprintf("%d %s", minor, currency)
}
```

- [ ] **Step 5: Run the config tests to verify they pass**

Run: `go test ./internal/config/`
Expected: PASS.

- [ ] **Step 6: Write the failing route test**

`internal/route/route_test.go`:

```go
package route

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, c := range []struct {
		path, verb, account string
		want                string // Path(), or "" when an error is expected
		err                 string
	}{
		{"act/campaigns", "GET", "1", "v26.0/act_1/campaigns", ""},
		{"/act_1/campaigns", "GET", "1", "v26.0/act_1/campaigns", ""},
		{"v24.0/act/insights", "GET", "1", "v24.0/act_1/insights", ""},
		{"me", "GET", "", "v26.0/me", ""},
		{"me/adaccounts", "GET", "", "v26.0/me/adaccounts", ""},
		{"act_5/campaigns", "GET", "1", "v26.0/act_5/campaigns", ""},
		{"act/campaigns", "GET", "", "", "no ad account is configured"},
		{"act/campaigns?fields=name", "GET", "1", "", "--fields"},
		{"act_1/camp%61igns", "GET", "1", "", "escape"},
		{"act_1#x", "GET", "1", "", "fragment"},
		{"act_1/ campaigns", "GET", "1", "", "whitespace"},
		{"act_1/..", "GET", "1", "", "invalid segment"},
		{"act_1/", "GET", "1", "", "invalid segment"},
		{"act_1//campaigns", "GET", "1", "", "<node>/<edge>"},
		{"a/b/c", "GET", "1", "", "<node>/<edge>"},
		{"v26.0", "GET", "1", "", "<node>/<edge>"},
		{"", "GET", "1", "", "a path is required"},
		{"act/campaigns", "POST", "1", "v26.0/act_1/campaigns", ""},
		{"act_1/adimages", "POST", "1", "v26.0/act_1/adimages", ""},
		{"act_2/campaigns", "POST", "1", "", "configured ad account act_1 only"},
		{"act_1/campaigns", "POST", "", "", "needs a configured ad account"},
		{"120330000000", "POST", "", "v26.0/120330000000", ""},
		{"120330000000/copies", "POST", "1", "v26.0/120330000000/copies", ""},
		{"me/adaccounts", "POST", "1", "", "needs act, act_<id> or an object ID"},
		{"120330000000", "DELETE", "1", "v26.0/120330000000", ""},
		{"act/adimages", "DELETE", "1", "v26.0/act_1/adimages", ""},
	} {
		r, err := Parse(c.path, c.verb, c.account, "v26.0")
		if c.err == "" {
			if err != nil || r.Path() != c.want {
				t.Errorf("%s %q: got %q %v, want %q", c.verb, c.path, r.Path(), err, c.want)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s %q: want an error containing %q, got %q %v", c.verb, c.path, c.err, r.Path(), err)
		}
	}
}

func TestRouteParts(t *testing.T) {
	r, err := Parse("act/campaigns", "POST", "7", "v26.0")
	if err != nil || r.Version != "v26.0" || r.Node != "act_7" || r.Edge != "campaigns" || !r.IsAccount() {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = Parse("123", "POST", "7", "v26.0")
	if err != nil || r.Node != "123" || r.Edge != "" || r.IsAccount() {
		t.Fatalf("%+v %v", r, err)
	}
}
```

- [ ] **Step 7: Run the route test to verify it fails**

Run: `go test ./internal/route/`
Expected: FAIL (build error: `Parse` undefined).

- [ ] **Step 8: Implement the route package**

`internal/route/route.go`:

```go
// Package route parses the Graph API paths the CLI accepts:
// [vNN.N/]<node>[/<edge>], where the node "act" stands for the configured ad
// account. Everything the CLI sends besides the path comes from flags, so a
// path carries no query string, fragment, escape or whitespace: the guard
// must see every field that is sent.
package route

import (
	"fmt"
	"regexp"
	"strings"
)

// Route is a parsed Graph API path.
type Route struct {
	Version string // "v26.0"
	Node    string // "act_123", "120330000000", "me", ...
	Edge    string // "" when the request addresses the node itself
}

// Path is the request path relative to the API base: "v26.0/act_123/campaigns".
func (r Route) Path() string {
	p := r.Version + "/" + r.Node
	if r.Edge != "" {
		p += "/" + r.Edge
	}
	return p
}

// IsAccount reports whether the node is an ad account (act_<digits>).
func (r Route) IsAccount() bool { return accountRe.MatchString(r.Node) }

var (
	versionRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+$`)
	segmentRe = regexp.MustCompile(`^[A-Za-z0-9_:-]+$`)
	accountRe = regexp.MustCompile(`^act_[0-9]+$`)
	objectRe  = regexp.MustCompile(`^[0-9]+$`)
)

// Parse reads path for verb (GET, POST or DELETE). account is the configured
// ad account's digits, "" when none is configured; defaultVersion is used
// when the path does not start with its own version.
//
// GET accepts any node. POST and DELETE need an ad account or an object ID
// as the node, and an ad account must be the configured one: writes never
// reach another account, whose currency the budget caps know nothing about.
func Parse(path, verb, account, defaultVersion string) (Route, error) {
	p := strings.TrimPrefix(strings.TrimSpace(path), "/")
	if p == "" {
		return Route{}, fmt.Errorf("a path is required, such as act/campaigns")
	}
	if strings.ContainsAny(p, "?#%\\ \t\r\n") {
		return Route{}, fmt.Errorf("path %q contains a query string, fragment, escape or whitespace; pass fields with --fields, --param or --data", path)
	}
	segs := strings.Split(p, "/")
	r := Route{Version: defaultVersion}
	if versionRe.MatchString(segs[0]) {
		r.Version, segs = segs[0], segs[1:]
	}
	if len(segs) == 0 || len(segs) > 2 {
		return Route{}, fmt.Errorf("path %q must be <node>/<edge> or <node>, optionally after a version such as v26.0", path)
	}
	for _, s := range segs {
		if !segmentRe.MatchString(s) {
			return Route{}, fmt.Errorf("path %q has an empty or invalid segment %q", path, s)
		}
	}
	r.Node = segs[0]
	if len(segs) == 2 {
		r.Edge = segs[1]
	}
	if r.Node == "act" {
		if account == "" {
			return Route{}, fmt.Errorf("the path uses act, but no ad account is configured; run `metaads config set ad-account-id <id>` or set META_ADS_AD_ACCOUNT_ID")
		}
		r.Node = "act_" + account
	}
	if verb == "GET" {
		return r, nil
	}
	switch {
	case accountRe.MatchString(r.Node):
		if account == "" {
			return Route{}, fmt.Errorf("%s on an ad account needs a configured ad account; run `metaads config set ad-account-id <id>`", verb)
		}
		if r.Node != "act_"+account {
			return Route{}, fmt.Errorf("%s goes to the configured ad account act_%s only, not %s", verb, account, r.Node)
		}
	case objectRe.MatchString(r.Node):
	default:
		return Route{}, fmt.Errorf("%s needs act, act_<id> or an object ID as the node, got %q", verb, r.Node)
	}
	return r, nil
}
```

- [ ] **Step 9: Run all tests and vet**

Run: `go vet ./... && go test ./...`
Expected: PASS for `internal/config` and `internal/route`.

- [ ] **Step 10: Commit**

```bash
git add go.mod LICENSE Makefile .gitignore .gitattributes internal/config internal/route
git commit -m "Add scaffold, config with Meta's currency table, and Graph path parsing

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: API client

**Files:**
- Create: `internal/api/errors.go`, `internal/api/meta.go`, `internal/api/retry.go`, `internal/api/encode.go`, `internal/api/proof.go`, `internal/api/client.go`
- Test: `internal/api/encode_test.go`, `internal/api/meta_test.go`, `internal/api/client_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (the host name `graph.facebook.com` is a literal here).
- Produces:
  - Kinds `api.KindAuth`, `KindForbidden`, `KindNotFound`, `KindValidation`, `KindRateLimited`, `KindServer`, `KindTransport`, `KindOutcomeUnknown`, `KindIncomplete`, `KindUsage`, `KindOutputFailed`.
  - `type api.Error struct { Kind string; Message string; Status int; Details any; RequestID string }` (JSON keys `kind`, `error`, `status`, `details`, `request_id`), `api.Usagef(format, ...any) *Error`.
  - `const api.ClassRead = "read"`.
  - `type api.Client struct { BaseURL, Token, AppSecret string; ReadOnly, AllowCustomBase bool; Timeout, RetryBudget time.Duration; MaxAttempts int; Verbose io.Writer; HTTP *http.Client; Sleep func(time.Duration) }`.
  - `type api.Request struct { Method, Path string; Query, Form url.Values; Multipart []byte; ContentType, Class string; ValidateOnly bool; Bearer string; NoAuth, NoProof bool }`.
  - `type api.Response struct { Status int; Header http.Header; Body []byte }`.
  - `(*api.Client).Do(ctx, Request) (*Response, error)`; the error is always `*api.Error`.
  - `api.EncodeForm(map[string]any) (url.Values, error)`, `type api.File struct { Field, Path string }`, `api.EncodeMultipart(url.Values, []File) ([]byte, string, error)`.
  - `api.Proof(token, secret string) string`.

- [ ] **Step 1: Write the failing encoding and error tests**

`internal/api/encode_test.go`:

```go
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestEncodeForm(t *testing.T) {
	obj := map[string]any{
		"s": "x <b>", "n": json.Number("3000"), "b": true, "z": nil, "a": []any{},
		"o": map[string]any{"geo_locations": map[string]any{"countries": []any{"CH"}}, "html": "<a&b>"},
	}
	form, err := EncodeForm(obj)
	if err != nil {
		t.Fatal(err)
	}
	want := url.Values{"s": {"x <b>"}, "n": {"3000"}, "b": {"true"}, "z": {"null"}, "a": {"[]"},
		"o": {`{"geo_locations":{"countries":["CH"]},"html":"<a&b>"}`}}
	for k, v := range want {
		if form.Get(k) != v[0] {
			t.Errorf("%s = %q, want %q", k, form.Get(k), v[0])
		}
	}
	if len(form) != len(want) {
		t.Errorf("fields %v", form)
	}
}

func TestEncodeMultipart(t *testing.T) {
	p := filepath.Join(t.TempDir(), "motiv.png")
	if err := os.WriteFile(p, []byte("PNGDATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	body, ct, err := EncodeMultipart(url.Values{"name": {"img"}}, []File{{Field: "filename", Path: p}})
	if err != nil {
		t.Fatal(err)
	}
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil || mt != "multipart/form-data" {
		t.Fatalf("content type %q %v", ct, err)
	}
	form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	if form.Value["name"][0] != "img" || len(form.File["filename"]) != 1 {
		t.Fatalf("form %+v", form)
	}
	fh := form.File["filename"][0]
	if fh.Filename != "motiv.png" || fh.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("file header %+v", fh.Header)
	}
	f, _ := fh.Open()
	data, _ := io.ReadAll(f)
	if string(data) != "PNGDATA" {
		t.Fatalf("file content %q", data)
	}
	if _, _, err := EncodeMultipart(nil, []File{{Field: "filename", Path: filepath.Join(t.TempDir(), "missing.png")}}); err == nil {
		t.Fatal("a missing file was accepted")
	}
}

func TestProof(t *testing.T) {
	// printf '%s' token | openssl dgst -sha256 -hmac secret
	if got := Proof("token", "secret"); got != "e941110e3d2bfe82621f0e3e1434730d7305d106c5f68c87165d0b27a4611a4a" {
		t.Fatalf("proof %s", got)
	}
}
```

`internal/api/meta_test.go`:

```go
package api

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMetaKinds(t *testing.T) {
	for _, c := range []struct {
		e      metaError
		status int
		want   string
	}{
		{metaError{Code: 190}, 400, KindAuth},
		{metaError{Code: 10}, 400, KindForbidden},
		{metaError{Code: 200}, 400, KindForbidden},
		{metaError{Code: 294}, 400, KindForbidden},
		{metaError{Code: 368}, 400, KindForbidden},
		{metaError{Code: 100, Subcode: 33}, 400, KindNotFound},
		{metaError{Code: 100}, 400, KindValidation},
		{metaError{Code: 2500}, 400, KindValidation},
		{metaError{Code: 4}, 400, KindRateLimited},
		{metaError{Code: 17, Subcode: 2446079}, 400, KindRateLimited},
		{metaError{Code: 613}, 400, KindRateLimited},
		{metaError{Code: 80004}, 400, KindRateLimited},
		{metaError{Code: 2}, 400, KindServer},
		{metaError{Code: 3, IsTransient: true}, 400, KindServer},
		{metaError{}, 500, KindServer},
		{metaError{}, 404, KindNotFound},
		{metaError{}, 401, KindAuth},
		{metaError{Code: 3}, 400, KindValidation},
	} {
		if got := c.e.kind(c.status); got != c.want {
			t.Errorf("code %d/%d status %d: got %s, want %s", c.e.Code, c.e.Subcode, c.status, got, c.want)
		}
	}
}

func TestBlameFields(t *testing.T) {
	obj := metaError{ErrorData: []byte(`{"blame_field_specs":[["daily_budget"],["targeting","geo_locations"]]}`)}
	str := metaError{ErrorData: []byte(`"{\"blame_field_specs\":[[\"daily_budget\"]]}"`)}
	if got := strings.Join(obj.blameFields(), ","); got != "daily_budget,targeting.geo_locations" {
		t.Fatalf("object form: %s", got)
	}
	if got := strings.Join(str.blameFields(), ","); got != "daily_budget" {
		t.Fatalf("string form: %s", got)
	}
	if got := (metaError{}).blameFields(); got != nil {
		t.Fatalf("no error_data: %v", got)
	}
}

func TestUsageWait(t *testing.T) {
	h := http.Header{}
	if usageWait(h) != 0 {
		t.Fatal("no headers must mean no wait")
	}
	h.Set("X-Ad-Account-Usage", `{"acc_id_util_pct":100,"reset_time_duration":30,"ads_api_access_tier":"standard_access"}`)
	if got := usageWait(h); got != 30*time.Second {
		t.Fatalf("account usage: %s", got)
	}
	h.Set("X-Business-Use-Case-Usage", `{"123":[{"type":"ads_management","call_count":100,"estimated_time_to_regain_access":2},{"type":"ads_insights","estimated_time_to_regain_access":5}]}`)
	if got := usageWait(h); got != 5*time.Minute {
		t.Fatalf("business use case usage wins and takes the longest wait: %s", got)
	}
}
```

- [ ] **Step 2: Write the failing client test**

`internal/api/client_test.go`:

```go
package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type hit struct {
	Method, Path, Auth, ContentType string
	Query                           url.Values
	Body                            []byte
}

type recorder struct {
	mu   sync.Mutex
	hits []hit
}

func (r *recorder) all() []hit {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]hit(nil), r.hits...)
}

// server records every request and answers with reply(n), n counting from 1.
func server(t *testing.T, reply func(w http.ResponseWriter, n int)) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.hits = append(rec.hits, hit{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.URL.Query(), b})
		n := len(rec.hits)
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		reply(w, n)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func ok(body string) func(http.ResponseWriter, int) {
	return func(w http.ResponseWriter, _ int) { w.Write([]byte(body)) }
}

func fail(status int, body string, headers map[string]string) func(http.ResponseWriter, int) {
	return func(w http.ResponseWriter, _ int) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}
}

func newClient(base string, sleeps *[]time.Duration) *Client {
	return &Client{BaseURL: base, Token: "tok", AllowCustomBase: true, Sleep: func(d time.Duration) {
		if sleeps != nil {
			*sleeps = append(*sleeps, d)
		}
	}}
}

var ctx = context.Background()

func apiErr(t *testing.T, err error) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want *Error, got %T %v", err, err)
	}
	return e
}

func TestBearerProofAndNoTokenInURL(t *testing.T) {
	srv, rec := server(t, ok(`{"id":"1"}`))
	c := newClient(srv.URL, nil)
	c.Token, c.AppSecret = "token", "secret"
	if _, err := c.Do(ctx, Request{Method: "GET", Path: "v26.0/me", Query: url.Values{"fields": {"id"}}, Class: ClassRead}); err != nil {
		t.Fatal(err)
	}
	h := rec.all()[0]
	if h.Auth != "Bearer token" || h.Path != "/v26.0/me" || h.Query.Get("fields") != "id" || h.Query.Has("access_token") ||
		h.Query.Get("appsecret_proof") != "e941110e3d2bfe82621f0e3e1434730d7305d106c5f68c87165d0b27a4611a4a" {
		t.Fatalf("%+v", h)
	}
}

func TestNoProofWithoutSecretAndOverrides(t *testing.T) {
	srv, rec := server(t, ok(`{}`))
	c := newClient(srv.URL, nil)
	c.Do(ctx, Request{Method: "GET", Path: "v26.0/me", Class: ClassRead})
	c.AppSecret = "secret"
	c.Do(ctx, Request{Method: "GET", Path: "v26.0/debug_token", Class: ClassRead, Bearer: "42|secret", NoProof: true})
	c.Token = ""
	c.Do(ctx, Request{Method: "GET", Path: "v26.0/oauth/access_token", Class: ClassRead, NoAuth: true})
	hits := rec.all()
	if len(hits) != 3 || hits[0].Query.Has("appsecret_proof") || hits[1].Auth != "Bearer 42|secret" ||
		hits[1].Query.Has("appsecret_proof") || hits[2].Auth != "" || hits[2].Query.Has("appsecret_proof") {
		t.Fatalf("%+v", hits)
	}
}

func TestFormAndMultipartBodies(t *testing.T) {
	srv, rec := server(t, ok(`{"id":"1"}`))
	c := newClient(srv.URL, nil)
	form := url.Values{"name": {"x"}, "special_ad_categories": {"[]"}}
	if _, err := c.Do(ctx, Request{Method: "POST", Path: "v26.0/act_1/campaigns", Form: form, Class: "write"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(ctx, Request{Method: "DELETE", Path: "v26.0/act_1/adimages", Form: url.Values{"hash": {"abc"}}, Class: "delete"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(ctx, Request{Method: "POST", Path: "v26.0/act_1/adimages", Multipart: []byte("--b--\r\n"), ContentType: "multipart/form-data; boundary=b", Class: "write"}); err != nil {
		t.Fatal(err)
	}
	hits := rec.all()
	got, _ := url.ParseQuery(string(hits[0].Body))
	if hits[0].ContentType != "application/x-www-form-urlencoded" || got.Get("special_ad_categories") != "[]" || got.Get("name") != "x" {
		t.Fatalf("POST form: %+v %v", hits[0], got)
	}
	got, _ = url.ParseQuery(string(hits[1].Body))
	if hits[1].Method != "DELETE" || got.Get("hash") != "abc" {
		t.Fatalf("DELETE form: %+v", hits[1])
	}
	if hits[2].ContentType != "multipart/form-data; boundary=b" || !bytes.Equal(hits[2].Body, []byte("--b--\r\n")) {
		t.Fatalf("multipart: %+v", hits[2])
	}
}

func TestLocalRefusals(t *testing.T) {
	read := Request{Method: "GET", Path: "v26.0/me", Class: ClassRead}
	for _, c := range []struct {
		name string
		c    Client
		r    Request
		want string
	}{
		{"custom host", Client{BaseURL: "https://example.com", Token: "t"}, read, "refusing to send credentials to example.com"},
		{"plain http", Client{BaseURL: "http://example.com", Token: "t", AllowCustomBase: true}, read, "non-HTTPS"},
		{"no token", Client{BaseURL: "https://graph.facebook.com"}, read, "no access token"},
		{"post without class", Client{BaseURL: "https://graph.facebook.com", Token: "t"}, Request{Method: "POST", Path: "v26.0/act_1/campaigns"}, "no risk class"},
		{"credential in query", Client{BaseURL: "https://graph.facebook.com", Token: "t"}, Request{Method: "GET", Path: "v26.0/me", Query: url.Values{"access_token": {"x"}}, Class: ClassRead}, "carries a credential"},
		{"credential in form", Client{BaseURL: "https://graph.facebook.com", Token: "t"}, Request{Method: "POST", Path: "v26.0/1", Form: url.Values{"appsecret_proof": {"x"}}, Class: "write"}, "carries a credential"},
		{"read-only write", Client{BaseURL: "https://graph.facebook.com", Token: "t", ReadOnly: true}, Request{Method: "POST", Path: "v26.0/1", Class: "write"}, "read-only mode"},
		{"get with body", Client{BaseURL: "https://graph.facebook.com", Token: "t"}, Request{Method: "GET", Path: "v26.0/me", Form: url.Values{"a": {"b"}}, Class: ClassRead}, "body"},
		{"patch", Client{BaseURL: "https://graph.facebook.com", Token: "t"}, Request{Method: "PATCH", Path: "v26.0/1", Class: "write"}, "unsupported method"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.c.Do(ctx, c.r)
			e := apiErr(t, err)
			if e.Kind != KindUsage || !strings.Contains(e.Message, c.want) {
				t.Fatalf("got %s %q, want usage containing %q", e.Kind, e.Message, c.want)
			}
		})
	}
}

func TestReadOnlyAllowsValidateOnly(t *testing.T) {
	srv, rec := server(t, ok(`{"success":true}`))
	c := newClient(srv.URL, nil)
	c.ReadOnly = true
	if _, err := c.Do(ctx, Request{Method: "POST", Path: "v26.0/act_1/campaigns", Class: "spend", ValidateOnly: true}); err != nil {
		t.Fatal(err)
	}
	if len(rec.all()) != 1 {
		t.Fatal("the validate-only request was not sent")
	}
}

func TestErrorMapping(t *testing.T) {
	tier := `{"123":[{"type":"ads_management","call_count":100,"estimated_time_to_regain_access":10,"ads_api_access_tier":"development_access"}]}`
	for _, c := range []struct {
		name    string
		status  int
		body    string
		headers map[string]string
		kind    string
		rid     string
		want    []string
	}{
		{"token", 400, `{"error":{"message":"Invalid OAuth access token.","type":"OAuthException","code":190,"fbtrace_id":"T1"}}`, nil,
			KindAuth, "T1", []string{"HTTP 400: 190: Invalid OAuth access token.", "auth status --check"}},
		{"permission", 400, `{"error":{"message":"(#200) Permissions error","code":200}}`, nil,
			KindForbidden, "", []string{"ads_management"}},
		{"missing object", 400, `{"error":{"message":"Unsupported post request.","code":100,"error_subcode":33}}`, nil,
			KindNotFound, "", []string{"100/33", "does not exist"}},
		{"budget level", 400, `{"error":{"message":"Invalid parameter","code":100,"error_subcode":1885621,"error_user_title":"Budget conflict","error_user_msg":"Set the budget on one level.","error_data":"{\"blame_field_specs\":[[\"daily_budget\"]]}"}}`, nil,
			KindValidation, "", []string{"100/1885621: Budget conflict: Set the budget on one level. (fields: daily_budget)", "keep it on one level"}},
		{"special ad categories", 400, `{"error":{"message":"Invalid parameter","code":100,"error_data":{"blame_field_specs":[["special_ad_categories"]]}}}`, nil,
			KindValidation, "", []string{"send []"}},
		{"transient", 500, `{"error":{"message":"Service temporarily unavailable","code":2,"is_transient":true}}`, nil,
			KindServer, "", []string{"HTTP 500: 2: Service temporarily unavailable"}},
		{"html gateway", 502, `<html>bad gateway</html>`, map[string]string{"x-fb-trace-id": "H1"},
			KindServer, "H1", []string{"HTTP 502"}},
		{"rate limit beyond budget", 400, `{"error":{"message":"User request limit reached","code":17,"error_subcode":2446079}}`, map[string]string{"X-Business-Use-Case-Usage": tier},
			KindRateLimited, "", []string{"retry budget", "Limited access tier"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv, rec := server(t, fail(c.status, c.body, c.headers))
			_, err := newClient(srv.URL, nil).Do(ctx, Request{Method: "POST", Path: "v26.0/act_1/campaigns", Class: "write"})
			e := apiErr(t, err)
			if e.Kind != c.kind || e.Status != c.status || e.RequestID != c.rid || len(rec.all()) != 1 {
				t.Fatalf("kind %s status %d rid %q calls %d: %s", e.Kind, e.Status, e.RequestID, len(rec.all()), e.Message)
			}
			for _, w := range c.want {
				if !strings.Contains(e.Message, w) {
					t.Errorf("message lacks %q: %s", w, e.Message)
				}
			}
			if !strings.HasPrefix(e.Message, "POST v26.0/act_1/campaigns: ") {
				t.Errorf("message must start with the method and path: %s", e.Message)
			}
		})
	}
}

func TestErrorDetailsIsMetaErrorObject(t *testing.T) {
	srv, _ := server(t, fail(400, `{"error":{"message":"bad","code":100}}`, nil))
	_, err := newClient(srv.URL, nil).Do(ctx, Request{Method: "GET", Path: "v26.0/me", Class: ClassRead})
	d, ok := apiErr(t, err).Details.(map[string]any)
	if !ok || d["message"] != "bad" {
		t.Fatalf("details %#v", apiErr(t, err).Details)
	}
}

func TestRateLimitWaitsThenSucceeds(t *testing.T) {
	srv, rec := server(t, func(w http.ResponseWriter, n int) {
		if n == 1 {
			w.Header().Set("X-Ad-Account-Usage", `{"acc_id_util_pct":100,"reset_time_duration":2}`)
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"message":"limit","code":80004}}`))
			return
		}
		w.Write([]byte(`{"id":"1"}`))
	})
	var sleeps []time.Duration
	c := newClient(srv.URL, &sleeps)
	if _, err := c.Do(ctx, Request{Method: "POST", Path: "v26.0/act_1/campaigns", Class: "write"}); err != nil {
		t.Fatal(err)
	}
	if len(rec.all()) != 2 || len(sleeps) != 1 || sleeps[0] < 2*time.Second || sleeps[0] >= 2*time.Second+500*time.Millisecond {
		t.Fatalf("calls %d sleeps %v", len(rec.all()), sleeps)
	}
}

func TestServerErrorsRetriedForReadsOnly(t *testing.T) {
	flaky := func(w http.ResponseWriter, n int) {
		if n < 3 {
			w.WriteHeader(500)
			w.Write([]byte(`{"error":{"message":"oops","code":1}}`))
			return
		}
		w.Write([]byte(`{"id":"1"}`))
	}
	srv, rec := server(t, flaky)
	if _, err := newClient(srv.URL, nil).Do(ctx, Request{Method: "GET", Path: "v26.0/me", Class: ClassRead}); err != nil || len(rec.all()) != 3 {
		t.Fatalf("read: %v after %d calls", err, len(rec.all()))
	}
	srv, rec = server(t, flaky)
	_, err := newClient(srv.URL, nil).Do(ctx, Request{Method: "POST", Path: "v26.0/1", Class: "write"})
	if apiErr(t, err).Kind != KindServer || len(rec.all()) != 1 {
		t.Fatalf("write was retried: %d calls", len(rec.all()))
	}
}

func TestTransportFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	c := newClient(srv.URL, nil)
	c.AppSecret = "the-app-secret"
	_, err := c.Do(ctx, Request{Method: "POST", Path: "v26.0/act_1/campaigns", Form: url.Values{"name": {"x"}}, Class: "write"})
	e := apiErr(t, err)
	if e.Kind != KindOutcomeUnknown || !strings.HasPrefix(e.Message, "POST v26.0/act_1/campaigns: ") {
		t.Fatalf("write: %s %s", e.Kind, e.Message)
	}
	_, err = c.Do(ctx, Request{Method: "GET", Path: "v26.0/debug_token", Query: url.Values{"input_token": {"SECRET-INPUT"}}, Class: ClassRead})
	e = apiErr(t, err)
	if e.Kind != KindTransport {
		t.Fatalf("read: %s %s", e.Kind, e.Message)
	}
	for _, leak := range []string{"appsecret_proof", "SECRET-INPUT", "?"} {
		if strings.Contains(e.Message, leak) {
			t.Fatalf("the message leaks %q: %s", leak, e.Message)
		}
	}
}

func TestRedirectRefused(t *testing.T) {
	srv, _ := server(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Location", "https://evil.example/x")
		w.WriteHeader(302)
	})
	_, err := newClient(srv.URL, nil).Do(ctx, Request{Method: "GET", Path: "v26.0/me", Class: ClassRead})
	if e := apiErr(t, err); e.Kind != KindValidation || !strings.Contains(e.Message, "redirect") {
		t.Fatalf("%s %s", e.Kind, e.Message)
	}
}

func TestVerboseNeverLogsQuery(t *testing.T) {
	srv, _ := server(t, ok(`{}`))
	var log bytes.Buffer
	c := newClient(srv.URL, nil)
	c.AppSecret, c.Verbose = "secret", &log
	c.Do(ctx, Request{Method: "GET", Path: "v26.0/debug_token", Query: url.Values{"input_token": {"SECRET-INPUT"}}, Class: ClassRead})
	if strings.Contains(log.String(), "SECRET-INPUT") || strings.Contains(log.String(), "appsecret_proof") || !strings.Contains(log.String(), "GET v26.0/debug_token") {
		t.Fatalf("verbose log: %q", log.String())
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/api/`
Expected: FAIL (build errors: `EncodeForm`, `metaError`, `Client` and the rest are undefined).

- [ ] **Step 4: Implement errors, Meta error parsing, retry helpers, encoding and the proof**

`internal/api/errors.go`:

```go
package api

import "fmt"

// Error kinds. Every error the CLI reports carries exactly one.
const (
	KindAuth           = "auth"
	KindForbidden      = "forbidden"
	KindNotFound       = "not_found"
	KindValidation     = "validation"
	KindRateLimited    = "rate_limited"
	KindServer         = "server"
	KindTransport      = "transport"
	KindOutcomeUnknown = "outcome_unknown"
	KindIncomplete     = "incomplete"
	KindUsage          = "usage"
	// KindOutputFailed: the call succeeded, but its response could not be
	// written where it was asked to go. The change, if any, has happened.
	KindOutputFailed = "output_failed"
)

// Error is the single error type the CLI reports, shaped for JSON output.
type Error struct {
	Kind      string `json:"kind"`
	Message   string `json:"error"`
	Status    int    `json:"status,omitempty"`
	Details   any    `json:"details"`
	RequestID string `json:"request_id,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// Usagef builds a caller error: something wrong before any request is sent.
func Usagef(format string, a ...any) *Error {
	return &Error{Kind: KindUsage, Message: fmt.Sprintf(format, a...)}
}

// kindForStatus maps an HTTP status when the body carries no Meta error code.
func kindForStatus(status int) string {
	switch {
	case status == 401:
		return KindAuth
	case status == 403:
		return KindForbidden
	case status == 404 || status == 410:
		return KindNotFound
	case status == 429:
		return KindRateLimited
	case status >= 500:
		return KindServer
	default:
		return KindValidation
	}
}
```

`internal/api/meta.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// metaError is Meta's error object: {"error":{"message","type","code",
// "error_subcode","error_user_title","error_user_msg","fbtrace_id",
// "is_transient","error_data"}}.
type metaError struct {
	Message     string          `json:"message"`
	Type        string          `json:"type"`
	Code        int             `json:"code"`
	Subcode     int             `json:"error_subcode"`
	UserTitle   string          `json:"error_user_title"`
	UserMsg     string          `json:"error_user_msg"`
	TraceID     string          `json:"fbtrace_id"`
	IsTransient bool            `json:"is_transient"`
	ErrorData   json.RawMessage `json:"error_data"`
}

func parseMetaError(body []byte) (metaError, bool) {
	var env struct {
		Error *metaError `json:"error"`
	}
	if json.Unmarshal(body, &env) != nil || env.Error == nil {
		return metaError{}, false
	}
	return *env.Error, true
}

// kind maps Meta's error code to an error kind. Meta answers almost every
// error with HTTP 400, so the code decides and the status is the fallback.
func (e metaError) kind(status int) string {
	switch c := e.Code; {
	case c == 190:
		return KindAuth
	case c == 10 || (c >= 200 && c <= 299) || c == 294 || c == 368:
		return KindForbidden
	case c == 100 && e.Subcode == 33:
		return KindNotFound
	case c == 100 || c == 2500:
		return KindValidation
	case c == 4 || c == 17 || c == 32 || c == 341 || c == 613 || (c >= 80000 && c <= 80014):
		return KindRateLimited
	case c == 1 || c == 2 || e.IsTransient:
		return KindServer
	}
	return kindForStatus(status)
}

// blameFields reads error_data.blame_field_specs, which Meta sends as an
// object or as a JSON string holding one: [["daily_budget"]] names daily_budget.
func (e metaError) blameFields() []string {
	raw := []byte(e.ErrorData)
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw = []byte(s)
	}
	var d struct {
		Specs [][]string `json:"blame_field_specs"`
	}
	if json.Unmarshal(raw, &d) != nil {
		return nil
	}
	var out []string
	for _, spec := range d.Specs {
		if len(spec) > 0 {
			out = append(out, strings.Join(spec, "."))
		}
	}
	return out
}

// summary is the part of the message after "HTTP <status>": the code and
// subcode, Meta's text for people when it sent one, and the blamed fields.
func (e metaError) summary() string {
	if e.Code == 0 && e.Message == "" {
		return ""
	}
	s := ": " + strconv.Itoa(e.Code)
	if e.Subcode != 0 {
		s += "/" + strconv.Itoa(e.Subcode)
	}
	switch {
	case e.UserTitle != "" && e.UserMsg != "":
		s += ": " + e.UserTitle + ": " + e.UserMsg
	case e.UserMsg != "":
		s += ": " + e.UserMsg
	case e.Message != "":
		s += ": " + e.Message
	}
	if f := e.blameFields(); len(f) > 0 {
		s += " (fields: " + strings.Join(f, ", ") + ")"
	}
	return s
}

// mentions reports whether the error names field, in its blamed fields or
// its text. Hints only; kinds never depend on text.
func (e metaError) mentions(field string) bool {
	for _, f := range e.blameFields() {
		if strings.HasPrefix(f, field) {
			return true
		}
	}
	return strings.Contains(e.Message+" "+e.UserMsg, field)
}

// hint appends the next step for the errors people hit while setting up.
func hint(e metaError, kind string, h http.Header) string {
	switch {
	case e.Code == 190:
		return "; hint: the access token is expired or invalid; check it with `metaads auth status --check`, and renew a 60-day token with `metaads auth refresh`"
	case e.Code == 100 && e.Subcode == 33:
		return "; hint: the object does not exist, or the system user cannot see it"
	case kind == KindForbidden && e.Code != 368:
		return "; hint: the system user lacks ads_management, or is not assigned to this ad account (or Page) with a task that allows the call; check it in the Business Portfolio under Users, System users"
	case e.Subcode == 1885621:
		return "; hint: a budget is set on both the campaign and its ad sets; keep it on one level"
	case e.Subcode == 1885272 || e.Subcode == 2446307:
		return "; hint: the budget is below Meta's minimum for this optimization; minimums are doubled in some countries, Switzerland among them"
	case e.mentions("special_ad_categories"):
		return "; hint: every campaign create needs special_ad_categories; send [] when none applies"
	case e.mentions("is_adset_budget_sharing_enabled"):
		return "; hint: a campaign without a campaign budget needs is_adset_budget_sharing_enabled (true or false)"
	case e.mentions("dsa_beneficiary") || e.mentions("dsa_payor"):
		return "; hint: ad sets that target the EU need dsa_beneficiary and dsa_payor, or the ad account's defaults for them"
	case kind == KindRateLimited && limitedTier(h):
		return "; hint: the app is on the Limited access tier, which Meta rate-limits heavily per ad account; the Full tier needs App Review"
	}
	return ""
}

// limitedTier reports whether a usage header names the Limited (formerly
// development) access tier.
func limitedTier(h http.Header) bool {
	for _, name := range []string{"X-Business-Use-Case-Usage", "X-Ad-Account-Usage", "X-FB-Ads-Insights-Throttle"} {
		v := strings.ToLower(h.Get(name))
		if strings.Contains(v, "development_access") || strings.Contains(v, "limited") {
			return true
		}
	}
	return false
}
```

`internal/api/retry.go`:

```go
package api

import (
	"encoding/json"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"time"
)

// classifyTransport maps a network failure. A dial or DNS failure never
// reached Meta, and a read-safe call changes nothing, so both are safe to
// retry. Anything else may have been applied: the caller must verify state.
func classifyTransport(err error, readSafe bool) *Error {
	var opErr *net.OpError
	var dnsErr *net.DNSError
	presend := (errors.As(err, &opErr) && opErr.Op == "dial") || errors.As(err, &dnsErr)
	if readSafe || presend {
		return &Error{Kind: KindTransport, Message: err.Error()}
	}
	return &Error{Kind: KindOutcomeUnknown,
		Message: "the request may or may not have been applied: " + err.Error() + "; verify state before retrying"}
}

// retryAfter returns the Retry-After header as a duration, or 0.
func retryAfter(h http.Header) time.Duration {
	if secs, err := strconv.Atoi(h.Get("Retry-After")); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// jitter spreads retries so concurrent callers do not resynchronize.
func jitter() time.Duration { return time.Duration(rand.Int63n(int64(500 * time.Millisecond))) }

// usageWait is how long Meta asks to wait before calling again: the longest
// estimated_time_to_regain_access (minutes) in X-Business-Use-Case-Usage,
// else reset_time_duration (seconds) in X-Ad-Account-Usage. 0 when neither
// header says.
func usageWait(h http.Header) time.Duration {
	var buc map[string][]struct {
		ETA float64 `json:"estimated_time_to_regain_access"`
	}
	var longest time.Duration
	if json.Unmarshal([]byte(h.Get("X-Business-Use-Case-Usage")), &buc) == nil {
		for _, entries := range buc {
			for _, e := range entries {
				if d := time.Duration(e.ETA * float64(time.Minute)); d > longest {
					longest = d
				}
			}
		}
	}
	if longest > 0 {
		return longest
	}
	var acc struct {
		Reset float64 `json:"reset_time_duration"`
	}
	if json.Unmarshal([]byte(h.Get("X-Ad-Account-Usage")), &acc) == nil && acc.Reset > 0 {
		return time.Duration(acc.Reset * float64(time.Second))
	}
	return 0
}
```

`internal/api/encode.go`:

```go
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// EncodeForm turns a JSON object into Graph API form fields, the encoding
// Meta documents for every write: strings as they are, numbers as written,
// booleans and null as their JSON text, arrays and objects as compact JSON.
func EncodeForm(obj map[string]any) (url.Values, error) {
	form := url.Values{}
	for k, v := range obj {
		switch x := v.(type) {
		case string:
			form.Set(k, x)
		case json.Number:
			form.Set(k, x.String())
		case bool:
			form.Set(k, strconv.FormatBool(x))
		case nil:
			form.Set(k, "null")
		default:
			raw, err := compactJSON(x)
			if err != nil {
				return nil, fmt.Errorf("field %s: %v", k, err)
			}
			form.Set(k, string(raw))
		}
	}
	return form, nil
}

// compactJSON marshals v without HTML escaping, so text such as "<b>" in an
// ad goes out as written.
func compactJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// File is one --file part: the form field name and the local path.
type File struct{ Field, Path string }

// maxMultipart caps an upload so a wrong path (a disk image, a log file)
// fails locally instead of after minutes on the wire.
const maxMultipart = 100 << 20

var quoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// EncodeMultipart writes fields and files as multipart/form-data and returns
// the body and its content type with the boundary. Each file part carries
// the content type its extension implies (image/png for .png).
func EncodeMultipart(fields url.Values, files []File) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range fields[k] {
			if err := w.WriteField(k, v); err != nil {
				return nil, "", err
			}
		}
	}
	for _, f := range files {
		src, err := os.Open(f.Path)
		if err != nil {
			return nil, "", err
		}
		info, err := src.Stat()
		if err != nil {
			src.Close()
			return nil, "", err
		}
		// Checked before copying: refusing a 2 GB file should not first pull
		// it into memory.
		if int64(buf.Len())+info.Size() > maxMultipart {
			src.Close()
			return nil, "", fmt.Errorf("the upload exceeds the 100 MB limit")
		}
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
			quoteEscaper.Replace(f.Field), quoteEscaper.Replace(filepath.Base(f.Path))))
		ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(f.Path)))
		if ct == "" {
			ct = "application/octet-stream"
		}
		h.Set("Content-Type", ct)
		part, err := w.CreatePart(h)
		if err != nil {
			src.Close()
			return nil, "", err
		}
		_, err = io.Copy(part, src)
		src.Close()
		if err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}
```

`internal/api/proof.go`:

```go
package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Proof is appsecret_proof: the hex HMAC-SHA256 of the access token, keyed
// with the app secret. With "Require App Secret" switched on in the app,
// Meta refuses every call without it, so a leaked token alone is useless.
func Proof(token, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(token))
	return hex.EncodeToString(m.Sum(nil))
}
```

- [ ] **Step 5: Implement the client**

`internal/api/client.go`:

```go
// Package api is the HTTP client for the Graph API. It owns the host guard,
// the credentials on the wire, the request encoding, the retry policy and
// the error shape.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client performs Graph API requests. BaseURL is required; Timeout,
// RetryBudget and MaxAttempts fall back to defaults.
type Client struct {
	BaseURL                   string
	Token                     string // system user access token; "" means none is configured
	AppSecret                 string // when set, every request carries appsecret_proof
	ReadOnly, AllowCustomBase bool
	Timeout                   time.Duration // per attempt; default 60s
	RetryBudget               time.Duration // total wait on rate limits; default 60s
	MaxAttempts               int           // attempts for read-safe transient failures; default 3
	Verbose                   io.Writer     // nil = silent; never receives a credential or a query string
	HTTP                      *http.Client
	Sleep                     func(time.Duration)
}

// ClassRead is the guard's read class: the only class retried after a
// transient failure, and, with validate-only requests, the only one
// read-only mode lets through.
const ClassRead = "read"

// Request is one API call.
type Request struct {
	Method string     // GET, POST or DELETE
	Path   string     // relative to BaseURL: "v26.0/act_123/campaigns"
	Query  url.Values // query parameters
	Form   url.Values // POST and DELETE: sent as application/x-www-form-urlencoded
	// Multipart, when set, is a complete multipart/form-data body with its
	// ContentType; it replaces Form.
	Multipart   []byte
	ContentType string
	// Class is the guard's risk class; every POST and DELETE must carry one.
	// ValidateOnly says --validate-only was given, so Meta applies nothing.
	Class        string
	ValidateOnly bool
	// Bearer replaces the client's token for this request (the app token for
	// debug_token). NoAuth sends no credential at all (the token exchange,
	// whose credentials are its parameters). NoProof leaves out
	// appsecret_proof.
	Bearer  string
	NoAuth  bool
	NoProof bool
}

// Response is a successful (2xx) response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// readSafe reports whether the call cannot change anything: a read, or a
// validate-only call. Read-only mode allows exactly these, and only these
// are retried after a transient failure.
func (r Request) readSafe() bool { return r.Class == ClassRead || r.ValidateOnly }

// clientParams are the credential parameters only the client sets.
var clientParams = []string{"access_token", "appsecret_proof", "appsecret_time"}

func isLoopback(host string) bool { return host == "localhost" || host == "127.0.0.1" || host == "::1" }

func (c *Client) logf(format string, a ...any) {
	if c.Verbose != nil {
		fmt.Fprintf(c.Verbose, format+"\n", a...)
	}
}

// shape rejects a request that could change something the guard never saw.
func (r Request) shape() *Error {
	switch r.Method {
	case http.MethodGet:
		if r.Form != nil || r.Multipart != nil {
			return Usagef("refusing to send a body with GET %s", r.Path)
		}
	case http.MethodPost, http.MethodDelete:
		if r.Class == "" {
			return Usagef("refusing to send %s %s: no risk class was assigned, so the guard has not checked it", r.Method, r.Path)
		}
		if r.Method == http.MethodDelete && r.Multipart != nil {
			return Usagef("refusing to send a multipart body with DELETE %s", r.Path)
		}
	default:
		return Usagef("unsupported method %q", r.Method)
	}
	return nil
}

// guard rejects a request locally, before anything is sent.
func (c *Client) guard(r Request) *Error {
	if err := r.shape(); err != nil {
		return err
	}
	if !r.NoAuth && r.Bearer == "" && c.Token == "" {
		return Usagef("no access token: set META_ADS_ACCESS_TOKEN, store one with `metaads config set access-token` (reads stdin), or run `metaads init`")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" {
		return Usagef("invalid API base URL %q", c.BaseURL)
	}
	host := u.Hostname()
	if host != "graph.facebook.com" && !c.AllowCustomBase {
		return Usagef("refusing to send credentials to %s; set META_ADS_ALLOW_CUSTOM_BASE=1 if intentional", host)
	}
	if u.Scheme != "https" && !isLoopback(host) {
		return Usagef("refusing non-HTTPS API base %s", c.BaseURL)
	}
	if c.ReadOnly && !r.readSafe() {
		return Usagef("read-only mode (META_ADS_READ_ONLY or config read-only) blocks %s %s; reads and --validate-only creates are still allowed", r.Method, r.Path)
	}
	for _, k := range clientParams {
		if r.Query.Has(k) || r.Form.Has(k) {
			return Usagef("parameter %q carries a credential; the client owns authentication", k)
		}
	}
	return nil
}

// refuseRedirects never follows a redirect: Go keeps Authorization across a
// same-host https to http hop, and a 307/308 would replay a write.
func refuseRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// stripURL drops the URL from a transport error. Its query can carry
// appsecret_proof, debug_token's input token or the exchange's client
// secret, and the message is printed.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// attempt performs one round trip; Do wraps it with the retry loop.
func (c *Client) attempt(ctx context.Context, r Request) (*Response, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	q := url.Values{}
	for k, v := range r.Query {
		q[k] = append([]string(nil), v...)
	}
	token := c.Token
	if r.Bearer != "" {
		token = r.Bearer
	}
	if c.AppSecret != "" && !r.NoProof && !r.NoAuth {
		q.Set("appsecret_proof", Proof(token, c.AppSecret))
	}
	u := strings.TrimRight(c.BaseURL, "/") + "/" + strings.TrimLeft(r.Path, "/")
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var body io.Reader
	contentType := ""
	switch {
	case r.Multipart != nil:
		body, contentType = bytes.NewReader(r.Multipart), r.ContentType
	case r.Method != http.MethodGet:
		body, contentType = strings.NewReader(r.Form.Encode()), "application/x-www-form-urlencoded"
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, u, body)
	if err != nil {
		// The error text would quote the URL; name the path only.
		return nil, &Error{Kind: KindUsage, Message: fmt.Sprintf("%s %s: the request could not be built", r.Method, r.Path)}
	}
	if !r.NoAuth {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c.logf("> %s %s", r.Method, r.Path)
	httpClient := &http.Client{}
	if c.HTTP != nil {
		cp := *c.HTTP
		httpClient = &cp
	}
	httpClient.CheckRedirect = refuseRedirects
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, stripURL(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, stripURL(err)
	}
	c.logf("< %d (%d bytes)", resp.StatusCode, len(raw))
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: raw}, nil
}

// errorDetails is Meta's error object when the body has one, else the body.
func errorDetails(resp *Response) any {
	if len(resp.Body) == 0 {
		return nil
	}
	var env map[string]any
	if json.Unmarshal(resp.Body, &env) == nil {
		if e, ok := env["error"]; ok {
			return e
		}
		return env
	}
	var v any
	if json.Unmarshal(resp.Body, &v) == nil {
		return v
	}
	return string(resp.Body)
}

// errorFor builds the error for a non-2xx response.
func (c *Client) errorFor(r Request, resp *Response, me metaError, kind, extra string) *Error {
	rid := me.TraceID
	if rid == "" {
		rid = resp.Header.Get("x-fb-trace-id")
	}
	return &Error{
		Kind:      kind,
		Message:   fmt.Sprintf("%s %s: HTTP %d%s%s%s", r.Method, r.Path, resp.Status, me.summary(), extra, hint(me, kind, resp.Header)),
		Status:    resp.Status,
		Details:   errorDetails(resp),
		RequestID: rid,
	}
}

// Do sends a request with the retry policy: rate limits are retried for
// every call within the retry budget, waiting as long as Meta's usage
// headers ask; network failures and server errors are retried for read-safe
// calls only. A write is never replayed. The error is always *Error.
func (c *Client) Do(ctx context.Context, r Request) (*Response, error) {
	if err := c.guard(r); err != nil {
		return nil, err
	}
	sleep := c.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	budget := c.RetryBudget
	if budget == 0 {
		budget = 60 * time.Second
	}
	maxAttempts := c.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 3
	}
	var waited time.Duration
	transientTries := 0
	backoff := time.Second
	// pause waits before the next attempt. A context cancelled meanwhile
	// ends the call: another attempt would only fail, and for a write it
	// would turn a clean refusal into an unknown outcome.
	pause := func(d time.Duration) *Error {
		sleep(d)
		if err := ctx.Err(); err != nil {
			return &Error{Kind: KindTransport, Message: fmt.Sprintf("%s %s: cancelled while waiting to retry: %v", r.Method, r.Path, err)}
		}
		return nil
	}
	for {
		resp, err := c.attempt(ctx, r)
		if err != nil {
			var apiErr *Error
			if errors.As(err, &apiErr) {
				return nil, apiErr
			}
			e := classifyTransport(err, r.readSafe())
			e.Message = r.Method + " " + r.Path + ": " + e.Message
			if e.Kind == KindTransport && r.readSafe() {
				transientTries++
				if transientTries < maxAttempts {
					if pe := pause(backoff); pe != nil {
						return nil, pe
					}
					backoff *= 2
					continue
				}
			}
			return nil, e
		}
		if resp.Status >= 200 && resp.Status < 300 {
			return resp, nil
		}
		if resp.Status >= 300 && resp.Status < 400 {
			return nil, &Error{
				Kind: KindValidation, Status: resp.Status, Details: errorDetails(resp), RequestID: resp.Header.Get("x-fb-trace-id"),
				Message: fmt.Sprintf("%s %s: HTTP %d redirect to %q refused: metaads never follows redirects "+
					"(they can strip HTTPS off the token and replay a write); check META_ADS_API_BASE",
					r.Method, r.Path, resp.Status, resp.Header.Get("Location")),
			}
		}
		me, _ := parseMetaError(resp.Body)
		kind := me.kind(resp.Status)
		switch {
		case kind == KindRateLimited:
			wait := backoff
			if d := usageWait(resp.Header); d > 0 {
				wait = max(d, backoff)
			} else if ra := retryAfter(resp.Header); ra > 0 {
				wait = max(ra, backoff)
			}
			wait += jitter()
			if waited+wait > budget {
				return nil, c.errorFor(r, resp, me, kind, fmt.Sprintf("; waiting %s would exceed the %s retry budget",
					wait.Round(time.Second), budget))
			}
			waited += wait
			c.logf("* rate limited, waiting %s", wait.Round(time.Millisecond))
			if pe := pause(wait); pe != nil {
				return nil, pe
			}
			backoff *= 2
			continue
		case kind == KindServer && r.readSafe():
			transientTries++
			if transientTries < maxAttempts {
				if pe := pause(backoff); pe != nil {
					return nil, pe
				}
				backoff *= 2
				continue
			}
		}
		return nil, c.errorFor(r, resp, me, kind, "")
	}
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go vet ./internal/api/ && go test ./internal/api/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/api
git commit -m "Add the Graph API client: form encoding, appsecret_proof, Meta error codes, usage-header retries

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Guard

**Files:**
- Create: `internal/guard/json.go`, `internal/guard/rules.go`, `internal/guard/guard.go`
- Test: `internal/guard/json_test.go`, `internal/guard/guard_test.go`

**Interfaces:**
- Consumes: `config.Cap`, `config.FormatMinor` (Task 1); `route.Route`, `route.Parse` (Task 1).
- Produces:
  - `guard.Read`, `guard.Write`, `guard.Delete`, `guard.Spend`, `guard.Admin` (the class strings).
  - `guard.ParseBody(data []byte) (map[string]any, error)`: strict JSON object, numbers as `json.Number`.
  - `type guard.Request struct { Verb string; Route route.Route; Body map[string]any; Params url.Values; Files []string; ValidateOnly, Force bool }`.
  - `type guard.Policy struct { ReadOnly bool; Currency string; DailyCap, LifetimeCap *config.Cap }`.
  - `type guard.Decision struct { Class string; Findings []string }`.
  - `guard.Check(Request, Policy) (Decision, error)`: a non-nil error means nothing may be sent; its message has no method or path prefix (the CLI adds one).
  - `type guard.Catalog struct` and `guard.Rules() Catalog` for `commands --json`.

The spec's *Guardrails* section is the source of every rule below. Read it first.

- [ ] **Step 1: Write the failing tests**

`internal/guard/json_test.go`:

```go
package guard

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseBody(t *testing.T) {
	obj, err := ParseBody([]byte(`{"name":"x","daily_budget":3000,"adset_spec":{"status":"PAUSED"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := obj["daily_budget"].(json.Number); !ok || n.String() != "3000" {
		t.Fatalf("numbers must stay json.Number: %#v", obj["daily_budget"])
	}
	for body, want := range map[string]string{
		`{"status":"PAUSED","status":"ACTIVE"}`:           "duplicate key",
		`{"adset_spec":{"status":"PAUSED","status":"X"}}`: "adset_spec",
		`{"a":1} {"b":2}`:                                 "trailing data",
		`[1,2]`:                                           "JSON object",
		`"x"`:                                             "JSON object",
		`{"a":`:                                           "EOF",
	} {
		if _, err := ParseBody([]byte(body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want an error containing %q, got %v", body, want, err)
		}
	}
}
```

`internal/guard/guard_test.go`:

```go
package guard

import (
	"net/url"
	"strings"
	"testing"

	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
	"github.com/wir-drei-digital/meta-ads-cli/internal/route"
)

func pol() Policy {
	return Policy{Currency: "CHF", DailyCap: &config.Cap{Minor: 3000, Currency: "CHF"},
		LifetimeCap: &config.Cap{Minor: 30000, Currency: "CHF"}}
}

func rt(t *testing.T, path, verb string) route.Route {
	t.Helper()
	r, err := route.Parse(path, verb, "1", "v26.0")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func post(t *testing.T, path, body string) Request {
	t.Helper()
	obj, err := ParseBody([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return Request{Verb: "POST", Route: rt(t, path, "POST"), Body: obj}
}

type want struct {
	class string // expected class when err is ""
	err   string // substring of the expected error
}

func check(t *testing.T, name string, req Request, p Policy, w want) {
	t.Helper()
	d, err := Check(req, p)
	if w.err != "" {
		if err == nil || !strings.Contains(err.Error(), w.err) {
			t.Errorf("%s: want an error containing %q, got class %s err %v", name, w.err, d.Class, err)
		}
		return
	}
	if err != nil || d.Class != w.class {
		t.Errorf("%s: want class %s, got %s err %v", name, w.class, d.Class, err)
	}
}

func force(r Request) Request         { r.Force = true; return r }
func validateOnly(r Request) Request  { r.ValidateOnly = true; return r }
func readOnly(p Policy) Policy        { p.ReadOnly = true; return p }
func noCaps(p Policy) Policy          { p.DailyCap, p.LifetimeCap = nil, nil; return p }

func TestStatusRules(t *testing.T) {
	paused := `{"name":"c","objective":"OUTCOME_TRAFFIC","status":"PAUSED","special_ad_categories":[]}`
	check(t, "paused create", post(t, "act/campaigns", paused), pol(), want{class: Write})
	check(t, "create without status", post(t, "act/campaigns", `{"name":"c"}`), pol(), want{err: `created without status "PAUSED"`})
	check(t, "create without status, forced", force(post(t, "act/campaigns", `{"name":"c"}`)), pol(), want{class: Spend})
	check(t, "active create", post(t, "act/adsets", `{"status":"ACTIVE"}`), pol(), want{err: `status "ACTIVE"`})
	check(t, "lowercase paused", post(t, "act/ads", `{"status":"paused"}`), pol(), want{err: "--force"})
	check(t, "numeric status", post(t, "123", `{"status":1}`), pol(), want{err: "status 1"})
	check(t, "activate", post(t, "123", `{"status":"ACTIVE"}`), pol(), want{err: "needs --force"})
	check(t, "activate forced", force(post(t, "123", `{"status":"ACTIVE"}`)), pol(), want{class: Spend})
	check(t, "configured_status", post(t, "123", `{"configured_status":"ACTIVE"}`), pol(), want{err: "configured_status"})
	check(t, "pause", post(t, "123", `{"status":"PAUSED"}`), pol(), want{class: Write})
	check(t, "archive", post(t, "123", `{"status":"ARCHIVED"}`), pol(), want{class: Write})
	check(t, "delete by status", post(t, "123", `{"status":"DELETED"}`), pol(), want{err: "deletes the object"})
	check(t, "delete by status forced", force(post(t, "123", `{"status":"DELETED"}`)), pol(), want{class: Delete})
	check(t, "rename", post(t, "123", `{"name":"new"}`), pol(), want{class: Write})
}

func TestBudgetCaps(t *testing.T) {
	check(t, "daily at cap on create", post(t, "act/adsets", `{"status":"PAUSED","daily_budget":3000}`), pol(), want{class: Write})
	check(t, "daily as string", post(t, "act/adsets", `{"status":"PAUSED","daily_budget":"3000"}`), pol(), want{class: Write})
	check(t, "daily one above", post(t, "act/adsets", `{"status":"PAUSED","daily_budget":3001}`), pol(), want{err: "exceeds the daily-budget-cap of 3000 (30.00 CHF)"})
	check(t, "above cap with force", force(post(t, "act/adsets", `{"status":"PAUSED","daily_budget":3001}`)), pol(), want{err: "exceeds"})
	check(t, "above cap validate-only", validateOnly(post(t, "act/adsets", `{"status":"PAUSED","daily_budget":3001}`)), pol(), want{err: "exceeds"})
	check(t, "lifetime at cap", post(t, "act/adsets", `{"status":"PAUSED","lifetime_budget":30000}`), pol(), want{class: Write})
	check(t, "lifetime above cap", post(t, "act/campaigns", `{"status":"PAUSED","lifetime_budget":30001}`), pol(), want{err: "lifetime-budget-cap"})
	check(t, "no cap", post(t, "act/adsets", `{"status":"PAUSED","daily_budget":100}`), noCaps(pol()), want{err: "no daily-budget-cap is configured"})
	eur := pol()
	eur.DailyCap = &config.Cap{Minor: 3000, Currency: "EUR"}
	check(t, "cap in another currency", post(t, "act/adsets", `{"status":"PAUSED","daily_budget":100}`), eur, want{err: "set the cap again"})
	nocur := pol()
	nocur.Currency = ""
	check(t, "no currency", post(t, "act/adsets", `{"status":"PAUSED","daily_budget":100}`), nocur, want{err: "no currency is configured"})
	check(t, "budget change", post(t, "123", `{"daily_budget":2500}`), pol(), want{err: "changes the budget of an existing object"})
	check(t, "budget change forced", force(post(t, "123", `{"daily_budget":2500}`)), pol(), want{class: Spend})
	check(t, "budget change above cap forced", force(post(t, "123", `{"daily_budget":4000}`)), pol(), want{err: "exceeds"})
}

func TestAmountForms(t *testing.T) {
	for _, amount := range []string{`" 3000"`, `"+3000"`, `3e3`, `"3000.0"`, `"03000"`, `3000.0`, `-1`, `true`, `null`, `"30 CHF"`} {
		check(t, "amount "+amount, post(t, "act/adsets", `{"status":"PAUSED","daily_budget":`+amount+`}`), pol(),
			want{err: "whole number in the account currency's minor unit"})
	}
	check(t, "zero", post(t, "act/adsets", `{"status":"PAUSED","daily_budget":"0"}`), pol(), want{class: Write})
}

func TestNestedSpecs(t *testing.T) {
	check(t, "paused ad with paused adset_spec", post(t, "act/ads", `{"status":"PAUSED","adset_spec":{"status":"PAUSED","daily_budget":3000}}`), pol(), want{class: Write})
	check(t, "adset_spec active", post(t, "act/ads", `{"status":"PAUSED","adset_spec":{"status":"ACTIVE"}}`), pol(), want{err: `adset_spec.status "ACTIVE"`})
	check(t, "adset_spec without status", post(t, "act/ads", `{"status":"PAUSED","adset_spec":{"name":"s"}}`), pol(), want{err: `adset_spec: created without status "PAUSED"`})
	check(t, "adset_spec as string over cap", post(t, "act/ads", `{"status":"PAUSED","adset_spec":"{\"status\":\"PAUSED\",\"daily_budget\":5000}"}`), pol(), want{err: "adset_spec.daily_budget 5000"})
	check(t, "adset_spec as string, active", post(t, "act/ads", `{"status":"PAUSED","adset_spec":"{\"status\":\"ACTIVE\"}"}`), pol(), want{err: `adset_spec.status "ACTIVE"`})
	check(t, "adset_spec unparseable", post(t, "act/ads", `{"status":"PAUSED","adset_spec":"{"}`), pol(), want{err: "adset_spec: is a string that is not a JSON object"})
	check(t, "adset_spec duplicate key in string", post(t, "act/ads", `{"status":"PAUSED","adset_spec":"{\"status\":\"PAUSED\",\"status\":\"ACTIVE\"}"}`), pol(), want{err: "duplicate key"})
	check(t, "adset_spec not an object", post(t, "act/ads", `{"status":"PAUSED","adset_spec":[1]}`), pol(), want{err: "must be a JSON object"})
	check(t, "campaign_spec without status", post(t, "act/adsets", `{"status":"PAUSED","campaign_spec":{"name":"c"}}`), pol(), want{err: "campaign_spec: created without"})
	check(t, "campaign_spec spend_cap", force(post(t, "act/adsets", `{"status":"PAUSED","campaign_spec":{"status":"PAUSED","spend_cap":"922337203685478"}}`)), pol(), want{class: Spend})
	check(t, "adset_spec refused field", post(t, "act/ads", `{"status":"PAUSED","adset_spec":{"status":"PAUSED","budget_schedule_specs":[]}}`), pol(), want{err: "adset_spec.budget_schedule_specs is refused"})
}

func TestAdsetBudgets(t *testing.T) {
	check(t, "within cap", post(t, "123", `{"adset_budgets":[{"adset_id":"9","daily_budget":2000}]}`), pol(), want{err: "adset_budgets[0].daily_budget 2000 changes the budget"})
	check(t, "within cap forced", force(post(t, "123", `{"adset_budgets":[{"adset_id":"9","daily_budget":2000}]}`)), pol(), want{class: Spend})
	check(t, "above cap", force(post(t, "123", `{"adset_budgets":[{"adset_id":"9","daily_budget":4000}]}`)), pol(), want{err: "adset_budgets[0].daily_budget 4000: exceeds"})
	check(t, "as string above cap", force(post(t, "123", `{"adset_budgets":"[{\"adset_id\":\"9\",\"lifetime_budget\":40000}]"}`)), pol(), want{err: "adset_budgets[0].lifetime_budget 40000: exceeds"})
	check(t, "not an array", post(t, "123", `{"adset_budgets":{"a":1}}`), pol(), want{err: "must be a JSON array"})
	check(t, "entry not an object", post(t, "123", `{"adset_budgets":[1]}`), pol(), want{err: "adset_budgets[0] must be a JSON object"})
	check(t, "unparseable string", post(t, "123", `{"adset_budgets":"[{"}`), pol(), want{err: "not a JSON array"})
}

func TestSpendFieldsAndRefusals(t *testing.T) {
	check(t, "campaign spend_cap", post(t, "123", `{"spend_cap":"922337203685478"}`), pol(), want{err: "spend_cap changes or removes a spending limit"})
	check(t, "account spend_cap", force(post(t, "act", `{"spend_cap":"1000"}`)), pol(), want{err: "spend_cap on the ad account is refused"})
	check(t, "account spend_cap_action", force(post(t, "act", `{"spend_cap_action":"reset"}`)), pol(), want{err: "spend_cap_action is refused"})
	check(t, "account rename", post(t, "act", `{"name":"x"}`), pol(), want{err: "changes account settings"})
	check(t, "account rename forced", force(post(t, "act", `{"name":"x"}`)), pol(), want{class: Admin})
	check(t, "budget_schedule_specs", force(post(t, "123", `{"budget_schedule_specs":[]}`)), pol(), want{err: "budget_schedule_specs is refused"})
	check(t, "reserved buying", force(post(t, "act/campaigns", `{"status":"PAUSED","buying_type":"RESERVED"}`)), pol(), want{err: "buying_type \"RESERVED\" is refused"})
	check(t, "auction buying", post(t, "act/campaigns", `{"status":"PAUSED","buying_type":"AUCTION"}`), pol(), want{class: Write})
	check(t, "bids stay write", post(t, "123", `{"bid_amount":150,"bid_strategy":"COST_CAP","targeting":{"geo_locations":{"countries":["CH"]}}}`), pol(), want{class: Write})
	for _, edge := range []string{"budget_schedules", "adrules_library", "reachfrequencypredictions", "async_batch_requests"} {
		node := "act"
		if edge == "budget_schedules" {
			node = "123"
		}
		check(t, "edge "+edge, force(post(t, node+"/"+edge, `{}`)), pol(), want{err: "the " + edge + " edge is refused"})
	}
	check(t, "unknown edge", post(t, "act/customaudiences", `{"name":"a"}`), pol(), want{err: "outside campaign editing"})
	check(t, "unknown edge forced", force(post(t, "act/customaudiences", `{"name":"a"}`)), pol(), want{class: Admin})
	check(t, "copy paused", post(t, "123/copies", `{"deep_copy":true}`), pol(), want{class: Write})
	check(t, "copy active", post(t, "123/copies", `{"status_option":"ACTIVE"}`), pol(), want{err: "status_option \"ACTIVE\" starts delivery of the copy"})
	check(t, "copy inherited", post(t, "123/copies", `{"status_option":"INHERITED_FROM_SOURCE"}`), pol(), want{err: "--force"})
	check(t, "images", post(t, "act/adimages", `{}`), pol(), want{class: Write})
	check(t, "async insights", post(t, "act/insights", `{"level":"ad","date_preset":"last_7d"}`), pol(), want{class: Read})
}

func TestGetAndDelete(t *testing.T) {
	get := Request{Verb: "GET", Route: rt(t, "act/campaigns", "GET"), Params: url.Values{"limit": {"5"}}}
	check(t, "get", get, pol(), want{class: Read})
	check(t, "get read-only", get, readOnly(pol()), want{class: Read})
	del := Request{Verb: "DELETE", Route: rt(t, "123", "DELETE")}
	check(t, "delete", del, pol(), want{err: "needs --force"})
	check(t, "delete forced", force(del), pol(), want{class: Delete})
	bulk := Request{Verb: "DELETE", Route: rt(t, "act/campaigns", "DELETE"), Params: url.Values{"delete_strategy": {"DELETE_ANY"}}}
	check(t, "bulk delete", force(bulk), pol(), want{err: "delete_strategy is refused"})
}

func TestOwnedParams(t *testing.T) {
	get := func(k string) Request {
		return Request{Verb: "GET", Route: rt(t, "me", "GET"), Params: url.Values{k: {"x"}}}
	}
	for _, k := range []string{"method", "access_token", "appsecret_proof", "batch", "callback", "suppress_http_code"} {
		check(t, "get param "+k, get(k), pol(), want{err: "parameter " + k + " is refused"})
		check(t, "post field "+k, force(post(t, "123", `{"`+k+`":"x"}`)), pol(), want{err: "field " + k + " is refused"})
	}
	check(t, "execution_options in data", post(t, "act/campaigns", `{"status":"PAUSED","execution_options":["validate_only"]}`), pol(), want{err: "set by --validate-only"})
	check(t, "fields param", get("fields"), pol(), want{err: "--fields"})
	files := post(t, "act/adimages", `{"name":"x"}`)
	files.Files = []string{"status"}
	check(t, "file named status", files, pol(), want{err: "--file status"})
	files.Files = []string{"name"}
	check(t, "file colliding with data", files, pol(), want{err: "same name as a --data field"})
	files.Files = []string{"filename"}
	check(t, "file ok", files, pol(), want{class: Write})
}

func TestValidateOnlyAndReadOnly(t *testing.T) {
	create := post(t, "act/campaigns", `{"name":"c"}`)
	check(t, "validate-only skips force", validateOnly(create), pol(), want{class: Spend})
	check(t, "validate-only in read-only mode", validateOnly(create), readOnly(pol()), want{class: Spend})
	check(t, "validate-only on an update", validateOnly(post(t, "123", `{"name":"x"}`)), pol(), want{err: "accepted only when creating"})
	check(t, "validate-only on another edge", validateOnly(post(t, "act/customaudiences", `{}`)), pol(), want{err: "accepted only when creating"})
	check(t, "validate-only on a delete", validateOnly(Request{Verb: "DELETE", Route: rt(t, "123", "DELETE")}), pol(), want{err: "accepted only when creating"})
	check(t, "read-only blocks a paused create", post(t, "act/campaigns", `{"status":"PAUSED"}`), readOnly(pol()), want{err: "read-only mode"})
	check(t, "read-only blocks a forced delete", force(Request{Verb: "DELETE", Route: rt(t, "123", "DELETE")}), readOnly(pol()), want{err: "read-only mode"})
	check(t, "read-only allows async insights", post(t, "act/insights", `{}`), readOnly(pol()), want{class: Read})
}

func TestSeveralFindings(t *testing.T) {
	d, err := Check(force(post(t, "123", `{"status":"ACTIVE","daily_budget":2000}`)), pol())
	if err != nil || d.Class != Spend || len(d.Findings) != 2 {
		t.Fatalf("%+v %v", d, err)
	}
	_, err = Check(post(t, "123", `{"status":"ACTIVE","daily_budget":2000}`), pol())
	if err == nil || !strings.Contains(err.Error(), `status "ACTIVE"`) || !strings.Contains(err.Error(), "daily_budget 2000") {
		t.Fatalf("every finding must be named: %v", err)
	}
}

func TestRules(t *testing.T) {
	r := Rules()
	if r.PostEdges["campaigns"] != Write || r.PostEdges["insights"] != Read || r.DefaultPostEdge != Admin ||
		r.Refused["field delete_strategy"] == "" || r.Refused["edge adrules_library"] == "" ||
		r.Refused["field spend_cap on the ad account"] == "" || r.OwnedParams["method"] == "" ||
		strings.Join(r.ValidateOnlyEdges, ",") != "adcreatives,ads,adsets,campaigns" {
		t.Fatalf("%+v", r)
	}
	r.PostEdges["campaigns"] = Admin
	if Rules().PostEdges["campaigns"] != Write {
		t.Fatal("Rules must return a copy")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/guard/`
Expected: FAIL (build errors: `ParseBody`, `Check`, `Rules` and the rest are undefined).

- [ ] **Step 3: Implement the strict JSON reader**

`internal/guard/json.go` (adapted from google-ads-cli's `internal/guard/json.go`, without the camelCase folding: the Graph API uses snake_case only):

```go
package guard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ParseBody decodes --data strictly: one JSON object, no key given twice at
// any depth, nothing after it. Numbers come back as json.Number, so every
// amount keeps the text it was written with. The guard and Meta must read
// the same values, and a duplicate key is where they would differ.
func ParseBody(data []byte) (map[string]any, error) {
	v, err := parseStrict(data)
	if err != nil {
		return nil, err
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("the body must be a JSON object")
	}
	return obj, nil
}

// parseStrict decodes one JSON value with the same rules.
func parseStrict(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec, "")
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after the JSON body")
	}
	return v, nil
}

func parseValue(dec *json.Decoder, path string) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil // string, json.Number, bool or nil
	}
	switch delim {
	case '{':
		obj := map[string]any{}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key := kt.(string)
			if _, dup := obj[key]; dup {
				return nil, fmt.Errorf("duplicate key %q at %s", key, where(path))
			}
			v, err := parseValue(dec, join(path, key))
			if err != nil {
				return nil, err
			}
			obj[key] = v
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return obj, nil
	case '[':
		arr := []any{}
		for i := 0; dec.More(); i++ {
			v, err := parseValue(dec, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return arr, nil
	}
	return nil, fmt.Errorf("unexpected %v at %s", delim, where(path))
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func where(path string) string {
	if path == "" {
		return "the top level"
	}
	return path
}
```

- [ ] **Step 4: Implement the rule table**

`internal/guard/rules.go`:

```go
package guard

import (
	"maps"
	"sort"
)

// Risk classes. delete, spend and admin need --force.
const (
	Read   = "read"
	Write  = "write"
	Delete = "delete"
	Spend  = "spend"
	Admin  = "admin"
)

// rank orders the classes; delete, spend and admin share the top rank.
func rank(class string) int {
	switch class {
	case Read:
		return 0
	case Write:
		return 1
	}
	return 2
}

// postEdges classifies POST on an edge. An edge not listed here is admin:
// what the CLI does not know is outside campaign editing until reviewed.
var postEdges = map[string]string{
	"insights":    Read, // starts an asynchronous report run
	"campaigns":   Write,
	"adsets":      Write,
	"ads":         Write,
	"adcreatives": Write,
	"adimages":    Write,
	"adlabels":    Write,
	"copies":      Write,
}

// createEdges create objects that deliver unless they are created paused.
var createEdges = map[string]bool{"campaigns": true, "adsets": true, "ads": true}

// validateOnlyEdges are the create edges where Meta documents
// execution_options=["validate_only"].
var validateOnlyEdges = map[string]bool{"campaigns": true, "adsets": true, "ads": true, "adcreatives": true}

// refusedEdges are refused whatever the flags.
var refusedEdges = map[string]string{
	"budget_schedules":          "high-demand periods raise a daily budget up to 8 times, outside the budget cap",
	"adrules_library":           "automated rules change budgets and status later, outside any check",
	"reachfrequencypredictions": "reserved buying commits spend the budget cap cannot check",
	"async_batch_requests":      "batch requests are not supported in v1; send the requests one by one",
}

// refusedFields are refused wherever they appear.
var refusedFields = map[string]string{
	"budget_schedule_specs": "high-demand periods raise a daily budget up to 8 times, outside the budget cap",
	"delete_strategy":       "bulk deletion removes every campaign, ad set or ad that matches a strategy",
	"spend_cap_action":      "the account spending limit belongs to a person; reset frees headroom by zeroing the amount spent",
}

const (
	accountSpendCapReason = "the account spending limit belongs to a person; change it in the Business Portfolio's billing settings"
	buyingTypeReason      = "reserved buying commits spend the budget cap cannot check; only AUCTION is allowed"
)

// ownedParams are set by the CLI or change how Meta reads a request; they
// are refused in --data, --param and --file.
var ownedParams = map[string]string{
	"method":             "the Graph API would apply the request as another method than the one the guard checked",
	"access_token":       "the client sends the credential",
	"appsecret_proof":    "the client computes it",
	"appsecret_time":     "the client owns request signing",
	"batch":              "batch requests are not supported in v1",
	"include_headers":    "belongs to batch requests",
	"callback":           "JSONP would wrap the response in JavaScript",
	"suppress_http_code": "errors would arrive as HTTP 200 and read as success",
	"execution_options":  "set by --validate-only",
}

// inspected are the fields the checks read. A --file part carries bytes the
// guard does not read, so it may not use one of these names.
var inspected = []string{"adset_budgets", "adset_spec", "budget_schedule_specs", "buying_type", "campaign_spec",
	"configured_status", "daily_budget", "delete_strategy", "lifetime_budget", "spend_cap", "spend_cap_action",
	"status", "status_option"}

// nestedSpecs are objects inside a body that create what they describe.
var nestedSpecs = []string{"campaign_spec", "adset_spec"}

// Catalog is the rule table as `metaads commands --json` publishes it.
type Catalog struct {
	Classes           []string          `json:"classes"`
	ForceClasses      []string          `json:"force_classes"`
	PostEdges         map[string]string `json:"post_edges"`
	DefaultPostEdge   string            `json:"default_post_edge_class"`
	NodeUpdate        string            `json:"node_update_class"`
	AccountUpdate     string            `json:"account_update_class"`
	DeleteClass       string            `json:"delete_class"`
	CreateEdges       []string          `json:"create_edges"`
	ValidateOnlyEdges []string          `json:"validate_only_edges"`
	NestedSpecs       []string          `json:"nested_specs"`
	Refused           map[string]string `json:"refused"`
	OwnedParams       map[string]string `json:"owned_params"`
	FileFieldsRefused []string          `json:"file_fields_refused"`
}

// Rules returns a copy of the rule table.
func Rules() Catalog {
	refused := map[string]string{
		"field spend_cap on the ad account":    accountSpendCapReason,
		"field buying_type other than AUCTION": buyingTypeReason,
	}
	for k, v := range refusedEdges {
		refused["edge "+k] = v
	}
	for k, v := range refusedFields {
		refused["field "+k] = v
	}
	return Catalog{
		Classes:           []string{Read, Write, Delete, Spend, Admin},
		ForceClasses:      []string{Delete, Spend, Admin},
		PostEdges:         maps.Clone(postEdges),
		DefaultPostEdge:   Admin,
		NodeUpdate:        Write,
		AccountUpdate:     Admin,
		DeleteClass:       Delete,
		CreateEdges:       sortedSet(createEdges),
		ValidateOnlyEdges: sortedSet(validateOnlyEdges),
		NestedSpecs:       []string{"adset_budgets", "adset_spec", "campaign_spec"},
		Refused:           refused,
		OwnedParams:       maps.Clone(ownedParams),
		FileFieldsRefused: append([]string(nil), inspected...),
	}
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 5: Implement Check**

`internal/guard/guard.go`:

```go
// Package guard decides, before anything is sent, what a request may do. It
// classifies by verb and path, then reads every field that can start
// delivery or move money, including the nested specs Meta accepts. A refusal
// or an unmet gate is an error, and nothing is sent.
package guard

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
	"github.com/wir-drei-digital/meta-ads-cli/internal/route"
)

// Request is everything the guard inspects: the verb, the parsed route and
// every field that will be sent.
type Request struct {
	Verb         string // GET, POST or DELETE
	Route        route.Route
	Body         map[string]any // POST: the --data object, parsed with ParseBody
	Params       url.Values     // GET query parameters and DELETE form fields
	Files        []string       // POST: the field names of the --file parts
	ValidateOnly bool
	Force        bool
}

// Policy is the configuration the checks read.
type Policy struct {
	ReadOnly    bool
	Currency    string // the ad account currency
	DailyCap    *config.Cap
	LifetimeCap *config.Cap
}

// Decision is the class a request ended in and the findings that raised it.
type Decision struct {
	Class    string
	Findings []string
}

// Check classifies req and applies the gates. A non-nil error is the reason
// nothing may be sent; the CLI reports it as a usage error.
func Check(req Request, pol Policy) (Decision, error) {
	if err := checkOwned(req); err != nil {
		return Decision{}, err
	}
	c := &checker{pol: pol, class: Read}
	switch req.Verb {
	case "GET":
	case "DELETE":
		for _, k := range sortedParams(req.Params) {
			if reason, ok := refusedFields[k]; ok {
				return Decision{}, fmt.Errorf("%s is refused: %s", k, reason)
			}
		}
		c.add(Delete, "DELETE removes what the path names")
	case "POST":
		if err := c.post(req); err != nil {
			return Decision{}, err
		}
	default:
		return Decision{}, fmt.Errorf("unsupported method %q", req.Verb)
	}
	if req.ValidateOnly && (req.Verb != "POST" || !validateOnlyEdges[req.Route.Edge] || !req.Route.IsAccount()) {
		return Decision{}, fmt.Errorf("--validate-only is accepted only when creating on act/campaigns, act/adsets, act/ads " +
			"or act/adcreatives, where Meta documents it; elsewhere an endpoint could ignore it and apply the change")
	}
	d := Decision{Class: c.class, Findings: c.findings}
	if pol.ReadOnly && d.Class != Read && !req.ValidateOnly {
		return Decision{}, fmt.Errorf("read-only mode blocks this %s-class request; reads and --validate-only creates are still allowed", d.Class)
	}
	if rank(d.Class) >= 2 && !req.Force && !req.ValidateOnly {
		return Decision{}, fmt.Errorf("needs --force, which a person decides (%s): %s", d.Class, strings.Join(d.Findings, "; "))
	}
	return d, nil
}

// checkOwned refuses the parameters the CLI owns at the top level of what is
// sent, and --file names that would carry a field the guard reads.
func checkOwned(req Request) error {
	for _, k := range sortedParams(req.Params) {
		if reason, ok := ownedParams[k]; ok {
			return fmt.Errorf("parameter %s is refused: %s", k, reason)
		}
		if k == "fields" && req.Verb == "GET" {
			return fmt.Errorf("set fields with --fields, not --param")
		}
	}
	for _, k := range sortedKeys(req.Body) {
		if reason, ok := ownedParams[k]; ok {
			return fmt.Errorf("field %s is refused: %s", k, reason)
		}
	}
	for _, f := range req.Files {
		if reason, ok := ownedParams[f]; ok {
			return fmt.Errorf("--file %s is refused: %s", f, reason)
		}
		if slices.Contains(inspected, f) {
			return fmt.Errorf("--file %s is refused: a file part named like a field the guard checks would carry that field unchecked", f)
		}
		if _, dup := req.Body[f]; dup {
			return fmt.Errorf("--file %s has the same name as a --data field", f)
		}
	}
	return nil
}

// scope says what an object in a POST body is.
type scope int

const (
	scopeOther   scope = iota // an edge that is neither a create nor a copy
	scopeCreate               // a create of a campaign, ad set or ad; paused creates cannot spend
	scopeUpdate               // a change to something that exists
	scopeAccount              // the ad account's own settings
	scopeCopy                 // POST /<id>/copies
)

type checker struct {
	pol      Policy
	class    string
	findings []string
}

func (c *checker) add(class, finding string) {
	if rank(class) > rank(c.class) {
		c.class = class
	}
	if finding != "" {
		c.findings = append(c.findings, finding)
	}
}

func (c *checker) post(req Request) error {
	rt := req.Route
	sc := scopeOther
	switch {
	case rt.Edge == "" && rt.IsAccount():
		c.add(Admin, "POST on the ad account changes account settings")
		sc = scopeAccount
	case rt.Edge == "":
		c.add(Write, "")
		sc = scopeUpdate
	default:
		if reason, ok := refusedEdges[rt.Edge]; ok {
			return fmt.Errorf("the %s edge is refused: %s", rt.Edge, reason)
		}
		if class, known := postEdges[rt.Edge]; known {
			c.add(class, "")
		} else {
			c.add(Admin, fmt.Sprintf("POST on the %s edge is outside campaign editing", rt.Edge))
		}
		switch {
		case createEdges[rt.Edge]:
			sc = scopeCreate
		case rt.Edge == "copies":
			sc = scopeCopy
		}
	}
	return c.object(req.Body, "", sc)
}

// object checks one JSON object of the body; path prefixes every field name
// in a message ("adset_spec.").
func (c *checker) object(obj map[string]any, path string, sc scope) error {
	for _, k := range sortedKeys(obj) {
		if reason, ok := refusedFields[k]; ok {
			return fmt.Errorf("%s%s is refused: %s", path, k, reason)
		}
	}
	if v, ok := obj["buying_type"]; ok {
		if s, _ := v.(string); s != "AUCTION" {
			return fmt.Errorf("%sbuying_type %s is refused: %s", path, show(v), buyingTypeReason)
		}
	}
	if _, ok := obj["spend_cap"]; ok {
		if sc == scopeAccount {
			return fmt.Errorf("%sspend_cap on the ad account is refused: %s", path, accountSpendCapReason)
		}
		c.add(Spend, path+"spend_cap changes or removes a spending limit")
	}
	c.status(obj, path, sc)
	for _, b := range []struct {
		field, capName string
		cap            *config.Cap
	}{
		{"daily_budget", "daily-budget-cap", c.pol.DailyCap},
		{"lifetime_budget", "lifetime-budget-cap", c.pol.LifetimeCap},
	} {
		v, ok := obj[b.field]
		if !ok {
			continue
		}
		amount, err := amountOf(v)
		if err != nil {
			return fmt.Errorf("%s%s: %v", path, b.field, err)
		}
		if err := c.withinCap(amount, b.cap, b.capName); err != nil {
			return fmt.Errorf("%s%s %d: %v", path, b.field, amount, err)
		}
		if sc != scopeCreate {
			c.add(Spend, fmt.Sprintf("%s%s %d changes the budget of an existing object", path, b.field, amount))
		}
	}
	if sc == scopeCopy {
		if v, ok := obj["status_option"]; ok {
			if s, _ := v.(string); s != "PAUSED" {
				c.add(Spend, fmt.Sprintf("%sstatus_option %s starts delivery of the copy", path, show(v)))
			}
		}
	}
	for _, key := range nestedSpecs {
		v, ok := obj[key]
		if !ok {
			continue
		}
		spec, err := asObject(v)
		if err != nil {
			return fmt.Errorf("%s%s: %v", path, key, err)
		}
		if err := c.object(spec, path+key+".", scopeCreate); err != nil {
			return err
		}
	}
	if v, ok := obj["adset_budgets"]; ok {
		list, err := asArray(v)
		if err != nil {
			return fmt.Errorf("%sadset_budgets: %v", path, err)
		}
		for i, e := range list {
			entry, ok := e.(map[string]any)
			if !ok {
				return fmt.Errorf("%sadset_budgets[%d] must be a JSON object", path, i)
			}
			if err := c.object(entry, fmt.Sprintf("%sadset_budgets[%d].", path, i), scopeUpdate); err != nil {
				return err
			}
		}
	}
	return nil
}

// status applies the delivery rules: anything but PAUSED may start delivery
// (ARCHIVED stays write outside a create, DELETED is a delete), and a create
// without an explicit PAUSED may too, because Meta does not document the
// default.
func (c *checker) status(obj map[string]any, path string, sc scope) {
	for _, key := range []string{"status", "configured_status"} {
		v, ok := obj[key]
		if !ok {
			continue
		}
		s, _ := v.(string)
		switch {
		case s == "PAUSED":
		case s == "ARCHIVED" && sc != scopeCreate:
		case s == "DELETED" && sc != scopeCreate:
			c.add(Delete, fmt.Sprintf(`%s%s "DELETED" deletes the object`, path, key))
		default:
			c.add(Spend, fmt.Sprintf(`%s%s %s: anything but "PAUSED" may start delivery`, path, key, show(v)))
		}
	}
	if sc == scopeCreate {
		if _, ok := obj["status"]; !ok {
			prefix := ""
			if path != "" {
				prefix = strings.TrimSuffix(path, ".") + ": "
			}
			c.add(Spend, prefix+`created without status "PAUSED"; Meta does not document the default, so it may start delivery`)
		}
	}
}

func (c *checker) withinCap(amount int64, cp *config.Cap, name string) error {
	switch {
	case cp == nil:
		return fmt.Errorf("no %s is configured, so no such budget can be set; a person sets one with `metaads config set %s <amount>`", name, name)
	case c.pol.Currency == "":
		return fmt.Errorf("no currency is configured; run `metaads config set currency <code>` and set the cap again")
	case cp.Currency != c.pol.Currency:
		return fmt.Errorf("the %s was set in %s, but the account currency is %s; set the cap again", name, cp.Currency, c.pol.Currency)
	case amount > cp.Minor:
		return fmt.Errorf("exceeds the %s of %d (%s)", name, cp.Minor, config.FormatMinor(cp.Minor, cp.Currency))
	}
	return nil
}

var wholeNumber = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

// amountOf reads a budget: a JSON integer or a string of digits, exactly.
func amountOf(v any) (int64, error) {
	var s string
	switch x := v.(type) {
	case json.Number:
		s = x.String()
	case string:
		s = x
	}
	if !wholeNumber.MatchString(s) {
		return 0, fmt.Errorf("want a whole number in the account currency's minor unit, such as 3000 for CHF 30.00; got %s", show(v))
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %s is too large", s)
	}
	return n, nil
}

// asObject reads a nested spec: an object, or a JSON string holding one, as
// Meta's examples write nested values.
func asObject(v any) (map[string]any, error) {
	switch x := v.(type) {
	case map[string]any:
		return x, nil
	case string:
		obj, err := ParseBody([]byte(x))
		if err != nil {
			return nil, fmt.Errorf("is a string that is not a JSON object: %v", err)
		}
		return obj, nil
	}
	return nil, fmt.Errorf("must be a JSON object, got %s", show(v))
}

// asArray reads adset_budgets: an array, or a JSON string holding one.
func asArray(v any) ([]any, error) {
	switch x := v.(type) {
	case []any:
		return x, nil
	case string:
		val, err := parseStrict([]byte(x))
		if arr, ok := val.([]any); err == nil && ok {
			return arr, nil
		}
		if err != nil {
			return nil, fmt.Errorf("is a string that is not a JSON array: %v", err)
		}
		return nil, fmt.Errorf("is a string that is not a JSON array")
	}
	return nil, fmt.Errorf("must be a JSON array, got %s", show(v))
}

// show renders a value as JSON for a message.
func show(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedParams(v url.Values) []string {
	out := make([]string, 0, len(v))
	for k := range v {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go vet ./internal/guard/ && go test ./internal/guard/`
Expected: PASS. If a message assertion fails, fix the message in the code to match the test (the tests quote the spec's wording), not the other way round.

- [ ] **Step 7: Commit**

```bash
git add internal/guard
git commit -m "Add the guard: classes by verb and edge, status and budget checks, nested specs, refusals

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: CLI core: get, post, delete, paging, catalog

**Files:**
- Create: `cmd/metaads/main.go`
- Create: `internal/cli/app.go`, `internal/cli/root.go`, `internal/cli/util.go`, `internal/cli/body.go`, `internal/cli/output.go`, `internal/cli/verbs.go`, `internal/cli/all.go`, `internal/cli/catalog.go`, `internal/cli/version.go`
- Test: `internal/cli/app_test.go`, `internal/cli/verbs_test.go`
- Modify: `go.mod`, `go.sum` (cobra)

**Interfaces:**
- Consumes: `config.Resolve`, `config.Resolved`, `config.Cap`, `config.DefaultAPIVersion` (Task 1); `route.Parse`, `route.Route` (Task 1); `api.Client`, `api.Request`, `api.Response`, `api.Error`, `api.Usagef`, the kinds, `api.EncodeForm`, `api.EncodeMultipart`, `api.File` (Task 2); `guard.Check`, `guard.Request`, `guard.Policy`, `guard.ParseBody`, `guard.Rules`, `guard.Catalog` (Task 3).
- Produces (Task 5 builds on these):
  - `type app struct { client *api.Client; res config.Resolved; configErr error; stdout, stderr io.Writer; stdin io.Reader }` (Task 5 adds `prompt prompter` and `isTerminal func() bool`).
  - `cli.Execute(args []string) int`; `(*app).run(args []string) int`; `(*app).configure(getenv func(string) string, sleep func(time.Duration))`; `(*app).renderError(error) int`.
  - `(*app).newRoot() *cobra.Command` with `root.AddCommand(...)` listing the top-level commands (Task 5 appends `a.configCommand(), a.authCommand(), a.initCommand()` to that call).
  - Helpers `flagString(cmd, name) string`, `groupRunE(cmd, args) error`, `jsonCompact(v any) ([]byte, error)`.
  - Test helpers in `app_test.go`: `type seen`, `type fakeGraph` with `newFakeGraph(t, reply func(seen) (int, string)) *fakeGraph` and `(*fakeGraph).all() []seen`, `defaultRes() config.Resolved`, `testApp(t, g *fakeGraph, res config.Resolved) (*app, *bytes.Buffer, *bytes.Buffer)`, `errJSON(t, *bytes.Buffer) map[string]any`, `isolate(t) string`.

- [ ] **Step 1: Add cobra**

Run: `go get github.com/spf13/cobra@v1.10.2`

- [ ] **Step 2: Write the test helpers and the failing tests**

`internal/cli/app_test.go`:

```go
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
)

// seen is one request as the fake Graph server received it.
type seen struct {
	Method, Path, ContentType, Auth string
	Query, Form                     url.Values
	Body                            []byte
}

// fakeGraph records requests and answers with reply, or 200 {"id":"1"}.
type fakeGraph struct {
	*httptest.Server
	mu    sync.Mutex
	calls []seen
}

func newFakeGraph(t *testing.T, reply func(seen) (int, string)) *fakeGraph {
	t.Helper()
	g := &fakeGraph{}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s := seen{Method: r.Method, Path: r.URL.Path, ContentType: r.Header.Get("Content-Type"),
			Auth: r.Header.Get("Authorization"), Query: r.URL.Query(), Body: b}
		if strings.HasPrefix(s.ContentType, "application/x-www-form-urlencoded") {
			s.Form, _ = url.ParseQuery(string(b))
		}
		g.mu.Lock()
		g.calls = append(g.calls, s)
		g.mu.Unlock()
		status, body := 200, `{"id":"1"}`
		if reply != nil {
			status, body = reply(s)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *fakeGraph) all() []seen {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]seen(nil), g.calls...)
}

func defaultRes() config.Resolved {
	return config.Resolved{AccessToken: "tok", TokenFrom: "env", AdAccountID: "1", Currency: "CHF",
		DailyCap: &config.Cap{Minor: 3000, Currency: "CHF"}, LifetimeCap: &config.Cap{Minor: 30000, Currency: "CHF"}}
}

// testApp wires an app to g (or, with g nil, to the real host for tests that
// must refuse before any network I/O).
func testApp(t *testing.T, g *fakeGraph, res config.Resolved) (*app, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	base := "https://graph.facebook.com"
	if g != nil {
		base = g.URL
	}
	a := &app{res: res, stdout: &out, stderr: &errb, stdin: strings.NewReader("")}
	a.client = &api.Client{BaseURL: base, AllowCustomBase: true, Token: res.AccessToken, AppSecret: res.AppSecret,
		ReadOnly: res.ReadOnly, Sleep: func(time.Duration) {}}
	return a, &out, &errb
}

func errJSON(t *testing.T, errb *bytes.Buffer) map[string]any {
	t.Helper()
	if n := strings.Count(strings.TrimSuffix(errb.String(), "\n"), "\n"); n != 0 {
		t.Fatalf("stderr spans %d extra lines: %q", n, errb.String())
	}
	var e map[string]any
	if err := json.Unmarshal(errb.Bytes(), &e); err != nil {
		t.Fatalf("stderr is not JSON: %q", errb.String())
	}
	return e
}

// isolate points every per-user directory at a temp dir, so the tests never
// read or write the developer's real config file.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for k, v := range map[string]string{
		"HOME": home, "USERPROFILE": home,
		"XDG_CONFIG_HOME": filepath.Join(home, "config"), "XDG_CACHE_HOME": filepath.Join(home, "cache"),
		"AppData": filepath.Join(home, "appdata"), "LocalAppData": filepath.Join(home, "localappdata"),
	} {
		t.Setenv(k, v)
	}
	return home
}

func TestRootWithoutSubcommandIsUsage(t *testing.T) {
	a, _, errb := testApp(t, nil, defaultRes())
	if code := a.run(nil); code != 2 || errJSON(t, errb)["kind"] != "usage" {
		t.Fatalf("exit %d %s", code, errb)
	}
}

func TestConfigErrorOnlyBlocksAPICommands(t *testing.T) {
	a, _, errb := testApp(t, nil, defaultRes())
	a.configErr = errors.New("config.json is corrupt")
	if code := a.run([]string{"get", "me"}); code != 2 || !strings.Contains(errJSON(t, errb)["error"].(string), "cannot load the configuration") {
		t.Fatalf("exit %d %s", code, errb)
	}
	for _, args := range [][]string{{"version"}, {"commands"}} {
		if code := a.run(args); code != 0 {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
}

func TestVersion(t *testing.T) {
	a, out, _ := testApp(t, nil, defaultRes())
	if code := a.run([]string{"version"}); code != 0 || !strings.HasPrefix(out.String(), "metaads ") || !strings.Contains(out.String(), "v26.0") {
		t.Fatalf("exit %d %q", code, out)
	}
}

func TestCommandsCatalog(t *testing.T) {
	a, out, errb := testApp(t, nil, defaultRes())
	if code := a.run([]string{"commands", "--json"}); code != 0 {
		t.Fatalf("exit %d %s", code, errb)
	}
	var cat struct {
		SchemaVersion int    `json:"schema_version"`
		APIVersion    string `json:"api_version"`
		Commands      []struct {
			Command string   `json:"command"`
			Flags   []string `json:"flags"`
		} `json:"commands"`
		Guard struct {
			PostEdges map[string]string `json:"post_edges"`
			Refused   map[string]string `json:"refused"`
		} `json:"guard"`
	}
	if err := json.Unmarshal(out.Bytes(), &cat); err != nil {
		t.Fatal(err)
	}
	flags := map[string]string{}
	for _, c := range cat.Commands {
		flags[c.Command] = strings.Join(c.Flags, " ")
	}
	if cat.SchemaVersion != 1 || cat.APIVersion != "v26.0" || !strings.Contains(flags["get"], "--all") ||
		!strings.Contains(flags["post"], "--validate-only") || !strings.Contains(flags["delete"], "--force") ||
		strings.Contains(flags["get"], "--help") || cat.Guard.PostEdges["campaigns"] != "write" ||
		cat.Guard.Refused["field delete_strategy"] == "" {
		t.Fatalf("%+v", cat)
	}
}
```

`internal/cli/verbs_test.go`:

```go
package cli

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetBuildsQuery(t *testing.T) {
	g := newFakeGraph(t, func(seen) (int, string) { return 200, `{"data":[]}` })
	a, out, errb := testApp(t, g, defaultRes())
	code := a.run([]string{"get", "act/campaigns", "--fields", "name,status", "--param", "limit=5",
		"--param", `time_range={"since":"2026-09-01","until":"2026-09-24"}`})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	c := g.all()[0]
	if c.Method != "GET" || c.Path != "/v26.0/act_1/campaigns" || c.Query.Get("fields") != "name,status" ||
		c.Query.Get("limit") != "5" || c.Query.Get("time_range") != `{"since":"2026-09-01","until":"2026-09-24"}` ||
		c.Auth != "Bearer tok" || c.Query.Has("access_token") {
		t.Fatalf("%+v", c)
	}
	if out.String() != "{\"data\":[]}\n" {
		t.Fatalf("stdout %q", out)
	}
}

func TestGetRefusals(t *testing.T) {
	res := defaultRes()
	res.AdAccountID = ""
	for _, c := range []struct {
		res  bool // true: no ad account configured
		args []string
		want string
	}{
		{true, []string{"get", "act/campaigns"}, "no ad account is configured"},
		{false, []string{"get", "me", "--param", "fields=id"}, "--fields"},
		{false, []string{"get", "me", "--param", "method=post"}, "parameter method is refused"},
		{false, []string{"get", "me", "--param", "limit"}, "key=value"},
		{false, []string{"get", "me", "--param", "a=1", "--param", "a=2"}, "given twice"},
		{false, []string{"get", "act/campaigns?limit=5"}, "query string"},
	} {
		r := defaultRes()
		if c.res {
			r = res
		}
		a, _, errb := testApp(t, nil, r)
		if code := a.run(c.args); code != 2 {
			t.Fatalf("%v: exit %d", c.args, code)
		}
		if e := errJSON(t, errb); e["kind"] != "usage" || !strings.Contains(e["error"].(string), c.want) {
			t.Fatalf("%v: %v", c.args, e)
		}
	}
}

func TestPostEncodesForm(t *testing.T) {
	g := newFakeGraph(t, nil)
	a, out, errb := testApp(t, g, defaultRes())
	code := a.run([]string{"post", "act/campaigns", "--data", `{"name":"Test <1>","objective":"OUTCOME_TRAFFIC","status":"PAUSED",` +
		`"special_ad_categories":[],"daily_budget":3000,"is_adset_budget_sharing_enabled":false}`})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	c := g.all()[0]
	want := map[string]string{"name": "Test <1>", "objective": "OUTCOME_TRAFFIC", "status": "PAUSED",
		"special_ad_categories": "[]", "daily_budget": "3000", "is_adset_budget_sharing_enabled": "false"}
	for k, v := range want {
		if c.Form.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, c.Form.Get(k), v)
		}
	}
	if c.Method != "POST" || c.Path != "/v26.0/act_1/campaigns" || c.Form.Has("execution_options") ||
		!strings.HasPrefix(c.ContentType, "application/x-www-form-urlencoded") || !strings.Contains(out.String(), `"id":"1"`) {
		t.Fatalf("%+v stdout %q", c, out)
	}
}

func TestPostActiveNeedsForce(t *testing.T) {
	g := newFakeGraph(t, nil)
	a, _, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"post", "123", "--data", `{"status":"ACTIVE"}`}); code != 2 || len(g.all()) != 0 {
		t.Fatalf("exit %d, %d calls", code, len(g.all()))
	}
	msg := errJSON(t, errb)["error"].(string)
	if !strings.HasPrefix(msg, "POST v26.0/123: ") || !strings.Contains(msg, "--force") || !strings.Contains(msg, `status "ACTIVE"`) {
		t.Fatal(msg)
	}
	errb.Reset()
	if code := a.run([]string{"post", "123", "--force", "--data", `{"status":"ACTIVE"}`}); code != 0 || len(g.all()) != 1 {
		t.Fatalf("with --force: exit %d: %s", code, errb)
	}
}

func TestPostBudgetAboveCapRefusedEvenWithForce(t *testing.T) {
	g := newFakeGraph(t, nil)
	a, _, errb := testApp(t, g, defaultRes())
	code := a.run([]string{"post", "act/adsets", "--force", "--data", `{"status":"PAUSED","daily_budget":3001}`})
	if code != 2 || len(g.all()) != 0 || !strings.Contains(errJSON(t, errb)["error"].(string), "exceeds") {
		t.Fatalf("exit %d: %s", code, errb)
	}
}

func TestValidateOnly(t *testing.T) {
	g := newFakeGraph(t, func(seen) (int, string) { return 200, `{"success":true}` })
	a, _, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"post", "act/campaigns", "--validate-only", "--data", `{"name":"x","special_ad_categories":[]}`}); code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	if got := g.all()[0].Form.Get("execution_options"); got != `["validate_only"]` {
		t.Fatalf("execution_options %q", got)
	}
	errb.Reset()
	if code := a.run([]string{"post", "123", "--validate-only", "--data", `{"name":"x"}`}); code != 2 || len(g.all()) != 1 {
		t.Fatalf("update: exit %d", code)
	}
}

func TestPostPathRefusals(t *testing.T) {
	g := newFakeGraph(t, nil)
	for _, args := range [][]string{
		{"post", "act/campaigns?status=ACTIVE", "--data", `{"status":"PAUSED"}`},
		{"post", "act_2/campaigns", "--data", `{"status":"PAUSED"}`},
		{"post", "me/adaccounts", "--data", `{}`},
		{"post", "act/campaigns", "--data", `{"status":"PAUSED","status":"ACTIVE"}`},
		{"post", "act/campaigns", "--data", `[1]`},
	} {
		a, _, errb := testApp(t, g, defaultRes())
		if code := a.run(args); code != 2 || errJSON(t, errb)["kind"] != "usage" {
			t.Fatalf("%v: exit %d %s", args, code, errb)
		}
	}
	if len(g.all()) != 0 {
		t.Fatalf("%d requests were sent", len(g.all()))
	}
}

func TestFileUpload(t *testing.T) {
	p := filepath.Join(t.TempDir(), "motiv.png")
	if err := os.WriteFile(p, []byte("PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := newFakeGraph(t, func(seen) (int, string) { return 200, `{"images":{"motiv.png":{"hash":"abc"}}}` })
	a, out, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"post", "act/adimages", "--file", "filename=@" + p}); code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	c := g.all()[0]
	mt, params, err := mime.ParseMediaType(c.ContentType)
	if err != nil || mt != "multipart/form-data" {
		t.Fatalf("content type %q", c.ContentType)
	}
	form, err := multipart.NewReader(bytes.NewReader(c.Body), params["boundary"]).ReadForm(1 << 20)
	if err != nil || len(form.File["filename"]) != 1 {
		t.Fatalf("form %+v %v", form, err)
	}
	f, _ := form.File["filename"][0].Open()
	data, _ := io.ReadAll(f)
	if string(data) != "PNG" || !strings.Contains(out.String(), `"hash":"abc"`) {
		t.Fatalf("file %q stdout %q", data, out)
	}
	for _, bad := range []string{"filename", "filename=motiv.png", "=@" + p, "status=@" + p} {
		a, _, _ := testApp(t, g, defaultRes())
		if code := a.run([]string{"post", "act/adimages", "--file", bad}); code != 2 {
			t.Fatalf("--file %q: exit %d", bad, code)
		}
	}
}

func TestDelete(t *testing.T) {
	g := newFakeGraph(t, func(seen) (int, string) { return 200, `{"success":true}` })
	a, _, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"delete", "act/adimages", "--param", "hash=abc"}); code != 2 || len(g.all()) != 0 {
		t.Fatalf("without --force: exit %d", code)
	}
	errb.Reset()
	if code := a.run([]string{"delete", "act/adimages", "--param", "hash=abc", "--force"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	c := g.all()[0]
	if c.Method != "DELETE" || c.Form.Get("hash") != "abc" {
		t.Fatalf("%+v", c)
	}
	if code := a.run([]string{"delete", "act/campaigns", "--param", "delete_strategy=DELETE_ANY", "--force"}); code != 2 {
		t.Fatalf("bulk delete: exit %d", code)
	}
}

func TestAllMergesPages(t *testing.T) {
	g := newFakeGraph(t, func(s seen) (int, string) {
		if s.Query.Get("after") == "" {
			return 200, `{"data":[{"id":"1"}],"paging":{"cursors":{"before":"b0","after":"c1"},"next":"https://graph.facebook.com/v26.0/act_1/campaigns?after=c1"}}`
		}
		return 200, `{"data":[{"id":"2"}],"paging":{"cursors":{"before":"c1","after":"c2"}}}`
	})
	a, out, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"get", "act/campaigns", "--all", "--param", "after=zzz", "--param", "limit=1"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	if out.String() != "[{\"id\":\"1\"},{\"id\":\"2\"}]\n" {
		t.Fatalf("stdout %q", out)
	}
	calls := g.all()
	if len(calls) != 2 || calls[0].Query.Has("after") || calls[1].Query.Get("after") != "c1" || calls[1].Query.Get("limit") != "1" {
		t.Fatalf("%+v", calls)
	}
}

func TestAllMaxPagesIsIncomplete(t *testing.T) {
	g := newFakeGraph(t, func(seen) (int, string) {
		return 200, `{"data":[{"id":"x"}],"paging":{"cursors":{"after":"next"},"next":"https://graph.facebook.com/next"}}`
	})
	a, out, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"get", "act/campaigns", "--all", "--max-pages", "2"}); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if errJSON(t, errb)["kind"] != "incomplete" || out.String() != "[{\"id\":\"x\"},{\"id\":\"x\"}]\n" {
		t.Fatalf("stdout %q stderr %s", out, errb)
	}
}

func TestAllNeedsDataArray(t *testing.T) {
	g := newFakeGraph(t, func(seen) (int, string) { return 200, `{"id":"act_1","name":"x"}` })
	a, _, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"get", "act", "--all"}); code != 2 || !strings.Contains(errJSON(t, errb)["error"].(string), "data array") {
		t.Fatalf("exit %d %s", code, errb)
	}
}

func TestMetaErrorIsOneJSONLine(t *testing.T) {
	g := newFakeGraph(t, func(seen) (int, string) {
		return 400, `{"error":{"message":"Invalid parameter","type":"OAuthException","code":100,"fbtrace_id":"F1"}}`
	})
	a, out, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"get", "act/campaigns"}); code != 1 || out.Len() != 0 {
		t.Fatalf("exit %d stdout %q", code, out)
	}
	if e := errJSON(t, errb); e["kind"] != "validation" || e["request_id"] != "F1" || e["status"] != float64(400) {
		t.Fatalf("%v", e)
	}
}

func TestReadOnlyMode(t *testing.T) {
	g := newFakeGraph(t, nil)
	res := defaultRes()
	res.ReadOnly = true
	a, _, errb := testApp(t, g, res)
	if code := a.run([]string{"post", "act/campaigns", "--data", `{"status":"PAUSED"}`}); code != 2 || !strings.Contains(errJSON(t, errb)["error"].(string), "read-only") {
		t.Fatalf("exit %d %s", code, errb)
	}
	errb.Reset()
	if code := a.run([]string{"post", "act/campaigns", "--validate-only", "--data", `{"status":"PAUSED"}`}); code != 0 {
		t.Fatalf("validate-only: exit %d %s", code, errb)
	}
	if code := a.run([]string{"get", "me"}); code != 0 {
		t.Fatalf("get: exit %d %s", code, errb)
	}
}

func TestDataFileBOMAndUTF16(t *testing.T) {
	g := newFakeGraph(t, nil)
	dir := t.TempDir()
	bom := filepath.Join(dir, "bom.json")
	os.WriteFile(bom, append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"name":"x","status":"PAUSED"}`)...), 0o600)
	utf16 := filepath.Join(dir, "utf16.json")
	os.WriteFile(utf16, []byte{0xFF, 0xFE, '{', 0, '}', 0}, 0o600)
	a, _, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"post", "123", "--data", "@" + bom}); code != 0 || g.all()[0].Form.Get("name") != "x" {
		t.Fatalf("BOM: exit %d %s", code, errb)
	}
	errb.Reset()
	if code := a.run([]string{"post", "123", "--data", "@" + utf16}); code != 2 || !strings.Contains(errJSON(t, errb)["error"].(string), "UTF-16") {
		t.Fatalf("UTF-16: exit %d %s", code, errb)
	}
}

func TestDataFromStdinAndOutputFile(t *testing.T) {
	g := newFakeGraph(t, nil)
	a, out, errb := testApp(t, g, defaultRes())
	a.stdin = strings.NewReader(`{"name":"from stdin"}`)
	p := filepath.Join(t.TempDir(), "resp.json")
	if code := a.run([]string{"post", "123", "--data", "-", "--output", p}); code != 0 {
		t.Fatalf("exit %d %s", code, errb)
	}
	got, _ := os.ReadFile(p)
	if g.all()[0].Form.Get("name") != "from stdin" || string(got) != `{"id":"1"}` || out.Len() != 0 {
		t.Fatalf("form %v file %q stdout %q", g.all()[0].Form, got, out)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/cli/`
Expected: FAIL (build errors: `app` and the helpers it needs are undefined).

- [ ] **Step 4: Implement the entry point, app, root and shared helpers**

`cmd/metaads/main.go`:

```go
package main

import (
	"os"

	"github.com/wir-drei-digital/meta-ads-cli/internal/cli"
)

func main() { os.Exit(cli.Execute(os.Args[1:])) }
```

`internal/cli/app.go`:

```go
// Package cli builds the metaads command tree and turns process arguments
// into one guarded API call, or one merged page walk.
//
// Two rules shape everything here: stdout carries the API response and
// nothing else, and every failure leaves as one line of JSON on stderr so an
// agent can parse it without heuristics.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
	"github.com/wir-drei-digital/meta-ads-cli/internal/guard"
)

type app struct {
	client *api.Client
	res    config.Resolved
	// configErr is why the configuration could not be resolved. Only
	// version, commands, help and the config commands run without it, so a
	// person can find and repair the file; every other command reports it.
	configErr      error
	stdout, stderr io.Writer
	stdin          io.Reader
}

// Execute is the process entry point: it resolves the configuration, runs
// the command tree and returns the exit code (0 success, 1 API or network
// failure, 2 usage error).
func Execute(args []string) int {
	// Ctrl-C and SIGTERM cancel the request in flight and any retry wait.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a := &app{stdout: os.Stdout, stderr: os.Stderr, stdin: os.Stdin}
	a.configure(os.Getenv, contextSleeper(ctx))
	return a.runContext(ctx, args)
}

// configure resolves the config file and the environment and builds the
// client. A failure is kept in configErr rather than returned: prepare
// reports it for every command that needs the configuration.
func (a *app) configure(getenv func(string) string, sleep func(time.Duration)) {
	a.res, a.configErr = config.Resolve(getenv)
	a.client = &api.Client{BaseURL: a.res.APIBase, Token: a.res.AccessToken, AppSecret: a.res.AppSecret,
		ReadOnly: a.res.ReadOnly, AllowCustomBase: a.res.AllowCustomBase, Sleep: sleep}
}

// prepare is the root's persistent pre-run: it reports an unresolvable
// configuration and applies --timeout and --verbose to the client.
func (a *app) prepare(cmd *cobra.Command, args []string) error {
	if err := a.requireConfig(cmd); err != nil {
		return err
	}
	if d, err := cmd.Flags().GetDuration("timeout"); err == nil {
		a.client.Timeout = d
	}
	if v, _ := cmd.Flags().GetBool("verbose"); v {
		a.client.Verbose = a.stderr
	}
	return nil
}

// requireConfig lets only version, commands, help and the config commands
// run with an unresolvable configuration.
func (a *app) requireConfig(cmd *cobra.Command) error {
	if a.configErr == nil {
		return nil
	}
	for c := cmd; c.HasParent(); c = c.Parent() {
		if !c.Parent().HasParent() { // c is a top-level command
			switch c.Name() {
			case "version", "commands", "config", "help":
				return nil
			}
		}
	}
	return api.Usagef("cannot load the configuration: %v", a.configErr)
}

// policy is what the guard reads from the configuration.
func (a *app) policy() guard.Policy {
	return guard.Policy{ReadOnly: a.res.ReadOnly, Currency: a.res.Currency, DailyCap: a.res.DailyCap, LifetimeCap: a.res.LifetimeCap}
}

// contextSleeper returns a sleep that gives up as soon as ctx is done.
func contextSleeper(ctx context.Context) func(time.Duration) {
	return func(d time.Duration) {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
		case <-t.C:
		}
	}
}

func (a *app) run(args []string) int { return a.runContext(context.Background(), args) }

func (a *app) runContext(ctx context.Context, args []string) int {
	root := a.newRoot()
	root.SetArgs(args)
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		return a.renderError(err)
	}
	return 0
}

// renderError writes err as one line of JSON on stderr and returns the exit
// code: 2 for usage errors, 1 for everything the API or the network produced.
func (a *app) renderError(err error) int {
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		apiErr = api.Usagef("%v", err) // anything else is a cobra parse or usage failure
	}
	raw, mErr := jsonCompact(apiErr)
	if mErr != nil {
		raw, mErr = jsonCompact(&api.Error{Kind: apiErr.Kind, Message: apiErr.Message, Status: apiErr.Status, RequestID: apiErr.RequestID})
		if mErr != nil {
			raw = []byte(`{"error":"internal: cannot render error","kind":"usage","details":null}`)
		}
	}
	fmt.Fprintln(a.stderr, string(raw))
	if apiErr.Kind == api.KindUsage {
		return 2
	}
	return 1
}
```

`internal/cli/root.go`:

```go
package cli

import (
	"time"

	"github.com/spf13/cobra"
)

func (a *app) newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "metaads",
		Short: "CLI for the Meta Marketing API, built for agents: JSON in, JSON out",
		Long: "CLI for the Meta Graph and Marketing API, built for agents: JSON in, JSON out.\n\n" +
			"Every request is get, post or delete on a Graph path such as act/campaigns, where act\n" +
			"is the configured ad account. stdout carries the API response and nothing else; errors\n" +
			"are one line of JSON on stderr. Exit codes: 0 success, 1 API or network error, 2 usage\n" +
			"error or guardrail refusal.\n\n" +
			"Run `metaads commands --json` for the catalog and the guard's rules.",
		// The root names no request. Printing help and exiting 0 would read as
		// success and break the stdout contract, so it is a usage error.
		RunE:              groupRunE,
		PersistentPreRunE: a.prepare,
		SilenceErrors:     true,
		SilenceUsage:      true,
	}
	pf := root.PersistentFlags()
	pf.Bool("verbose", false, "log requests to stderr (credentials and query strings are never logged)")
	pf.Duration("timeout", 60*time.Second, "per-attempt HTTP timeout")
	root.AddCommand(a.getCommand(), a.postCommand(), a.deleteCommand(), a.versionCommand(), a.commandsCommand())
	return root
}
```

`internal/cli/util.go`:

```go
package cli

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
)

func flagString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

// groupRunE answers a namespace invoked without a subcommand: that is a
// usage error, not a success that prints help.
func groupRunE(cmd *cobra.Command, args []string) error {
	var names []string
	for _, sub := range cmd.Commands() {
		if !sub.Hidden && sub.Name() != "help" && sub.Name() != "completion" {
			names = append(names, sub.Name())
		}
	}
	return api.Usagef("%s needs a subcommand: %s", cmd.CommandPath(), strings.Join(names, ", "))
}

// jsonCompact marshals v without HTML escaping.
func jsonCompact(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
```

`internal/cli/body.go`: copy `/Users/daniel/Development/google-ads-cli/internal/cli/body.go` and change only the import path `github.com/wir-drei-digital/google-ads-cli/internal/api` to `github.com/wir-drei-digital/meta-ads-cli/internal/api`. It provides `(*app).readJSONBody(cmd) ([]byte, error)` (flag `data`: `-` for stdin, `@path` for a file, else literal; 20 MB cap; UTF-8 BOM stripped; UTF-16 refused; invalid JSON refused) and `decodeText`.

`internal/cli/output.go`: copy `/Users/daniel/Development/google-ads-cli/internal/cli/output.go` and change only the import path the same way. It provides `openOutput(path) (*outputFile, error)`, `(*outputFile).discard()`, and `(*app).writeResponse(resp *api.Response, out *outputFile) error`.

`internal/cli/version.go`: copy `/Users/daniel/Development/google-ads-cli/internal/cli/version.go`, change the doc comment's module path to `github.com/wir-drei-digital/meta-ads-cli/cmd/metaads@v0.1.0`, and replace the command with:

```go
func (a *app) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version and the default Graph API version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			info, ok := debug.ReadBuildInfo()
			fmt.Fprintf(a.stdout, "metaads %s (Graph API %s by default)\n",
				resolveVersion(Version, info, ok), config.DefaultAPIVersion)
		},
	}
}
```

(add the import `github.com/wir-drei-digital/meta-ads-cli/internal/config`).

- [ ] **Step 5: Implement get, post and delete**

`internal/cli/verbs.go`:

```go
package cli

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
	"github.com/wir-drei-digital/meta-ads-cli/internal/guard"
	"github.com/wir-drei-digital/meta-ads-cli/internal/route"
)

func (a *app) getCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <path>",
		Short: "Read a node or an edge (GET), including insights",
		Long: "Read a node or an edge of the Graph API. The node act is the configured ad account.\n\n" +
			"  metaads get act --fields name,currency,account_status,spend_cap,amount_spent\n" +
			"  metaads get act/campaigns --fields id,name,status,daily_budget --all\n" +
			"  metaads get act/insights --param level=ad --param date_preset=last_7d --fields ad_name,spend,clicks\n\n" +
			"--param values that are JSON (time_range, filtering, breakdowns) are written as JSON.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return a.runGet(cmd, args[0]) },
	}
	f := cmd.Flags()
	f.String("fields", "", "fields to return, comma-separated")
	f.StringArray("param", nil, "query parameter as key=value (repeatable)")
	f.Bool("all", false, "follow the after cursor and print one JSON array of every data entry")
	f.Int("max-pages", 100, "page limit for --all")
	f.String("output", "", "write the response body to a file instead of stdout")
	return cmd
}

func (a *app) postCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "post <path>",
		Short: "Create or change something (POST); guarded",
		Long: "Create or change something through the Graph API. --data is one JSON object and is sent\n" +
			"as form fields, the encoding Meta documents; nested values become JSON strings.\n\n" +
			"  metaads post act/campaigns --validate-only --data @campaign.json\n" +
			"  metaads post act/adimages --file filename=@motiv.png\n" +
			"  metaads post <id> --force --data '{\"status\":\"ACTIVE\"}'\n\n" +
			"Starting delivery, changing an existing budget and anything outside campaign editing need\n" +
			"--force. Budgets above the configured caps are refused whatever the flags.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return a.runPost(cmd, args[0]) },
	}
	f := cmd.Flags()
	f.String("data", "", "the fields as one JSON object: literal, @file.json, or - for stdin")
	f.StringArray("file", nil, "upload a file as field=@path (repeatable); sends multipart/form-data")
	f.Bool("validate-only", false, "let Meta validate a create without applying it (campaigns, adsets, ads, adcreatives)")
	f.Bool("force", false, "confirm a delete-, spend- or admin-class request")
	f.String("output", "", "write the response body to a file instead of stdout")
	return cmd
}

func (a *app) deleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <path>",
		Short: "Delete something (DELETE); needs --force",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return a.runDelete(cmd, args[0]) },
	}
	f := cmd.Flags()
	f.StringArray("param", nil, "form field as key=value (repeatable), such as hash=<image hash>")
	f.Bool("force", false, "confirm the delete")
	f.String("output", "", "write the response body to a file instead of stdout")
	return cmd
}

func (a *app) route(path, verb string) (route.Route, error) {
	rt, err := route.Parse(path, verb, a.res.AdAccountID, config.DefaultAPIVersion)
	if err != nil {
		return route.Route{}, api.Usagef("%v", err)
	}
	return rt, nil
}

// parseParams reads --param key=value flags. A key given twice is refused:
// the Graph API would read one of them, and the guard must know which.
func parseParams(cmd *cobra.Command) (url.Values, error) {
	raw, _ := cmd.Flags().GetStringArray("param")
	params := url.Values{}
	for _, p := range raw {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, api.Usagef("--param wants key=value, got %q", p)
		}
		if params.Has(k) {
			return nil, api.Usagef("--param %s is given twice", k)
		}
		params.Set(k, v)
	}
	return params, nil
}

// parseFiles reads --file field=@path flags.
func parseFiles(cmd *cobra.Command) ([]api.File, error) {
	raw, _ := cmd.Flags().GetStringArray("file")
	var files []api.File
	seen := map[string]bool{}
	for _, f := range raw {
		field, ref, ok := strings.Cut(f, "=")
		if !ok || field == "" || !strings.HasPrefix(ref, "@") || len(ref) < 2 {
			return nil, api.Usagef("--file wants field=@path, got %q", f)
		}
		if seen[field] {
			return nil, api.Usagef("--file %s is given twice", field)
		}
		seen[field] = true
		files = append(files, api.File{Field: field, Path: ref[1:]})
	}
	return files, nil
}

func (a *app) send(cmd *cobra.Command, req api.Request) error {
	out, err := openOutput(flagString(cmd, "output"))
	if err != nil {
		return err
	}
	defer out.discard()
	resp, err := a.client.Do(cmd.Context(), req)
	if err != nil {
		return err
	}
	return a.writeResponse(resp, out)
}

func (a *app) runGet(cmd *cobra.Command, path string) error {
	rt, err := a.route(path, http.MethodGet)
	if err != nil {
		return err
	}
	params, err := parseParams(cmd)
	if err != nil {
		return err
	}
	dec, err := guard.Check(guard.Request{Verb: http.MethodGet, Route: rt, Params: params}, a.policy())
	if err != nil {
		return api.Usagef("GET %s: %v", rt.Path(), err)
	}
	if fields := flagString(cmd, "fields"); fields != "" {
		params.Set("fields", fields)
	}
	req := api.Request{Method: http.MethodGet, Path: rt.Path(), Query: params, Class: dec.Class}
	if all, _ := cmd.Flags().GetBool("all"); all {
		out, err := openOutput(flagString(cmd, "output"))
		if err != nil {
			return err
		}
		defer out.discard()
		return a.runAll(cmd, req, out)
	}
	return a.send(cmd, req)
}

func (a *app) runPost(cmd *cobra.Command, path string) error {
	rt, err := a.route(path, http.MethodPost)
	if err != nil {
		return err
	}
	raw, err := a.readJSONBody(cmd)
	if err != nil {
		return err
	}
	body := map[string]any{}
	if raw != nil {
		if body, err = guard.ParseBody(raw); err != nil {
			return api.Usagef("--data: %v", err)
		}
	}
	files, err := parseFiles(cmd)
	if err != nil {
		return err
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Field
	}
	validateOnly, _ := cmd.Flags().GetBool("validate-only")
	force, _ := cmd.Flags().GetBool("force")
	dec, err := guard.Check(guard.Request{Verb: http.MethodPost, Route: rt, Body: body, Files: names,
		ValidateOnly: validateOnly, Force: force}, a.policy())
	if err != nil {
		return api.Usagef("POST %s: %v", rt.Path(), err)
	}
	// The form carries exactly the object the guard checked; the only
	// addition is execution_options, which --validate-only owns.
	form, err := api.EncodeForm(body)
	if err != nil {
		return api.Usagef("--data: %v", err)
	}
	if validateOnly {
		form.Set("execution_options", `["validate_only"]`)
	}
	req := api.Request{Method: http.MethodPost, Path: rt.Path(), Form: form, Class: dec.Class, ValidateOnly: validateOnly}
	if len(files) > 0 {
		mp, ct, err := api.EncodeMultipart(form, files)
		if err != nil {
			return api.Usagef("--file: %v", err)
		}
		req.Form, req.Multipart, req.ContentType = nil, mp, ct
	}
	return a.send(cmd, req)
}

func (a *app) runDelete(cmd *cobra.Command, path string) error {
	rt, err := a.route(path, http.MethodDelete)
	if err != nil {
		return err
	}
	params, err := parseParams(cmd)
	if err != nil {
		return err
	}
	force, _ := cmd.Flags().GetBool("force")
	dec, err := guard.Check(guard.Request{Verb: http.MethodDelete, Route: rt, Params: params, Force: force}, a.policy())
	if err != nil {
		return api.Usagef("DELETE %s: %v", rt.Path(), err)
	}
	return a.send(cmd, api.Request{Method: http.MethodDelete, Path: rt.Path(), Form: params, Class: dec.Class})
}
```

- [ ] **Step 6: Implement the cursor walk and the catalog**

`internal/cli/all.go`:

```go
package cli

import (
	"bytes"
	"encoding/json"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
)

// runAll follows the after cursor and writes ONE JSON array of the merged
// data entries. It repeats the request with the original parameters instead
// of following Meta's next URL, which keeps the host pinned and credentials
// out of URLs. Hitting --max-pages is not silent success: the partial array
// is written and the run ends as incomplete.
func (a *app) runAll(cmd *cobra.Command, base api.Request, out *outputFile) error {
	maxPages, _ := cmd.Flags().GetInt("max-pages")
	if maxPages < 1 {
		return api.Usagef("--max-pages must be at least 1, got %d", maxPages)
	}
	var rows []json.RawMessage
	after, complete := "", false
	for page := 0; page < maxPages; page++ {
		q := url.Values{}
		for k, v := range base.Query {
			q[k] = append([]string(nil), v...)
		}
		// A cursor in --param would start mid-stream and drop the pages
		// before it, so the walk always starts at the first page.
		q.Del("after")
		q.Del("before")
		if after != "" {
			q.Set("after", after)
		}
		req := base
		req.Query = q
		resp, err := a.client.Do(cmd.Context(), req)
		if err != nil {
			return err
		}
		var env struct {
			Data   json.RawMessage `json:"data"`
			Paging struct {
				Cursors struct {
					After string `json:"after"`
				} `json:"cursors"`
				Next string `json:"next"`
			} `json:"paging"`
		}
		if err := json.Unmarshal(resp.Body, &env); err != nil {
			return api.Usagef("--all expects a JSON object response, got: %.100s", resp.Body)
		}
		var pageRows []json.RawMessage
		if env.Data == nil || json.Unmarshal(env.Data, &pageRows) != nil {
			return api.Usagef("--all needs a response with a data array; %s returns something else", base.Path)
		}
		rows = append(rows, pageRows...)
		if env.Paging.Next == "" || env.Paging.Cursors.After == "" {
			complete = true
			break
		}
		after = env.Paging.Cursors.After
	}
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, r := range rows {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(r)
	}
	buf.WriteString("]\n")
	if err := a.writeResponse(&api.Response{Body: buf.Bytes()}, out); err != nil {
		return err
	}
	if !complete {
		return &api.Error{Kind: api.KindIncomplete, Message: "hit --max-pages before the last page; the output is partial"}
	}
	return nil
}
```

`internal/cli/catalog.go`:

```go
package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
	"github.com/wir-drei-digital/meta-ads-cli/internal/guard"
)

// catalogCommand is one command as `metaads commands --json` publishes it.
// schema_version is the contract a consumer pins to; new optional keys may
// appear under the same version.
type catalogCommand struct {
	Command string   `json:"command"`
	Use     string   `json:"use"`
	Summary string   `json:"summary"`
	Flags   []string `json:"flags,omitempty"`
}

func (a *app) commandsCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "commands",
		Short: "List every command (--json: the machine-readable catalog with the guard's rules)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmds := catalogOf(cmd.Root())
			if !asJSON {
				for _, c := range cmds {
					fmt.Fprintf(a.stdout, "%-28s %s\n", c.Command, c.Summary)
				}
				return nil
			}
			out := struct {
				SchemaVersion int              `json:"schema_version"`
				APIVersion    string           `json:"api_version"`
				Commands      []catalogCommand `json:"commands"`
				Guard         guard.Catalog    `json:"guard"`
			}{SchemaVersion: 1, APIVersion: config.DefaultAPIVersion, Commands: cmds, Guard: guard.Rules()}
			enc := json.NewEncoder(a.stdout)
			enc.SetEscapeHTML(false)
			return enc.Encode(out)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the machine-readable catalog")
	return cmd
}

// catalogOf walks the command tree and lists every leaf command with its own
// flags, so the catalog is derived from the code and cannot drift from it.
func catalogOf(root *cobra.Command) []catalogCommand {
	var out []catalogCommand
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			if sub.HasSubCommands() {
				walk(sub)
				continue
			}
			e := catalogCommand{Command: strings.TrimPrefix(sub.CommandPath(), root.Name()+" "), Use: sub.UseLine(), Summary: sub.Short}
			sub.LocalFlags().VisitAll(func(f *pflag.Flag) {
				if f.Name != "help" {
					e.Flags = append(e.Flags, "--"+f.Name)
				}
			})
			out = append(out, e)
		}
	}
	walk(root)
	return out
}
```

- [ ] **Step 7: Tidy, run the tests and build**

Run: `go mod tidy && go vet ./... && go test ./... && CGO_ENABLED=0 go build -o /dev/null ./cmd/metaads`
Expected: PASS and a successful build. `go.mod` now requires `github.com/spf13/cobra v1.10.2` and `github.com/spf13/pflag` directly.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum cmd internal/cli
git commit -m "Add the CLI core: get, post and delete with the guard, cursor paging, catalog, version

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: auth, config and init

**Files:**
- Create: `internal/auth/auth.go`, `internal/cli/authcmd.go`, `internal/cli/configcmd.go`, `internal/cli/initcmd.go`, `internal/cli/prompt.go`
- Modify: `internal/cli/app.go` (two fields), `internal/cli/root.go` (three commands)
- Test: `internal/auth/auth_test.go`, `internal/cli/configcmd_test.go`, `internal/cli/authcmd_test.go`, `internal/cli/initcmd_test.go`
- Modify: `go.mod`, `go.sum` (x/term)

**Interfaces:**
- Consumes: everything Task 4 produces, including the test helpers in `app_test.go` (`newFakeGraph`, `seen`, `defaultRes`, `testApp`, `errJSON`, `isolate`); `config.Load/Save/Path/Config/Cap/NormalizeAccountID/NormalizeAppID/NormalizeCurrency/ParseMajor/FormatMinor` (Task 1); `api.Client/Request/ClassRead/Error/Usagef/KindAuth/KindValidation` (Task 2).
- Produces:
  - `type auth.TokenInfo struct` with `AdAccounts() []string`; `auth.DebugToken(ctx, *api.Client, version, appID, secret, token string) (TokenInfo, error)`; `type auth.Exchanged struct { AccessToken string; ExpiresIn int64 }`; `auth.Exchange(ctx, *api.Client, version, appID, secret, token string) (Exchanged, error)`.
  - Commands `metaads auth status [--check]`, `metaads auth refresh`, `metaads config set|unset <key>`, `metaads config path`, `metaads init`.

- [ ] **Step 1: Add x/term**

Run: `go get golang.org/x/term@v0.45.0`

- [ ] **Step 2: Write the failing tests**

`internal/auth/auth_test.go`:

```go
package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
)

func TestDebugToken(t *testing.T) {
	var gotAuth, gotInput, gotProof, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		gotInput, gotProof = r.URL.Query().Get("input_token"), r.URL.Query().Get("appsecret_proof")
		w.Write([]byte(`{"data":{"app_id":"42","type":"SYSTEM_USER","is_valid":true,"expires_at":0,` +
			`"scopes":["ads_management","ads_read"],"granular_scopes":[{"scope":"ads_management","target_ids":["8","7"]},` +
			`{"scope":"ads_read","target_ids":["7"]},{"scope":"pages_manage_ads","target_ids":["99"]}],"user_id":"5"}}`))
	}))
	defer srv.Close()
	c := &api.Client{BaseURL: srv.URL, AllowCustomBase: true, AppSecret: "sec", Sleep: func(time.Duration) {}}
	info, err := DebugToken(context.Background(), c, "v26.0", "42", "sec", "user-tok")
	if err != nil || !info.IsValid || info.ExpiresAt != 0 || strings.Join(info.AdAccounts(), ",") != "act_7,act_8" {
		t.Fatalf("%+v %v", info, err)
	}
	if gotPath != "/v26.0/debug_token" || gotAuth != "Bearer 42|sec" || gotInput != "user-tok" || gotProof != "" {
		t.Fatalf("path %q auth %q input %q proof %q", gotPath, gotAuth, gotInput, gotProof)
	}
}

func TestExchange(t *testing.T) {
	var q map[string]string
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		q = map[string]string{}
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		if r.URL.Path != "/v26.0/oauth/access_token" {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte(`{"access_token":"new-tok","token_type":"bearer","expires_in":5184000}`))
	}))
	defer srv.Close()
	c := &api.Client{BaseURL: srv.URL, AllowCustomBase: true, Sleep: func(time.Duration) {}}
	ex, err := Exchange(context.Background(), c, "v26.0", "42", "sec", "old-tok")
	if err != nil || ex.AccessToken != "new-tok" || ex.ExpiresIn != 5184000 {
		t.Fatalf("%+v %v", ex, err)
	}
	if gotAuth != "" || q["grant_type"] != "fb_exchange_token" || q["client_id"] != "42" || q["client_secret"] != "sec" ||
		q["fb_exchange_token"] != "old-tok" || q["set_token_expires_in_60_days"] != "true" {
		t.Fatalf("auth %q query %v", gotAuth, q)
	}
}

func TestExchangeWithoutTokenInResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer srv.Close()
	c := &api.Client{BaseURL: srv.URL, AllowCustomBase: true, Sleep: func(time.Duration) {}}
	if _, err := Exchange(context.Background(), c, "v26.0", "42", "sec", "old"); err == nil {
		t.Fatal("an empty response was accepted")
	}
}
```

`internal/cli/configcmd_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
)

func runOK(t *testing.T, a *app, stdin string, args ...string) {
	t.Helper()
	a.stdin = strings.NewReader(stdin)
	if code := a.run(args); code != 0 {
		t.Fatalf("%v: exit %d %s", args, code, a.stderr)
	}
}

func TestConfigSetAndUnset(t *testing.T) {
	isolate(t)
	a, out, _ := testApp(t, nil, config.Resolved{})
	runOK(t, a, "TOKEN-123\n", "config", "set", "access-token")
	runOK(t, a, " S3CR3T \n", "config", "set", "app-secret")
	runOK(t, a, "", "config", "set", "app-id", "42")
	runOK(t, a, "", "config", "set", "ad-account-id", "act_7")
	runOK(t, a, "", "config", "set", "currency", "chf")
	runOK(t, a, "", "config", "set", "daily-budget-cap", "30")
	runOK(t, a, "", "config", "set", "lifetime-budget-cap", "300.50")
	runOK(t, a, "", "config", "set", "read-only", "true")
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.AccessToken != "TOKEN-123" || c.AppSecret != "S3CR3T" || c.AppID != "42" || c.AdAccountID != "7" || c.Currency != "CHF" ||
		c.DailyCap == nil || *c.DailyCap != (config.Cap{Minor: 3000, Currency: "CHF"}) ||
		c.LifetimeCap == nil || *c.LifetimeCap != (config.Cap{Minor: 30050, Currency: "CHF"}) || !c.ReadOnly {
		t.Fatalf("%+v", c)
	}
	if strings.Contains(out.String(), "TOKEN-123") || strings.Contains(out.String(), "S3CR3T") {
		t.Fatalf("a secret reached stdout: %q", out)
	}
	if !strings.Contains(out.String(), "daily_budget_cap = 30.00 CHF") {
		t.Fatalf("stdout %q", out)
	}
	for _, k := range []string{"access-token", "app-secret", "app-id", "ad-account-id", "currency", "daily-budget-cap", "lifetime-budget-cap", "read-only"} {
		runOK(t, a, "", "config", "unset", k)
	}
	if c, _ := config.Load(); c != (config.Config{}) {
		t.Fatalf("after unset: %+v", c)
	}
	out.Reset()
	runOK(t, a, "", "config", "unset", "access-token")
	if !strings.Contains(out.String(), "was not set") {
		t.Fatalf("stdout %q", out)
	}
}

func TestConfigSetCapNeedsCurrency(t *testing.T) {
	isolate(t)
	a, _, errb := testApp(t, nil, config.Resolved{})
	if code := a.run([]string{"config", "set", "daily-budget-cap", "30"}); code != 2 || !strings.Contains(errJSON(t, errb)["error"].(string), "currency") {
		t.Fatalf("exit %d %s", code, errb)
	}
}

func TestConfigSetCapComma(t *testing.T) {
	isolate(t)
	a, _, errb := testApp(t, nil, config.Resolved{})
	runOK(t, a, "", "config", "set", "currency", "CHF")
	if code := a.run([]string{"config", "set", "daily-budget-cap", "29,50"}); code != 2 || !strings.Contains(errJSON(t, errb)["error"].(string), "dot") {
		t.Fatalf("exit %d %s", code, errb)
	}
	if c, _ := config.Load(); c.DailyCap != nil {
		t.Fatalf("a refused cap was saved: %+v", c.DailyCap)
	}
}

func TestConfigSetCapJPY(t *testing.T) {
	isolate(t)
	a, _, _ := testApp(t, nil, config.Resolved{})
	runOK(t, a, "", "config", "set", "currency", "JPY")
	runOK(t, a, "", "config", "set", "daily-budget-cap", "3000")
	if c, _ := config.Load(); c.DailyCap == nil || *c.DailyCap != (config.Cap{Minor: 3000, Currency: "JPY"}) {
		t.Fatalf("%+v", c.DailyCap)
	}
}

func TestConfigCurrencyChangeWarnsAboutCaps(t *testing.T) {
	isolate(t)
	a, _, errb := testApp(t, nil, config.Resolved{})
	runOK(t, a, "", "config", "set", "currency", "CHF")
	runOK(t, a, "", "config", "set", "daily-budget-cap", "30")
	runOK(t, a, "", "config", "set", "currency", "EUR")
	if !strings.Contains(errb.String(), "set the caps again") {
		t.Fatalf("stderr %q", errb)
	}
}

func TestConfigSecretsNeverFromArgs(t *testing.T) {
	isolate(t)
	a, _, _ := testApp(t, nil, config.Resolved{})
	for _, k := range []string{"access-token", "app-secret"} {
		if code := a.run([]string{"config", "set", k, "on-the-command-line"}); code != 2 {
			t.Fatalf("%s from argv: exit %d", k, code)
		}
		a.stdin = strings.NewReader("  \n")
		if code := a.run([]string{"config", "set", k}); code != 2 {
			t.Fatalf("%s from empty stdin: exit %d", k, code)
		}
	}
	if c, _ := config.Load(); c.AccessToken != "" || c.AppSecret != "" {
		t.Fatalf("%+v", c)
	}
}

func TestConfigKeySuggestion(t *testing.T) {
	isolate(t)
	a, _, errb := testApp(t, nil, config.Resolved{})
	if code := a.run([]string{"config", "set", "META_ADS_ACCESS_TOKEN"}); code != 2 ||
		!strings.Contains(errJSON(t, errb)["error"].(string), "`metaads config set access-token`") {
		t.Fatalf("exit %d %s", code, errb)
	}
}

func TestConfigPath(t *testing.T) {
	isolate(t)
	a, out, _ := testApp(t, nil, config.Resolved{})
	runOK(t, a, "", "config", "path")
	p, _ := config.Path()
	if strings.TrimSpace(out.String()) != p {
		t.Fatalf("%q vs %q", out, p)
	}
}
```

`internal/cli/authcmd_test.go`:

```go
package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
)

func status(t *testing.T, out string) map[string]any {
	t.Helper()
	var s map[string]any
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("not JSON: %q", out)
	}
	return s
}

func TestAuthStatusOffline(t *testing.T) {
	a, out, _ := testApp(t, nil, defaultRes())
	if code := a.run([]string{"auth", "status"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	s := status(t, out.String())
	if s["mode"] != "token" || s["source"] != "env" || s["ad_account_id"] != "act_1" || s["currency"] != "CHF" ||
		s["daily_budget_cap"] != "30.00 CHF" || s["lifetime_budget_cap"] != "300.00 CHF" || s["app_secret"] != "missing" ||
		strings.Contains(out.String(), `"tok"`) {
		t.Fatalf("%s", out)
	}
}

func TestAuthStatusNothingConfigured(t *testing.T) {
	a, out, _ := testApp(t, nil, config.Resolved{})
	if code := a.run([]string{"auth", "status"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	s := status(t, out.String())
	missing, _ := json.Marshal(s["missing"])
	if s["mode"] != "none" || !strings.Contains(string(missing), "access_token") || !strings.Contains(s["hint"].(string), "metaads init") {
		t.Fatalf("%s", out)
	}
}

func TestAuthStatusCheckWithDebugToken(t *testing.T) {
	isolate(t)
	if err := config.Save(config.Config{AccessToken: "tok", AppID: "42", AppSecret: "sec"}); err != nil {
		t.Fatal(err)
	}
	g := newFakeGraph(t, func(s seen) (int, string) {
		if s.Path == "/v26.0/debug_token" {
			return 200, `{"data":{"is_valid":true,"expires_at":1790000000,"scopes":["ads_management"],"granular_scopes":[{"scope":"ads_management","target_ids":["1"]}]}}`
		}
		return 404, `{}`
	})
	res := defaultRes()
	res.TokenFrom, res.AppID, res.AppSecret = "config", "42", "sec"
	a, out, errb := testApp(t, g, res)
	if code := a.run([]string{"auth", "status", "--check"}); code != 0 {
		t.Fatalf("exit %d %s", code, errb)
	}
	want := time.Unix(1790000000, 0).UTC().Format(time.RFC3339)
	s := status(t, out.String())
	if s["is_valid"] != true || s["token_expires_at"] != want || s["app_secret"] != "set" {
		t.Fatalf("%s", out)
	}
	if accts, _ := json.Marshal(s["ad_accounts"]); string(accts) != `["act_1"]` {
		t.Fatalf("ad_accounts %s", accts)
	}
	if g.all()[0].Auth != "Bearer 42|sec" {
		t.Fatalf("debug_token auth %q", g.all()[0].Auth)
	}
	if c, _ := config.Load(); c.TokenExpiresAt != want {
		t.Fatalf("expiry not recorded: %+v", c)
	}
}

func TestAuthStatusCheckWithoutAppSecret(t *testing.T) {
	valid := newFakeGraph(t, func(s seen) (int, string) { return 200, `{"id":"5","name":"system user"}` })
	a, out, _ := testApp(t, valid, defaultRes())
	if code := a.run([]string{"auth", "status", "--check"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if s := status(t, out.String()); s["is_valid"] != true || !strings.Contains(s["hint"].(string), "only validity") || valid.all()[0].Path != "/v26.0/me" {
		t.Fatalf("%s", out)
	}
	invalid := newFakeGraph(t, func(s seen) (int, string) {
		return 400, `{"error":{"message":"Error validating access token","type":"OAuthException","code":190}}`
	})
	a, out, _ = testApp(t, invalid, defaultRes())
	if code := a.run([]string{"auth", "status", "--check"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if s := status(t, out.String()); s["is_valid"] != false {
		t.Fatalf("%s", out)
	}
}

func TestAuthRefresh(t *testing.T) {
	isolate(t)
	if err := config.Save(config.Config{AccessToken: "old-tok", AppID: "42", AppSecret: "sec"}); err != nil {
		t.Fatal(err)
	}
	g := newFakeGraph(t, func(s seen) (int, string) {
		if s.Path == "/v26.0/oauth/access_token" && s.Query.Get("fb_exchange_token") == "old-tok" && s.Query.Get("client_secret") == "sec" {
			return 200, `{"access_token":"new-tok","token_type":"bearer","expires_in":5184000}`
		}
		return 400, `{"error":{"message":"bad exchange","code":100}}`
	})
	res := config.Resolved{AccessToken: "old-tok", TokenFrom: "config", AppID: "42", AppSecret: "sec"}
	a, out, errb := testApp(t, g, res)
	if code := a.run([]string{"auth", "refresh"}); code != 0 {
		t.Fatalf("exit %d %s", code, errb)
	}
	c, _ := config.Load()
	if c.AccessToken != "new-tok" || c.TokenExpiresAt == "" || c.TokenExpiresAt == "never" ||
		!strings.Contains(out.String(), `"expires_at"`) || strings.Contains(out.String(), "new-tok") {
		t.Fatalf("config %+v stdout %q", c, out)
	}
}

func TestAuthRefreshRefusals(t *testing.T) {
	for _, c := range []struct {
		res  config.Resolved
		want string
	}{
		{config.Resolved{AccessToken: "t", TokenFrom: "env", AppID: "42", AppSecret: "s"}, "META_ADS_ACCESS_TOKEN"},
		{config.Resolved{}, "no stored token"},
		{config.Resolved{AccessToken: "t", TokenFrom: "config"}, "app ID and app secret"},
	} {
		a, _, errb := testApp(t, nil, c.res)
		if code := a.run([]string{"auth", "refresh"}); code != 2 || !strings.Contains(errJSON(t, errb)["error"].(string), c.want) {
			t.Fatalf("%+v: exit %d %s", c.res, code, errb)
		}
	}
}
```

`internal/cli/initcmd_test.go`:

```go
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
)

type fakePrompter struct {
	answers []string
	prompts []string
}

func (f *fakePrompter) next(prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	if len(f.answers) == 0 {
		return "", io.EOF
	}
	a := f.answers[0]
	f.answers = f.answers[1:]
	return a, nil
}

func (f *fakePrompter) Line(p string) (string, error)   { return f.next(p) }
func (f *fakePrompter) Secret(p string) (string, error) { return f.next(p) }
func (f *fakePrompter) Confirm(p string, def bool) (bool, error) {
	s, err := f.next(p)
	return s == "y", err
}

// initGraph answers the calls init makes: debug_token, me/adaccounts and
// each ad account's details.
func initGraph(t *testing.T, valid bool, accounts, spendCap string) *fakeGraph {
	return newFakeGraph(t, func(s seen) (int, string) {
		switch {
		case s.Path == "/v26.0/debug_token":
			return 200, fmt.Sprintf(`{"data":{"is_valid":%v,"expires_at":0,"scopes":["ads_management","ads_read"]}}`, valid)
		case s.Path == "/v26.0/me/adaccounts":
			return 200, accounts
		case strings.HasPrefix(s.Path, "/v26.0/act_"):
			id := strings.TrimPrefix(s.Path, "/v26.0/act_")
			return 200, fmt.Sprintf(`{"id":"act_%s","name":"Account %s","currency":"CHF","account_status":1,"spend_cap":"%s","amount_spent":"1234"}`, id, id, spendCap)
		}
		return 400, `{"error":{"message":"Unsupported get request.","code":100,"error_subcode":33}}`
	})
}

const oneAccount = `{"data":[{"account_id":"7","name":"Account 7","currency":"CHF","account_status":1}]}`

func TestInitRefusesWithoutTerminal(t *testing.T) {
	isolate(t)
	a, _, errb := testApp(t, nil, config.Resolved{})
	a.isTerminal = func() bool { return false }
	if code := a.run([]string{"init"}); code != 2 || !strings.Contains(errJSON(t, errb)["error"].(string), "needs a terminal") {
		t.Fatalf("exit %d %s", code, errb)
	}
}

func TestInitSavesEverything(t *testing.T) {
	isolate(t)
	g := initGraph(t, true, oneAccount, "100000")
	a, out, errb := testApp(t, g, config.Resolved{})
	a.isTerminal = func() bool { return true }
	a.prompt = &fakePrompter{answers: []string{"SECRET-TOKEN-1", "42", "APP-SECRET-1", "30", "300"}}
	if code := a.run([]string{"init"}); code != 0 {
		t.Fatalf("exit %d %s", code, errb)
	}
	c, _ := config.Load()
	if c.AccessToken != "SECRET-TOKEN-1" || c.AppID != "42" || c.AppSecret != "APP-SECRET-1" || c.AdAccountID != "7" ||
		c.Currency != "CHF" || c.DailyCap == nil || *c.DailyCap != (config.Cap{Minor: 3000, Currency: "CHF"}) ||
		c.LifetimeCap == nil || *c.LifetimeCap != (config.Cap{Minor: 30000, Currency: "CHF"}) || c.TokenExpiresAt != "never" {
		t.Fatalf("%+v", c)
	}
	o := out.String()
	if !strings.Contains(o, "Account spending limit: 1000.00 CHF") || strings.Contains(o, "SECRET-TOKEN-1") || strings.Contains(o, "APP-SECRET-1") {
		t.Fatalf("stdout %q", o)
	}
}

func TestInitWithoutAppID(t *testing.T) {
	isolate(t)
	g := initGraph(t, true, oneAccount, "0")
	a, out, errb := testApp(t, g, config.Resolved{})
	a.isTerminal = func() bool { return true }
	a.prompt = &fakePrompter{answers: []string{"tok", "", "30", "300"}}
	if code := a.run([]string{"init"}); code != 0 {
		t.Fatalf("exit %d %s", code, errb)
	}
	for _, s := range g.all() {
		if s.Path == "/v26.0/debug_token" {
			t.Fatal("debug_token needs the app ID and secret")
		}
	}
	if c, _ := config.Load(); c.AppID != "" || c.AdAccountID != "7" {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(out.String(), "no spending limit") {
		t.Fatalf("stdout %q", out)
	}
}

func TestInitInvalidTokenSavesNothing(t *testing.T) {
	isolate(t)
	g := initGraph(t, false, oneAccount, "0")
	a, _, errb := testApp(t, g, config.Resolved{})
	a.isTerminal = func() bool { return true }
	a.prompt = &fakePrompter{answers: []string{"tok", "42", "sec", "30", "300"}}
	if code := a.run([]string{"init"}); code != 2 || !strings.Contains(errJSON(t, errb)["error"].(string), "not valid") {
		t.Fatalf("exit %d %s", code, errb)
	}
	p, _ := config.Path()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("a config file was written: %v", err)
	}
}

func TestInitAsksWhichAccount(t *testing.T) {
	isolate(t)
	two := `{"data":[{"account_id":"7","name":"Account 7","currency":"CHF","account_status":1},` +
		`{"account_id":"8","name":"Account 8","currency":"CHF","account_status":1}]}`
	g := initGraph(t, true, two, "100000")
	a, _, errb := testApp(t, g, config.Resolved{})
	a.isTerminal = func() bool { return true }
	a.prompt = &fakePrompter{answers: []string{"tok", "42", "sec", "3", "2", "30", "300"}}
	if code := a.run([]string{"init"}); code != 0 {
		t.Fatalf("exit %d %s", code, errb)
	}
	if c, _ := config.Load(); c.AdAccountID != "8" {
		t.Fatalf("%+v", c)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/auth/ ./internal/cli/`
Expected: FAIL (build errors: `DebugToken`, `Exchange`, `a.isTerminal`, `a.prompt` and the commands are undefined).

- [ ] **Step 4: Implement the auth package**

`internal/auth/auth.go`:

```go
// Package auth talks to Meta about the credential itself: whether a token is
// valid (debug_token), and exchanging a 60-day token for a new one.
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
)

// TokenInfo is debug_token's answer about a token.
type TokenInfo struct {
	IsValid        bool     `json:"is_valid"`
	AppID          string   `json:"app_id"`
	Type           string   `json:"type"`
	ExpiresAt      int64    `json:"expires_at"` // Unix seconds; 0 means the token never expires
	Scopes         []string `json:"scopes"`
	UserID         string   `json:"user_id"`
	GranularScopes []struct {
		Scope     string   `json:"scope"`
		TargetIDs []string `json:"target_ids"`
	} `json:"granular_scopes"`
}

// AdAccounts lists the ad accounts the token reaches through ads_management
// or ads_read, as act_<id>, sorted.
func (t TokenInfo) AdAccounts() []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range t.GranularScopes {
		if g.Scope != "ads_management" && g.Scope != "ads_read" {
			continue
		}
		for _, id := range g.TargetIDs {
			a := "act_" + strings.TrimPrefix(id, "act_")
			if !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	sort.Strings(out)
	return out
}

// DebugToken asks Meta about token, authenticated with the app token
// <appID>|<secret>. The token under inspection travels as input_token, the
// parameter debug_token defines; the client never logs or prints query
// strings.
func DebugToken(ctx context.Context, c *api.Client, version, appID, secret, token string) (TokenInfo, error) {
	resp, err := c.Do(ctx, api.Request{Method: http.MethodGet, Path: version + "/debug_token",
		Query: url.Values{"input_token": {token}}, Class: api.ClassRead, Bearer: appID + "|" + secret, NoProof: true})
	if err != nil {
		return TokenInfo{}, err
	}
	var env struct {
		Data TokenInfo `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &env); err != nil {
		return TokenInfo{}, &api.Error{Kind: api.KindValidation, Message: "debug_token returned an unreadable response"}
	}
	return env.Data, nil
}

// Exchanged is a renewed token.
type Exchanged struct {
	AccessToken string
	ExpiresIn   int64 // seconds; 0 when Meta sent no expiry
}

// Exchange renews a 60-day token with grant_type=fb_exchange_token. Its
// credentials are its parameters, so the request carries no Authorization
// header and no appsecret_proof.
func Exchange(ctx context.Context, c *api.Client, version, appID, secret, token string) (Exchanged, error) {
	q := url.Values{"grant_type": {"fb_exchange_token"}, "client_id": {appID}, "client_secret": {secret},
		"fb_exchange_token": {token}, "set_token_expires_in_60_days": {"true"}}
	resp, err := c.Do(ctx, api.Request{Method: http.MethodGet, Path: version + "/oauth/access_token", Query: q,
		Class: api.ClassRead, NoAuth: true})
	if err != nil {
		return Exchanged{}, err
	}
	var r struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(resp.Body, &r); err != nil || r.AccessToken == "" {
		return Exchanged{}, &api.Error{Kind: api.KindValidation, Message: "the token exchange returned no access_token"}
	}
	return Exchanged{AccessToken: r.AccessToken, ExpiresIn: r.ExpiresIn}, nil
}
```

- [ ] **Step 5: Implement the prompt and the app seams**

`internal/cli/prompt.go`: copy `/Users/daniel/Development/google-ads-cli/internal/cli/prompt.go` verbatim, then replace `googleads init` with `metaads init` in its comments, and rewrite the two em dashes in the `isTerminal` comment as parentheses or a new sentence (this repo has no em dash in any Go file). It provides `type prompter interface { Line; Secret; Confirm }`, `newTermPrompter`, `ask(p, prompt, secret) (string, error)`, `stdioIsTerminal()` and `isTerminal(files ...*os.File)`.

In `internal/cli/app.go`, add two fields to `app`, after `stdin`:

```go
	// prompt and isTerminal are seams for `init`, the one interactive
	// command; nil in production, where init uses the real terminal.
	prompt     prompter
	isTerminal func() bool
```

In `internal/cli/root.go`, extend the `AddCommand` call:

```go
	root.AddCommand(a.getCommand(), a.postCommand(), a.deleteCommand(), a.versionCommand(), a.commandsCommand(),
		a.configCommand(), a.authCommand(), a.initCommand())
```

- [ ] **Step 6: Implement the config commands**

`internal/cli/configcmd.go`:

```go
package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
)

const configKeys = "access-token, app-secret, app-id, ad-account-id, currency, daily-budget-cap, lifetime-budget-cap and read-only"

func (a *app) configCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Manage the metaads config file", RunE: groupRunE}
	set := &cobra.Command{Use: "set", Short: "Set a config value", Args: cobra.ArbitraryArgs, RunE: keyGroupRunE("set")}
	set.AddCommand(
		a.secretSetter("access-token", "system user access token", "META_ADS_ACCESS_TOKEN", func(c *config.Config, v string) {
			c.AccessToken, c.TokenExpiresAt = v, ""
		}, "access token saved; check it with `metaads auth status --check`"),
		a.secretSetter("app-secret", "app secret", "META_ADS_APP_SECRET", func(c *config.Config, v string) { c.AppSecret = v },
			"app secret saved; requests now carry appsecret_proof"),
		a.idSetter("app-id", "Meta app ID", "META_ADS_APP_ID", config.NormalizeAppID, func(c *config.Config, v string) { c.AppID = v }, ""),
		a.idSetter("ad-account-id", "ad account (e.g. act_1234567890)", "META_ADS_AD_ACCOUNT_ID", config.NormalizeAccountID,
			func(c *config.Config, v string) { c.AdAccountID = v }, "act_"),
		&cobra.Command{
			Use:   "currency <code>",
			Short: "Set the ad account's currency (ISO code, e.g. CHF); budget caps are entered in it",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				cur, err := config.NormalizeCurrency(args[0])
				if err != nil {
					return api.Usagef("currency: %v", err)
				}
				var stale bool
				if err := a.updateConfig(func(c *config.Config) {
					c.Currency = cur
					stale = (c.DailyCap != nil && c.DailyCap.Currency != cur) || (c.LifetimeCap != nil && c.LifetimeCap.Currency != cur)
				}); err != nil {
					return err
				}
				if stale {
					fmt.Fprintf(a.stderr, "note: a budget cap was entered in another currency; budgets are refused until you set the caps again in %s\n", cur)
				}
				fmt.Fprintf(a.stdout, "currency = %s\n", cur)
				return nil
			},
		},
		a.capSetter("daily-budget-cap", "daily", func(c *config.Config, cp *config.Cap) { c.DailyCap = cp }),
		a.capSetter("lifetime-budget-cap", "lifetime", func(c *config.Config, cp *config.Cap) { c.LifetimeCap = cp }),
		&cobra.Command{
			Use:   "read-only <true|false>",
			Short: "Persist read-only mode",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				v, err := strconv.ParseBool(args[0])
				if err != nil {
					return api.Usagef("read-only wants true or false, got %q", args[0])
				}
				if err := a.updateConfig(func(c *config.Config) { c.ReadOnly = v }); err != nil {
					return err
				}
				fmt.Fprintf(a.stdout, "read_only = %v\n", v)
				return nil
			},
		},
	)

	unset := &cobra.Command{Use: "unset", Short: "Remove a config value", Args: cobra.ArbitraryArgs, RunE: keyGroupRunE("unset")}
	for _, k := range []struct {
		use, done string
		stored    func(config.Config) bool
		clear     func(*config.Config)
	}{
		{"access-token", "access token removed from this machine; it stays valid until you remove it, or the system user, in the Business Portfolio",
			func(c config.Config) bool { return c.AccessToken != "" }, func(c *config.Config) { c.AccessToken, c.TokenExpiresAt = "", "" }},
		{"app-secret", "app secret removed; requests no longer carry appsecret_proof",
			func(c config.Config) bool { return c.AppSecret != "" }, func(c *config.Config) { c.AppSecret = "" }},
		{"app-id", "app-id removed",
			func(c config.Config) bool { return c.AppID != "" }, func(c *config.Config) { c.AppID = "" }},
		{"ad-account-id", "ad-account-id removed",
			func(c config.Config) bool { return c.AdAccountID != "" }, func(c *config.Config) { c.AdAccountID = "" }},
		{"currency", "currency removed; no budget can be set until it is set again",
			func(c config.Config) bool { return c.Currency != "" }, func(c *config.Config) { c.Currency = "" }},
		{"daily-budget-cap", "daily-budget-cap removed; no daily budget can be set until a new cap is configured",
			func(c config.Config) bool { return c.DailyCap != nil }, func(c *config.Config) { c.DailyCap = nil }},
		{"lifetime-budget-cap", "lifetime-budget-cap removed; no lifetime budget can be set until a new cap is configured",
			func(c config.Config) bool { return c.LifetimeCap != nil }, func(c *config.Config) { c.LifetimeCap = nil }},
		{"read-only", "read-only removed",
			func(c config.Config) bool { return c.ReadOnly }, func(c *config.Config) { c.ReadOnly = false }},
	} {
		unset.AddCommand(&cobra.Command{
			Use: k.use, Short: "Remove " + k.use, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := config.Load()
				if err != nil {
					return api.Usagef("%v", err)
				}
				if !k.stored(c) {
					fmt.Fprintf(a.stdout, "%s was not set\n", k.use)
					return nil
				}
				k.clear(&c)
				if err := config.Save(c); err != nil {
					return api.Usagef("%v", err)
				}
				fmt.Fprintln(a.stdout, k.done)
				return nil
			},
		})
	}

	path := &cobra.Command{
		Use: "path", Short: "Print the config file location", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := config.Path()
			if err != nil {
				return api.Usagef("%v", err)
			}
			fmt.Fprintln(a.stdout, p)
			return nil
		},
	}
	cmd.AddCommand(set, unset, path)
	return cmd
}

// secretSetter reads a secret from stdin, never from the command line, which
// every process on the machine can see.
func (a *app) secretSetter(use, what, envName string, assign func(*config.Config, string), done string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: "Read the " + what + " from stdin and store it (0600)",
		Long: "Read the " + what + " from stdin and store it in the config file (0600). It never comes from\n" +
			"the command line:\n  printf '%s' \"$VALUE\" | metaads config set " + use + "\n\n" + envName + ", when set, wins.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := io.ReadAll(io.LimitReader(a.stdin, 64<<10))
			if err != nil {
				return api.Usagef("reading stdin: %v", err)
			}
			v := strings.TrimSpace(strings.TrimPrefix(string(raw), "﻿"))
			if v == "" {
				return api.Usagef("no value on stdin; usage: printf '%%s' \"$VALUE\" | metaads config set %s", use)
			}
			if strings.ContainsAny(v, " \t\r\n") {
				return api.Usagef("the %s on stdin contains whitespace; pass exactly one value", what)
			}
			if err := a.updateConfig(func(c *config.Config) { assign(c, v) }); err != nil {
				return err
			}
			a.warnEnv(envName)
			fmt.Fprintln(a.stdout, done)
			return nil
		},
	}
}

func (a *app) idSetter(use, what, envName string, normalize func(string) (string, error), assign func(*config.Config, string), prefix string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <id>",
		Short: "Save the " + what,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := normalize(args[0])
			if err != nil {
				return api.Usagef("%s: %v", use, err)
			}
			if err := a.updateConfig(func(c *config.Config) { assign(c, id) }); err != nil {
				return err
			}
			a.warnEnv(envName)
			fmt.Fprintf(a.stdout, "%s = %s%s\n", strings.ReplaceAll(use, "-", "_"), prefix, id)
			return nil
		},
	}
}

func (a *app) capSetter(use, kind string, assign func(*config.Config, *config.Cap)) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <amount>",
		Short: "Set the highest " + kind + " budget metaads may ever set, in the account currency",
		Long: "Set the " + use + ": the highest " + kind + " budget metaads accepts in any request, in the\n" +
			"configured currency (for example 30 or 29.50, a dot as the decimal separator). --force does not\n" +
			"override it, and no environment variable or flag can change it. Without it no " + kind + " budget\n" +
			"can be set at all.\n\n" +
			"Meta may spend up to 75% more than a daily budget on a single day, and at most 7 times the daily\n" +
			"budget in a week. A lifetime budget is never exceeded.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return api.Usagef("%v", err)
			}
			if c.Currency == "" {
				return api.Usagef("%s: set the currency first with `metaads config set currency <code>` (or run `metaads init`)", use)
			}
			minor, err := config.ParseMajor(args[0], c.Currency)
			if err != nil {
				return api.Usagef("%s: %v", use, err)
			}
			cp := &config.Cap{Minor: minor, Currency: c.Currency}
			assign(&c, cp)
			if err := config.Save(c); err != nil {
				return api.Usagef("%v", err)
			}
			fmt.Fprintf(a.stdout, "%s = %s (%d in Meta's minor unit)\n", strings.ReplaceAll(use, "-", "_"),
				config.FormatMinor(minor, c.Currency), minor)
			return nil
		},
	}
}

// updateConfig loads the config file, applies edit and saves it.
func (a *app) updateConfig(edit func(*config.Config)) error {
	c, err := config.Load()
	if err != nil {
		return api.Usagef("%v", err)
	}
	edit(&c)
	if err := config.Save(c); err != nil {
		return api.Usagef("%v", err)
	}
	return nil
}

// warnEnv says on stderr when an environment variable overrides what was
// just saved.
func (a *app) warnEnv(name string) {
	if a.res.FromEnv[name] {
		fmt.Fprintf(a.stderr, "note: %s is set in this environment and wins over the config file\n", name)
	}
}

// keyGroupRunE answers `config set` or `config unset` given something that is
// not one of their keys. An environment-variable name is the expected
// mistake, so it is answered with the key it plainly means.
func keyGroupRunE(verb string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			if k := suggestConfigKey(args[0]); k != "" {
				return api.Usagef("config keys are %s, not environment variable names; did you mean `metaads config %s %s`?", configKeys, verb, k)
			}
		}
		return groupRunE(cmd, args)
	}
}

func suggestConfigKey(given string) string {
	switch strings.TrimPrefix(strings.ToLower(given), "meta_ads_") {
	case "access_token", "token":
		return "access-token"
	case "app_secret", "secret":
		return "app-secret"
	case "app_id", "appid":
		return "app-id"
	case "ad_account_id", "account_id", "account":
		return "ad-account-id"
	case "read_only", "readonly":
		return "read-only"
	case "daily_budget_cap", "budget_cap":
		return "daily-budget-cap"
	case "lifetime_budget_cap":
		return "lifetime-budget-cap"
	}
	return ""
}
```

- [ ] **Step 7: Implement the auth commands**

`internal/cli/authcmd.go`:

```go
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
	"github.com/wir-drei-digital/meta-ads-cli/internal/auth"
	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
)

// authStatus is `metaads auth status`: what is configured, never a secret.
type authStatus struct {
	Mode              string   `json:"mode"` // token or none
	Source            string   `json:"source,omitempty"`
	AppID             string   `json:"app_id,omitempty"`
	AppSecret         string   `json:"app_secret"` // set or missing
	AdAccountID       string   `json:"ad_account_id,omitempty"`
	Currency          string   `json:"currency,omitempty"`
	DailyBudgetCap    string   `json:"daily_budget_cap,omitempty"`
	LifetimeBudgetCap string   `json:"lifetime_budget_cap,omitempty"`
	ReadOnly          bool     `json:"read_only"`
	TokenExpiresAt    string   `json:"token_expires_at,omitempty"`
	IsValid           *bool    `json:"is_valid,omitempty"` // --check only
	Scopes            []string `json:"scopes,omitempty"`
	AdAccounts        []string `json:"ad_accounts,omitempty"`
	Missing           []string `json:"missing,omitempty"`
	Hint              string   `json:"hint,omitempty"`
}

func (a *app) authCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Inspect and renew the configured credential", RunE: groupRunE}
	var check bool
	status := &cobra.Command{
		Use:   "status",
		Short: "Show the configured credential as one line of JSON (never prints a token or secret)",
		Long: "Show the configured credential as one line of JSON. Offline by default. With --check it asks\n" +
			"Meta: with the app ID and secret through debug_token (validity, expiry, scopes, ad accounts),\n" +
			"otherwise with a call to me (validity only).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s := a.authStatus()
			if check {
				if err := a.checkToken(cmd.Context(), &s); err != nil {
					return err
				}
			}
			enc := json.NewEncoder(a.stdout)
			enc.SetEscapeHTML(false)
			return enc.Encode(s)
		},
	}
	status.Flags().BoolVar(&check, "check", false, "ask Meta whether the token is valid, when it expires and what it reaches")
	refresh := &cobra.Command{
		Use:   "refresh",
		Short: "Renew a 60-day token and store the new one (needs the app ID and secret)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return a.refreshToken(cmd.Context()) },
	}
	cmd.AddCommand(status, refresh)
	return cmd
}

func (a *app) authStatus() authStatus {
	s := authStatus{Mode: "none", Source: a.res.TokenFrom, AppID: a.res.AppID, AppSecret: "missing",
		Currency: a.res.Currency, ReadOnly: a.res.ReadOnly, TokenExpiresAt: a.res.TokenExpiresAt}
	if a.res.AppSecret != "" {
		s.AppSecret = "set"
	}
	if a.res.AdAccountID != "" {
		s.AdAccountID = "act_" + a.res.AdAccountID
	}
	if c := a.res.DailyCap; c != nil {
		s.DailyBudgetCap = config.FormatMinor(c.Minor, c.Currency)
	}
	if c := a.res.LifetimeCap; c != nil {
		s.LifetimeBudgetCap = config.FormatMinor(c.Minor, c.Currency)
	}
	var hints []string
	if a.res.AccessToken == "" {
		s.Missing = append(s.Missing, "access_token")
		hints = append(hints, "run `metaads init`, or store a token with `metaads config set access-token` (reads stdin)")
	} else {
		s.Mode = "token"
	}
	if s.AdAccountID == "" {
		s.Missing = append(s.Missing, "ad_account_id")
		hints = append(hints, "set the ad account with `metaads config set ad-account-id <id>`")
	}
	if s.Currency == "" {
		hints = append(hints, "no currency: set it with `metaads config set currency <code>` before the budget caps")
	}
	if s.DailyBudgetCap == "" || s.LifetimeBudgetCap == "" {
		hints = append(hints, "a budget without a matching cap is refused; a person sets the caps with `metaads config set daily-budget-cap <amount>` and `metaads config set lifetime-budget-cap <amount>`")
	}
	if s.AppSecret == "missing" {
		hints = append(hints, "no app secret: requests carry no appsecret_proof; store it with `metaads config set app-secret` (reads stdin)")
	}
	s.Hint = strings.Join(hints, "; ")
	return s
}

// checkToken asks Meta about the token and fills the --check fields.
func (a *app) checkToken(ctx context.Context, s *authStatus) error {
	if a.res.AccessToken == "" {
		return api.Usagef("no access token to check; run `metaads init` or `metaads config set access-token`")
	}
	if a.res.AppID != "" && a.res.AppSecret != "" {
		info, err := auth.DebugToken(ctx, a.client, config.DefaultAPIVersion, a.res.AppID, a.res.AppSecret, a.res.AccessToken)
		if err != nil {
			return err
		}
		valid := info.IsValid
		s.IsValid, s.Scopes, s.AdAccounts = &valid, info.Scopes, info.AdAccounts()
		s.TokenExpiresAt = expiryString(info.ExpiresAt)
		return a.recordExpiry(s.TokenExpiresAt)
	}
	_, err := a.client.Do(ctx, api.Request{Method: http.MethodGet, Path: config.DefaultAPIVersion + "/me",
		Query: url.Values{"fields": {"id,name"}}, Class: api.ClassRead})
	valid := err == nil
	var e *api.Error
	if err != nil && (!errors.As(err, &e) || e.Kind != api.KindAuth) {
		return err
	}
	s.IsValid = &valid
	note := "without the app ID and secret only validity can be checked, not expiry or scopes"
	if !valid {
		note = e.Message
	}
	s.Hint = strings.TrimPrefix(s.Hint+"; "+note, "; ")
	return nil
}

func expiryString(unix int64) string {
	if unix == 0 {
		return "never"
	}
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}

// recordExpiry stores the expiry when the token comes from the config file;
// an environment token's expiry belongs to whoever sets the environment.
func (a *app) recordExpiry(exp string) error {
	if a.res.TokenFrom != "config" {
		return nil
	}
	return a.updateConfig(func(c *config.Config) { c.TokenExpiresAt = exp })
}

func (a *app) refreshToken(ctx context.Context) error {
	switch {
	case a.res.TokenFrom == "env":
		return api.Usagef("the token comes from META_ADS_ACCESS_TOKEN; renew it where that variable is set, or store the token with `metaads config set access-token` so metaads can renew it")
	case a.res.TokenFrom == "":
		return api.Usagef("no stored token to renew; run `metaads init`")
	case a.res.AppID == "" || a.res.AppSecret == "":
		return api.Usagef("renewing a token needs the app ID and app secret; set them with `metaads config set app-id <id>` and `metaads config set app-secret` (reads stdin)")
	}
	ex, err := auth.Exchange(ctx, a.client, config.DefaultAPIVersion, a.res.AppID, a.res.AppSecret, a.res.AccessToken)
	if err != nil {
		return err
	}
	exp := "never"
	if ex.ExpiresIn > 0 {
		exp = time.Now().Add(time.Duration(ex.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	if err := a.updateConfig(func(c *config.Config) { c.AccessToken, c.TokenExpiresAt = ex.AccessToken, exp }); err != nil {
		return err
	}
	return json.NewEncoder(a.stdout).Encode(map[string]string{"expires_at": exp})
}
```

- [ ] **Step 8: Implement init**

`internal/cli/initcmd.go`:

```go
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
	"github.com/wir-drei-digital/meta-ads-cli/internal/auth"
	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
)

func (a *app) initCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactive setup: token, app, ad account, budget caps (needs a terminal)",
		Long: "Interactive setup. Asks for the system user access token, the app ID and app secret, checks\n" +
			"them against Meta with read-only calls, lets you pick the ad account, shows its spending limit,\n" +
			"asks for a daily and a lifetime budget cap in the account currency, and saves everything to\n" +
			"the config file (0600). Needs a terminal: agents configure metaads with `metaads config set`\n" +
			"or the environment instead.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			isTTY := a.isTerminal
			if isTTY == nil {
				isTTY = stdioIsTerminal
			}
			if !isTTY() {
				return api.Usagef("metaads init is interactive and needs a terminal; use `metaads config set ...` instead")
			}
			p := a.prompt
			if p == nil {
				p = newTermPrompter(os.Stdin, a.stdout)
			}
			return a.runInit(cmd.Context(), p)
		},
	}
}

// adAccount is what init reads about an ad account. Meta sends spend_cap
// and amount_spent as strings of digits in the minor unit.
type adAccount struct {
	AccountID     string      `json:"account_id"`
	Name          string      `json:"name"`
	Currency      string      `json:"currency"`
	AccountStatus int         `json:"account_status"`
	SpendCap      json.Number `json:"spend_cap"`
	AmountSpent   json.Number `json:"amount_spent"`
}

func (a *app) runInit(ctx context.Context, p prompter) error {
	out := a.stdout
	fmt.Fprintln(out, "metaads setup: you need the system user's access token, and ideally the app ID and app secret.")
	for _, name := range []string{"META_ADS_ACCESS_TOKEN", "META_ADS_APP_SECRET", "META_ADS_APP_ID", "META_ADS_AD_ACCOUNT_ID", "META_ADS_READ_ONLY"} {
		if a.res.FromEnv[name] {
			fmt.Fprintf(out, "Note: %s is set in this shell and wins over what you save here.\n", name)
		}
	}
	token, err := ask(p, "System user access token (hidden): ", true)
	if err != nil {
		return api.Usagef("init: %v", err)
	}
	appID, err := p.Line("App ID (Enter to skip): ")
	if err != nil {
		return api.Usagef("init: %v", err)
	}
	if appID != "" {
		if appID, err = config.NormalizeAppID(appID); err != nil {
			return api.Usagef("init: %v", err)
		}
	}
	secret := ""
	if appID != "" {
		if secret, err = p.Secret("App secret (hidden, Enter to skip): "); err != nil {
			return api.Usagef("init: %v", err)
		}
	}
	if secret == "" {
		fmt.Fprintln(out, "Without the app secret, requests carry no appsecret_proof. Store it later with `metaads config set app-secret`.")
	}
	// Every call below is a read; read-only makes sure of it.
	client := *a.client
	client.Token, client.AppSecret, client.ReadOnly = token, secret, true
	expiry := ""
	if appID != "" && secret != "" {
		info, err := auth.DebugToken(ctx, &client, config.DefaultAPIVersion, appID, secret, token)
		if err != nil {
			return err
		}
		if !info.IsValid {
			return api.Usagef("init: Meta reports the token as not valid; generate a new one for the system user")
		}
		expiry = expiryString(info.ExpiresAt)
		fmt.Fprintf(out, "Token valid; expires: %s; scopes: %s.\n", expiry, strings.Join(info.Scopes, ", "))
	}
	var list struct {
		Data []adAccount `json:"data"`
	}
	if err := getJSON(ctx, &client, "me/adaccounts", url.Values{"fields": {"account_id,name,currency,account_status"}, "limit": {"100"}}, &list); err != nil {
		return err
	}
	if len(list.Data) == 0 {
		return api.Usagef("init: the token reaches no ad account; assign the ad account to the system user in the Business Portfolio")
	}
	acct := list.Data[0]
	if len(list.Data) > 1 {
		for i, ac := range list.Data {
			fmt.Fprintf(out, "  %d. %s (act_%s, %s)\n", i+1, ac.Name, ac.AccountID, ac.Currency)
		}
		n, err := askValid(p, out, fmt.Sprintf("Which ad account (1-%d)? ", len(list.Data)), func(s string) (int, error) {
			i, err := strconv.Atoi(s)
			if err != nil || i < 1 || i > len(list.Data) {
				return 0, fmt.Errorf("enter a number from 1 to %d", len(list.Data))
			}
			return i, nil
		})
		if err != nil {
			return err
		}
		acct = list.Data[n-1]
	}
	id, err := config.NormalizeAccountID(acct.AccountID)
	if err != nil {
		return api.Usagef("init: %v", err)
	}
	var details adAccount
	if err := getJSON(ctx, &client, "act_"+id, url.Values{"fields": {"name,currency,account_status,spend_cap,amount_spent"}}, &details); err != nil {
		return err
	}
	currency, err := config.NormalizeCurrency(details.Currency)
	if err != nil {
		return api.Usagef("init: %v", err)
	}
	fmt.Fprintf(out, "Ad account %s (act_%s), currency %s, status %s.\n", details.Name, id, currency, accountStatus(details.AccountStatus))
	if limit, _ := details.SpendCap.Int64(); limit > 0 {
		spent, _ := details.AmountSpent.Int64()
		fmt.Fprintf(out, "Account spending limit: %s, spent so far: %s.\n", config.FormatMinor(limit, currency), config.FormatMinor(spent, currency))
	} else {
		fmt.Fprintln(out, "Warning: this ad account has no spending limit. Set one in the Business Portfolio's billing settings: it is the ceiling that holds even when --force is used.")
	}
	parse := func(s string) (int64, error) { return config.ParseMajor(s, currency) }
	daily, err := askValid(p, out, fmt.Sprintf("Daily budget cap: the highest daily budget metaads may set, in %s (e.g. 30): ", currency), parse)
	if err != nil {
		return err
	}
	lifetime, err := askValid(p, out, fmt.Sprintf("Lifetime budget cap: the highest lifetime budget metaads may set, in %s (e.g. 300): ", currency), parse)
	if err != nil {
		return err
	}
	if err := a.updateConfig(func(c *config.Config) {
		c.AccessToken, c.AppID, c.AppSecret, c.AdAccountID, c.Currency = token, appID, secret, id, currency
		c.DailyCap = &config.Cap{Minor: daily, Currency: currency}
		c.LifetimeCap = &config.Cap{Minor: lifetime, Currency: currency}
		c.TokenExpiresAt = expiry
	}); err != nil {
		return err
	}
	path, _ := config.Path()
	fmt.Fprintf(out, "Saved to %s.\n", path)
	return nil
}

// getJSON reads a Graph path into v.
func getJSON(ctx context.Context, c *api.Client, path string, q url.Values, v any) error {
	resp, err := c.Do(ctx, api.Request{Method: http.MethodGet, Path: config.DefaultAPIVersion + "/" + path, Query: q, Class: api.ClassRead})
	if err != nil {
		return err
	}
	if err := json.Unmarshal(resp.Body, v); err != nil {
		return api.Usagef("init: unreadable response from %s: %.200s", path, resp.Body)
	}
	return nil
}

var accountStatuses = map[int]string{1: "active", 2: "disabled", 3: "unsettled", 7: "pending risk review",
	8: "pending settlement", 9: "in grace period", 100: "pending closure", 101: "closed"}

func accountStatus(code int) string {
	if s, ok := accountStatuses[code]; ok {
		return s
	}
	return fmt.Sprintf("code %d", code)
}

// askValid asks until parse accepts the answer, three times at most.
func askValid[T any](p prompter, out io.Writer, prompt string, parse func(string) (T, error)) (T, error) {
	var zero T
	for range 3 {
		s, err := ask(p, prompt, false)
		if err != nil {
			return zero, api.Usagef("init: %v", err)
		}
		v, err := parse(s)
		if err == nil {
			return v, nil
		}
		fmt.Fprintln(out, err)
	}
	return zero, api.Usagef("init: no valid answer after 3 attempts")
}
```

- [ ] **Step 9: Tidy, run the tests and build**

Run: `go mod tidy && go vet ./... && go test ./... && CGO_ENABLED=0 go build -o /dev/null ./cmd/metaads`
Expected: PASS and a successful build.

- [ ] **Step 10: Commit**

```bash
git add go.mod go.sum internal/auth internal/cli
git commit -m "Add auth status and refresh, config set/unset/path, and the init wizard

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: End-to-end tests, documentation, CI and release

**Files:**
- Create: `e2e/helpers_test.go`, `e2e/smoke_test.go`, `e2e/live_test.go`
- Create: `README.md`, `SECURITY.md`, `docs/agents.md`, `.goreleaser.yaml`, `.github/workflows/ci.yml`, `.github/workflows/release.yml`

**Interfaces:**
- Consumes: the finished CLI. Every command, flag, key, error kind and message quoted in the docs must match the code; check each against `go run ./cmd/metaads <cmd> --help`, `go run ./cmd/metaads commands --json` and the tests.
- Produces: the subprocess tests, the public documentation and the release pipeline.

- [ ] **Step 1: Write the end-to-end smoke test**

`e2e/helpers_test.go`:

```go
// Package e2e drives the real binary as a subprocess: argv parsing, exit
// codes and stdout and stderr separation, exactly as a user gets them.
// Everything else in the repo tests in-process.
package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "metaads")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", bin, "github.com/wir-drei-digital/meta-ads-cli/cmd/metaads").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// run executes the binary with exactly env and returns stdout, stderr and the exit code.
func run(t *testing.T, bin string, env []string, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String(), stderr.String(), code
}

// scrubbedEnv drops every META_ADS_* variable and points every per-user
// directory into a temp dir, so a developer's real token or config can never
// turn these tests green or red.
func scrubbedEnv(t *testing.T, extra ...string) []string {
	t.Helper()
	home := t.TempDir()
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "META_ADS_") {
			env = append(env, kv)
		}
	}
	env = append(env, "HOME="+home, "USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, "config"), "XDG_CACHE_HOME="+filepath.Join(home, "cache"),
		"AppData="+filepath.Join(home, "appdata"), "LocalAppData="+filepath.Join(home, "localappdata"))
	return append(env, extra...)
}

// fakeGraph plays graph.facebook.com for the smoke test.
type fakeGraph struct {
	*httptest.Server
	writes   atomic.Int32
	mu       sync.Mutex
	lastForm url.Values
}

func newFakeGraph(t *testing.T) *fakeGraph {
	t.Helper()
	g := &fakeGraph{}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		b, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer smoke-token" {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"message":"Invalid OAuth access token.","type":"OAuthException","code":190}}`))
			return
		}
		switch {
		case r.URL.Path == "/v26.0/me":
			w.Write([]byte(`{"id":"9","name":"metaads smoke user"}`))
		case r.URL.Path == "/v26.0/act_1/campaigns" && r.Method == "GET":
			if r.URL.Query().Get("after") == "" {
				w.Write([]byte(`{"data":[{"id":"101"}],"paging":{"cursors":{"after":"c1"},"next":"https://graph.facebook.com/v26.0/act_1/campaigns?after=c1"}}`))
				return
			}
			w.Write([]byte(`{"data":[{"id":"102"}],"paging":{"cursors":{"after":"c2"}}}`))
		case r.URL.Path == "/v26.0/act_1/campaigns" && r.Method == "POST":
			g.writes.Add(1)
			form, _ := url.ParseQuery(string(b))
			g.mu.Lock()
			g.lastForm = form
			g.mu.Unlock()
			if form.Get("name") == "FAIL_ME" {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":{"message":"Invalid parameter","type":"OAuthException","code":100,"error_subcode":1885621,` +
					`"error_user_title":"Budget conflict","error_user_msg":"Set the budget on one level.","fbtrace_id":"trace-smoke"}}`))
				return
			}
			w.Write([]byte(`{"id":"120"}`))
		default:
			w.WriteHeader(400)
			fmt.Fprintf(w, `{"error":{"message":"Unsupported %s request: %s","code":100,"error_subcode":33}}`, r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *fakeGraph) form() url.Values {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lastForm
}

// errLine parses the single JSON line a failure leaves on stderr.
func errLine(t *testing.T, stderr string) map[string]any {
	t.Helper()
	if strings.Count(strings.TrimSuffix(stderr, "\n"), "\n") != 0 {
		t.Fatalf("stderr is not one line: %q", stderr)
	}
	var e map[string]any
	if err := json.Unmarshal([]byte(stderr), &e); err != nil {
		t.Fatalf("stderr is not JSON: %q", stderr)
	}
	return e
}
```

`e2e/smoke_test.go`:

```go
package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSmoke(t *testing.T) {
	bin := buildBinary(t)
	g := newFakeGraph(t)
	env := scrubbedEnv(t, "META_ADS_ACCESS_TOKEN=smoke-token", "META_ADS_API_BASE="+g.URL,
		"META_ADS_ALLOW_CUSTOM_BASE=1", "META_ADS_AD_ACCOUNT_ID=act_1")
	paused := `{"name":"smoke","objective":"OUTCOME_TRAFFIC","status":"PAUSED","special_ad_categories":[]}`

	t.Run("version", func(t *testing.T) {
		out, _, code := run(t, bin, env, "", "version")
		if code != 0 || !strings.Contains(out, "Graph API v26.0") {
			t.Fatalf("exit %d out %q", code, out)
		}
	})
	t.Run("catalog", func(t *testing.T) {
		out, _, code := run(t, bin, env, "", "commands", "--json")
		var cat struct {
			SchemaVersion int `json:"schema_version"`
			Commands      []struct {
				Command string `json:"command"`
			} `json:"commands"`
		}
		if code != 0 || json.Unmarshal([]byte(out), &cat) != nil || cat.SchemaVersion != 1 || len(cat.Commands) < 10 {
			t.Fatalf("exit %d %s", code, out)
		}
	})
	t.Run("get", func(t *testing.T) {
		out, errOut, code := run(t, bin, env, "", "get", "me", "--fields", "id,name")
		if code != 0 || errOut != "" || !strings.Contains(out, `"name":"metaads smoke user"`) {
			t.Fatalf("exit %d out %q err %q", code, out, errOut)
		}
	})
	t.Run("all", func(t *testing.T) {
		out, errOut, code := run(t, bin, env, "", "get", "act/campaigns", "--all")
		if code != 0 || out != "[{\"id\":\"101\"},{\"id\":\"102\"}]\n" {
			t.Fatalf("exit %d out %q err %q", code, out, errOut)
		}
	})
	t.Run("active needs force", func(t *testing.T) {
		_, errOut, code := run(t, bin, env, "", "post", "act/campaigns", "--data", `{"name":"x","status":"ACTIVE"}`)
		if code != 2 || errLine(t, errOut)["kind"] != "usage" || g.writes.Load() != 0 {
			t.Fatalf("exit %d err %q writes %d", code, errOut, g.writes.Load())
		}
	})
	t.Run("validate-only", func(t *testing.T) {
		_, errOut, code := run(t, bin, env, "", "post", "act/campaigns", "--validate-only", "--data", `{"name":"x"}`)
		if code != 0 || g.form().Get("execution_options") != `["validate_only"]` {
			t.Fatalf("exit %d err %q form %v", code, errOut, g.form())
		}
	})
	t.Run("paused create", func(t *testing.T) {
		out, errOut, code := run(t, bin, env, "", "post", "act/campaigns", "--data", paused)
		if code != 0 || !strings.Contains(out, `"id":"120"`) || g.form().Get("special_ad_categories") != "[]" {
			t.Fatalf("exit %d out %q err %q", code, out, errOut)
		}
	})
	t.Run("meta error", func(t *testing.T) {
		_, errOut, code := run(t, bin, env, "", "post", "act/campaigns", "--data", `{"name":"FAIL_ME","status":"PAUSED"}`)
		e := errLine(t, errOut)
		if code != 1 || e["kind"] != "validation" || e["request_id"] != "trace-smoke" ||
			!strings.Contains(e["error"].(string), "1885621: Budget conflict") {
			t.Fatalf("exit %d err %v", code, e)
		}
	})
	t.Run("budget caps", func(t *testing.T) {
		budget := func(amount string) string {
			return `{"name":"b","status":"PAUSED","special_ad_categories":[],"daily_budget":` + amount + `}`
		}
		if _, errOut, code := run(t, bin, env, "", "post", "act/campaigns", "--data", budget("100")); code != 2 || !strings.Contains(errOut, "no daily-budget-cap") {
			t.Fatalf("no cap: exit %d %q", code, errOut)
		}
		for _, args := range [][]string{{"config", "set", "currency", "CHF"}, {"config", "set", "daily-budget-cap", "30"}} {
			if _, errOut, code := run(t, bin, env, "", args...); code != 0 {
				t.Fatalf("%v: exit %d %q", args, code, errOut)
			}
		}
		if _, errOut, code := run(t, bin, env, "", "post", "act/campaigns", "--force", "--data", budget("3001")); code != 2 || !strings.Contains(errOut, "exceeds") {
			t.Fatalf("above cap: exit %d %q", code, errOut)
		}
		if _, errOut, code := run(t, bin, env, "", "post", "act/campaigns", "--data", budget("3000")); code != 0 {
			t.Fatalf("at cap: exit %d %q", code, errOut)
		}
	})
	t.Run("auth status", func(t *testing.T) {
		out, _, code := run(t, bin, env, "", "auth", "status")
		if code != 0 || strings.Contains(out, "smoke-token") || !strings.Contains(out, `"mode":"token"`) {
			t.Fatalf("exit %d out %q", code, out)
		}
	})
	t.Run("no token", func(t *testing.T) {
		_, errOut, code := run(t, bin, scrubbedEnv(t, "META_ADS_AD_ACCOUNT_ID=1"), "", "get", "me")
		if code != 2 || !strings.Contains(errLine(t, errOut)["error"].(string), "no access token") {
			t.Fatalf("exit %d err %q", code, errOut)
		}
	})
}
```

`e2e/live_test.go`:

```go
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// liveCampaign is a minimal campaign without a campaign budget, so it
// declares is_adset_budget_sharing_enabled, which Meta requires then.
func liveCampaign(status string) string {
	return fmt.Sprintf(`{"name":"metaads live check %s","objective":"OUTCOME_TRAFFIC","status":%q,`+
		`"special_ad_categories":[],"is_adset_budget_sharing_enabled":false}`, time.Now().UTC().Format("20060102T150405"), status)
}

// TestLive runs against the operator's real ad account. It changes nothing
// unless META_ADS_LIVE_WRITE=1: the campaign create is sent with
// --validate-only, and the active variant is refused before it leaves.
//
//	META_ADS_LIVE=1 go test ./e2e -run TestLive -v
//	META_ADS_LIVE=1 META_ADS_LIVE_WRITE=1 go test ./e2e -run TestLive -v
func TestLive(t *testing.T) {
	if os.Getenv("META_ADS_LIVE") != "1" {
		t.Skip("set META_ADS_LIVE=1 with a configured token, ad account, currency and caps to run the live checks")
	}
	bin := buildBinary(t)
	env := os.Environ()

	out, errOut, code := run(t, bin, env, "", "auth", "status")
	var st struct {
		Mode        string `json:"mode"`
		AdAccountID string `json:"ad_account_id"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &st) != nil || st.Mode != "token" || st.AdAccountID == "" {
		t.Fatalf("configure a token and an ad account first: %s %s", out, errOut)
	}

	for _, args := range [][]string{
		{"get", "me", "--fields", "id,name"},
		{"get", "act", "--fields", "name,currency,account_status,spend_cap,amount_spent"},
		{"get", "act/campaigns", "--fields", "id,name,status,effective_status", "--all"},
		{"get", "act/insights", "--param", "date_preset=last_7d", "--fields", "spend,impressions,clicks"},
	} {
		out, errOut, code := run(t, bin, env, "", args...)
		if code != 0 {
			t.Fatalf("%v: exit %d %s", args, code, errOut)
		}
		t.Logf("%v: %.300s", args, out)
	}

	out, errOut, code = run(t, bin, env, "", "post", "act/campaigns", "--validate-only", "--data", liveCampaign("PAUSED"))
	if code != 0 || !strings.Contains(out, `"success":true`) {
		t.Fatalf("validate-only create: exit %d out %s err %s", code, out, errOut)
	}
	_, errOut, code = run(t, bin, env, "", "post", "act/campaigns", "--data", liveCampaign("ACTIVE"))
	if code != 2 || !strings.Contains(errOut, "--force") {
		t.Fatalf("an active create must be refused locally: exit %d %s", code, errOut)
	}

	if os.Getenv("META_ADS_LIVE_WRITE") != "1" {
		t.Log("set META_ADS_LIVE_WRITE=1 to also create, read back and delete a paused campaign")
		return
	}
	out, errOut, code = run(t, bin, env, "", "post", "act/campaigns", "--data", liveCampaign("PAUSED"))
	var created struct {
		ID string `json:"id"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &created) != nil || created.ID == "" {
		t.Fatalf("create: exit %d out %s err %s", code, out, errOut)
	}
	out, errOut, code = run(t, bin, env, "", "get", created.ID, "--fields", "name,status")
	if code != 0 || !strings.Contains(out, `"status":"PAUSED"`) {
		t.Fatalf("read back: exit %d out %s err %s", code, out, errOut)
	}
	out, errOut, code = run(t, bin, env, "", "delete", created.ID, "--force")
	if code != 0 || !strings.Contains(out, `"success":true`) {
		t.Fatalf("delete %s by hand: exit %d out %s err %s", created.ID, code, out, errOut)
	}
}
```

- [ ] **Step 2: Run the end-to-end tests**

Run: `go test ./e2e/ -v`
Expected: `TestSmoke` PASS with every subtest; `TestLive` SKIP.

- [ ] **Step 3: Write README.md**

Write `README.md` with these sections, in this order, filling each with the content listed. Follow the tone of `/Users/daniel/Development/google-ads-cli/README.md` if it exists, else `/Users/daniel/Development/bexio-cli/README.md` (plain, exact, no marketing), with no em dash characters.

1. **Title and summary.** `# metaads`, then: a command-line client for the Meta Marketing API (Facebook and Instagram ads), built for agents and scripts; JSON in, JSON out, one static binary; every request is `get`, `post` or `delete` on a Graph API path, so the whole API is reachable with Meta's own paths and field names; stdout carries the API response, every failure is one line of JSON on stderr; it keeps the two decisions that cost money (starting delivery, and budget amounts) with a person. Then the sentence: "metaads is not affiliated with or endorsed by Meta."
2. **Why not Meta's Ads CLI.** Two or three sentences from the spec's section of the same name: Meta's `meta ads` (Python, proprietary license, alpha) covers a curated set of commands and has no budget cap or spend gate; metaads reaches every endpoint and refuses spend it was not told to allow.
3. **Install.** `go install github.com/wir-drei-digital/meta-ads-cli/cmd/metaads@latest`, the release table (Linux amd64/arm64, macOS amd64/arm64, Windows amd64, `checksums.txt`).
4. **Setup (once).** The five operator steps from the spec's *Operator setup*: spending limit and payment method; a Business app with the Marketing API product and "Require App Secret"; a regular system user with Advertise on the ad account only, the Page with permission to create ads, and the app; a token with `ads_management`, `ads_read`, `pages_manage_ads` (60-day expiry if the portfolio requires it); `metaads init` or the `config set` commands.
5. **Authentication.** `META_ADS_ACCESS_TOKEN` versus `metaads config set access-token` (stdin); the app secret and `appsecret_proof`; the token only in the Authorization header; `auth status` and its fields, `auth status --check`, `auth refresh` for 60-day tokens (a weekly cron job is enough); revocation in the Business Portfolio. Link SECURITY.md.
6. **Usage.** Real commands:
   - `metaads get act --fields name,currency,account_status,spend_cap,amount_spent`;
   - `metaads get act/campaigns --fields id,name,status,effective_status,daily_budget,lifetime_budget --all`;
   - `metaads get act/insights --param level=ad --param date_preset=last_7d --fields ad_name,spend,impressions,clicks,actions --all`;
   - `metaads post act/adimages --file filename=@motiv.png`;
   - `metaads post act/campaigns --validate-only --data '{"name":"Test","objective":"OUTCOME_TRAFFIC","status":"PAUSED","special_ad_categories":[],"is_adset_budget_sharing_enabled":false}'`, then the same without `--validate-only`;
   - switching on: `metaads post <campaign id> --force --data '{"status":"ACTIVE"}'` (and the same for the ad set and the ad);
   - `metaads delete <id> --force`; `metaads commands --json`.
   State that budgets are whole numbers in the account currency's minor unit (CHF 30 is `3000`), that insights `spend` is a decimal string in major units, and that `act` stands for the configured ad account.
7. **Guardrails.** The class table from the spec; the field checks 1 to 7; the refused list; the owned parameters; `--validate-only` and where it is accepted; read-only mode; `--force` is a flag only; the caps (config file only, per budget not per account, a daily and a lifetime cap); Meta's overspend rule (up to 175% of the daily budget on a day, at most 7 times per week, a lifetime budget is never exceeded); the account spending limit and the Advertise task as the ceiling outside the CLI; what the CLI cannot do.
8. **Errors and exit codes.** The error line keys, the kinds table (all eleven kinds, including `output_failed`), mapping by Meta error code, the setup hints, exit codes 0/1/2, the retry policy (rate limits waited out from Meta's usage headers within 60 seconds; reads retried on server and network errors; writes never replayed).
9. **API version policy.** Default v26.0; a path can carry its own version; Marketing API versions live about 12 months and expired versions are auto-upgraded for unchanged endpoints; moving the default is a changelog review of fields the guard checks and a minor release.
10. **Development.** `make build`, `make test`, `make check`; the smoke test; `META_ADS_LIVE=1 go test ./e2e -run TestLive -v`, and `META_ADS_LIVE_WRITE=1` for the create-and-delete round trip.
11. **License.** MIT, see LICENSE.

- [ ] **Step 4: Write SECURITY.md**

````markdown
# Security

## What a system user token is

A system user access token belongs to a system user in a Meta Business Portfolio, not to a person.
It reaches exactly the assets the system user is assigned, with the tasks it holds there, and it
keeps working when people change passwords or leave. Depending on how it was generated it never
expires, or it expires after 60 days and can be renewed with `metaads auth refresh`. Anyone who has
the token can call the Marketing API as that system user, from any machine.

## Make the token useless on its own

Store the app secret too (`metaads config set app-secret`, read from stdin) and switch on "Require
App Secret" in the app's settings (App Settings, Advanced). metaads then sends `appsecret_proof`, an
HMAC of the token keyed with the secret, with every request, and Meta refuses calls without it. A
token that leaks into a log or a transcript is then not enough to call the API.

## Give the system user as little as possible

- A regular system user, not an admin system user.
- Only the ad account metaads works on, with the task Advertise. Manage would also cover billing,
  the account spending limit and permissions.
- Only the Facebook Page the ads run under, with the permission to create ads.
- One system user per purpose, so revoking one does not break the others.

## The account spending limit

metaads refuses to change the ad account's spending limit, and its budget caps and `--force` gate
are only as strong as the shell they run in: someone with shell access can pass `--force` or edit
the config file. The ceiling that holds regardless is the account spending limit, set by a person in
the Business Portfolio's billing settings. Set one before any agent works on campaigns.

## Where the credentials live

- In the environment: `META_ADS_ACCESS_TOKEN` and `META_ADS_APP_SECRET`; or
- in the config file (`metaads config path`), written with mode 0600 inside a 0700 directory.

The token and the secret are never read from the command line, which other processes on the
machine can see. `metaads auth status` never prints either.

## What metaads never does

- Send the token to a host other than graph.facebook.com, unless `META_ADS_ALLOW_CUSTOM_BASE=1` is
  set for testing.
- Follow redirects.
- Put the token into a URL. It travels in the Authorization header; debug output and error messages
  print paths without query strings.
- Log credentials.

## Revoking access

Remove the system user's token, or the system user itself, in the Business Portfolio under Users,
System users. `metaads config unset access-token` removes the local copy only.

## Reporting a vulnerability

Please report security issues privately through GitHub's security advisories for this repository,
not in a public issue.
````

- [ ] **Step 5: Write docs/agents.md**

````markdown
# metaads for agents

Paste this into the instructions of an agent that works on Meta ads through metaads.

## Contract

- Every request is `metaads get|post|delete <path>`. The path is a Graph API path, `<node>` or
  `<node>/<edge>`, optionally after a version (`v26.0/...`). The node `act` is the configured ad
  account: `act/campaigns`, `act/insights`, `act/adimages`. Object IDs are nodes too:
  `metaads get 120330000000 --fields name,status`.
- stdout carries the API response exactly as Meta sent it. A failure is one line of JSON on stderr:
  `{"kind","error","status","details","request_id"}`. Exit codes: 0 success, 1 API or network
  error, 2 usage error or guardrail refusal (nothing was sent).
- `metaads commands --json` lists every command and the guard's rules.

## Reading

```
metaads get act --fields name,currency,account_status,spend_cap,amount_spent
metaads get act/campaigns --fields id,name,status,effective_status,daily_budget,lifetime_budget --all
metaads get act/adsets --fields id,name,status,daily_budget,lifetime_budget,end_time,targeting --all
metaads get act/ads --fields id,name,status,effective_status,creative --all
metaads get act/insights --param level=ad --param date_preset=last_7d --fields ad_id,ad_name,spend,impressions,clicks,actions --all
metaads get act/insights --param level=campaign --param 'time_range={"since":"2026-09-01","until":"2026-09-24"}' --param time_increment=1 --fields campaign_name,spend,clicks
```

- `--fields` selects fields; `--param key=value` adds query parameters. Write JSON-valued ones
  (`time_range`, `filtering`, `breakdowns`) as JSON.
- `--all` follows the cursor and prints one JSON array of every `data` entry.
- Insights `spend` is a decimal string in the account currency (major units). Budgets are whole
  numbers in the minor unit.
- A large insights query can run as a report: `metaads post act/insights --data '{"level":"ad","date_preset":"last_30d","fields":"ad_name,spend"}'`
  returns `report_run_id`; poll `metaads get <report_run_id> --fields async_status,async_percent_completion`
  until `async_status` is `Job Completed`, then `metaads get <report_run_id>/insights --all`.

## Writing

- `--data` is one JSON object; metaads sends it as form fields, the encoding Meta documents (nested
  values become JSON strings). Field names are Meta's, in snake_case.
- Money: budgets are whole numbers in the account currency's minor unit. For CHF, `3000` is CHF
  30.00. Never write decimals.
- Create everything with `"status": "PAUSED"`. A create without it, or any status other than
  `PAUSED`, `ARCHIVED` or `DELETED`, needs `--force`.
- Check a create first with `--validate-only` (campaigns, ad sets, ads and ad creatives on `act`):
  Meta validates every field and creates nothing.
- Never pass `--force`, and never change a budget cap, without a person's go-ahead in this
  conversation. `--force` is required to start delivery, change an existing budget, delete, and
  call anything outside campaign editing.
- Budgets above the configured caps are refused whatever the flags. The account spending limit
  cannot be changed through metaads.
- Refused outright: budget schedules, automated rules, reserved buying, bulk deletes, batch
  requests, and the parameters metaads owns (`method`, `access_token`, `execution_options` and the
  others listed in `commands --json`).

## Worked example: a paused campaign with one image ad

1. Upload the image; the response carries its hash.

   ```
   metaads post act/adimages --file filename=@motiv.png
   ```

2. The campaign, paused, no special ad category, budgets on the ad set. Validate first, then send
   the same without `--validate-only`; the response carries the campaign ID.

   ```
   metaads post act/campaigns --validate-only --data '{"name":"Office work solved","objective":"OUTCOME_TRAFFIC","status":"PAUSED","special_ad_categories":[],"is_adset_budget_sharing_enabled":false}'
   ```

3. The ad set: a lifetime budget of CHF 300 over 14 days, paused.

   ```
   metaads post act/adsets --data '{"name":"Switzerland","campaign_id":"<campaign id>","status":"PAUSED","lifetime_budget":30000,"start_time":"2026-10-01T08:00:00+0200","end_time":"2026-10-15T08:00:00+0200","billing_event":"IMPRESSIONS","optimization_goal":"LINK_CLICKS","bid_strategy":"LOWEST_COST_WITHOUT_CAP","targeting":{"geo_locations":{"countries":["CH"]},"age_min":25}}'
   ```

4. The creative, with the image hash and the Page.

   ```
   metaads post act/adcreatives --data '{"name":"Motif 1","object_story_spec":{"page_id":"<page id>","link_data":{"image_hash":"<hash>","link":"https://example.com/check","message":"<text>","call_to_action":{"type":"LEARN_MORE"}}}}'
   ```

5. The ad, paused.

   ```
   metaads post act/ads --data '{"name":"Motif 1","adset_id":"<ad set id>","creative":{"creative_id":"<creative id>"},"status":"PAUSED"}'
   ```

6. Starting delivery is a person's decision. With their go-ahead, set `"status":"ACTIVE"` with
   `--force` on the campaign, the ad set and the ad: `metaads post <id> --force --data '{"status":"ACTIVE"}'`.

## Errors

- `usage`: refused locally, nothing was sent; the message says why.
- `validation`: Meta rejected a field; the message names Meta's code, its text and the blamed
  fields.
- `auth`: the token is invalid or expired. `forbidden`: the system user lacks a permission or the
  assignment. `not_found`: the object does not exist or is not visible.
- `rate_limited`: metaads already waited as long as Meta asked, within 60 seconds; try later.
- `outcome_unknown`: a write may or may not have landed; read the object before retrying.
- `incomplete`: `--all` hit `--max-pages`; the output is partial.
- The message may end with `hint: ...`, naming the next step for the person.
- Do not add a retry loop of your own.

## Notes for the person wiring this up

- Give agents that only report a read-only posture: `META_ADS_READ_ONLY=1` in their environment.
  `--validate-only` still works there.
- `--force` has no environment variable or config setting on purpose: every spend decision stays a
  visible flag in the transcript.
- Set the account spending limit and both caps (`metaads config set daily-budget-cap <amount>`,
  `metaads config set lifetime-budget-cap <amount>`) before any agent writes.
````

- [ ] **Step 6: Release and CI configuration**

`.goreleaser.yaml`:

```yaml
version: 2
project_name: metaads

builds:
  - main: ./cmd/metaads
    binary: metaads
    env: [CGO_ENABLED=0]
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    ignore:
      - goos: windows
        goarch: arm64
    ldflags:
      - -s -w -X github.com/wir-drei-digital/meta-ads-cli/internal/cli.Version={{.Version}}

archives:
  - formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]

checksum:
  name_template: checksums.txt
```

`.github/workflows/ci.yml`:

```yaml
name: CI
on:
  push:
    branches: [main]
  pull_request:

permissions:
  contents: read

jobs:
  test:
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version: stable
      - run: go vet ./...
      - run: go test ./...

  checks:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version: stable
      - name: staticcheck
        run: go run honnef.co/go/tools/cmd/staticcheck@latest ./...
      - name: cross-compile
        env:
          CGO_ENABLED: "0"
        run: |
          GOOS=linux  GOARCH=arm64 go build ./cmd/metaads
          GOOS=darwin GOARCH=arm64 go build ./cmd/metaads
          GOOS=windows GOARCH=amd64 go build ./cmd/metaads
```

`.github/workflows/release.yml`: copy `/Users/daniel/Development/bexio-cli/.github/workflows/release.yml` verbatim.

- [ ] **Step 7: Verify**

Run:

```bash
make check
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/metaads
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/metaads
grep -c "—" README.md SECURITY.md docs/agents.md
grep -rn "—" --include=*.go . | grep -v _test.go
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
```

Expected: `make check` passes; both builds succeed; the first grep prints `0` for each file and the second prints nothing (no em dash in any message or comment); staticcheck is silent (if it cannot be downloaded, say so in your report). Then read README.md once against the code: every command, flag, key and error kind it names must exist.

- [ ] **Step 8: Commit**

```bash
git add e2e README.md SECURITY.md docs/agents.md .goreleaser.yaml .github
git commit -m "Add end-to-end tests, README, SECURITY, agent reference, CI and release configuration

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

## After Task 6 (controller, not a subagent task)

- Whole-branch review, then push `main` to `git@github.com:wir-drei-digital/meta-ads-cli.git`.
- The live checks need the operator's Meta setup (spending limit, Business app with "Require App Secret", system user with Advertise, token, `metaads init`). Run `META_ADS_LIVE=1 go test ./e2e -run TestLive -v` once that exists, then with `META_ADS_LIVE_WRITE=1`, and settle the spec's *Open questions* with what they show.
- Tag `v0.1.0` only after the live checks pass.
