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
		c.DailyCap == nil || *c.DailyCap != (config.Cap{Minor: 3000, Currency: "CHF", Account: "7"}) ||
		c.LifetimeCap == nil || *c.LifetimeCap != (config.Cap{Minor: 30050, Currency: "CHF", Account: "7"}) || !c.ReadOnly {
		t.Fatalf("%+v", c)
	}
	if strings.Contains(out.String(), "TOKEN-123") || strings.Contains(out.String(), "S3CR3T") {
		t.Fatalf("a secret reached stdout: %q", out)
	}
	if !strings.Contains(out.String(), "daily_budget_cap = 30.00 CHF for act_7") {
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
	runOK(t, a, "", "config", "set", "ad-account-id", "act_7")
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
	runOK(t, a, "", "config", "set", "ad-account-id", "7")
	runOK(t, a, "", "config", "set", "currency", "JPY")
	runOK(t, a, "", "config", "set", "daily-budget-cap", "3000")
	if c, _ := config.Load(); c.DailyCap == nil || *c.DailyCap != (config.Cap{Minor: 3000, Currency: "JPY", Account: "7"}) {
		t.Fatalf("%+v", c.DailyCap)
	}
}

func TestConfigCurrencyChangeWarnsAboutCaps(t *testing.T) {
	isolate(t)
	a, _, errb := testApp(t, nil, config.Resolved{})
	runOK(t, a, "", "config", "set", "ad-account-id", "act_7")
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

// A secret given on argv is refused without repeating it: stderr may be
// logged, and the refusal must not copy the value there.
func TestConfigSecretFromArgsIsNotEchoed(t *testing.T) {
	isolate(t)
	a, _, errb := testApp(t, nil, config.Resolved{})
	for _, k := range []string{"access-token", "app-secret"} {
		errb.Reset()
		if code := a.run([]string{"config", "set", k, "SECRET-ON-ARGV"}); code != 2 || strings.Contains(errb.String(), "SECRET-ON-ARGV") ||
			!strings.Contains(errJSON(t, errb)["error"].(string), "stdin") {
			t.Fatalf("%s: exit %d %s", k, code, errb)
		}
	}
}

// A cap is bound to an ad account from the config file. The environment's
// account is what an agent can change, so it cannot be what a cap is set for.
func TestConfigSetCapNeedsAdAccountInFile(t *testing.T) {
	isolate(t)
	res := config.Resolved{AdAccountID: "7", FromEnv: map[string]bool{"META_ADS_AD_ACCOUNT_ID": true}}
	a, _, errb := testApp(t, nil, res)
	runOK(t, a, "", "config", "set", "currency", "CHF")
	for _, k := range []string{"daily-budget-cap", "lifetime-budget-cap"} {
		errb.Reset()
		if code := a.run([]string{"config", "set", k, "30"}); code != 2 ||
			!strings.Contains(errJSON(t, errb)["error"].(string), "set the ad account first with `metaads config set ad-account-id <id>`") {
			t.Fatalf("%s: exit %d %s", k, code, errb)
		}
	}
	if c, _ := config.Load(); c.DailyCap != nil || c.LifetimeCap != nil {
		t.Fatalf("a cap without an ad account was saved: %+v", c)
	}
}

func TestConfigAdAccountChangeWarnsAboutCaps(t *testing.T) {
	isolate(t)
	a, _, errb := testApp(t, nil, config.Resolved{})
	runOK(t, a, "", "config", "set", "ad-account-id", "act_7")
	runOK(t, a, "", "config", "set", "currency", "CHF")
	runOK(t, a, "", "config", "set", "lifetime-budget-cap", "300")
	runOK(t, a, "", "config", "set", "ad-account-id", "7")
	if errb.Len() != 0 {
		t.Fatalf("the same account needs no note: %q", errb)
	}
	runOK(t, a, "", "config", "set", "ad-account-id", "act_8")
	if !strings.Contains(errb.String(), "budgets are refused until you set the caps again for act_8") {
		t.Fatalf("stderr %q", errb)
	}
}

// cobra's own flag errors quote the argument ("unknown shorthand flag: 'S'
// in -SECRETVALUE"); for the secret setters that argument can be the secret.
func TestConfigSecretSetterFlagErrorIsNotEchoed(t *testing.T) {
	isolate(t)
	a, _, errb := testApp(t, nil, config.Resolved{})
	for _, k := range []string{"access-token", "app-secret"} {
		for _, arg := range []string{"-SECRETVALUE", "--SECRETVALUE", "--SECRETVALUE=x", "--timeout=SECRETVALUE"} {
			errb.Reset()
			if code := a.run([]string{"config", "set", k, arg}); code != 2 || strings.Contains(errb.String(), "SECRETVALUE") ||
				!strings.Contains(errJSON(t, errb)["error"].(string), "config set "+k+" takes no flags or arguments; pipe the value on stdin") {
				t.Fatalf("%s %s: exit %d %s", k, arg, code, errb)
			}
		}
	}
	if c, _ := config.Load(); c.AccessToken != "" || c.AppSecret != "" {
		t.Fatalf("%+v", c)
	}
}
