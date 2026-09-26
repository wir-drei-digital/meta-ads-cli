package cli

import (
	"encoding/json"
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
		c.Currency != "CHF" || c.DailyCap == nil || *c.DailyCap != (config.Cap{Minor: 3000, Currency: "CHF", Account: "7"}) ||
		c.LifetimeCap == nil || *c.LifetimeCap != (config.Cap{Minor: 30000, Currency: "CHF", Account: "7"}) || c.TokenExpiresAt != "never" {
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
	if c, _ := config.Load(); c.AdAccountID != "8" || c.DailyCap == nil || c.DailyCap.Account != "8" || c.LifetimeCap == nil || c.LifetimeCap.Account != "8" {
		t.Fatalf("%+v", c)
	}
}

// An answer pasted into the echoed App ID prompt may be a secret; the
// refusal must not repeat it on stderr, which init does not require to be a
// terminal.
func TestInitBadAppIDIsNotEchoed(t *testing.T) {
	isolate(t)
	a, _, errb := testApp(t, nil, config.Resolved{})
	a.isTerminal = func() bool { return true }
	a.prompt = &fakePrompter{answers: []string{"tok", "PASTED-APP-SECRET"}}
	if code := a.run([]string{"init"}); code != 2 || strings.Contains(errb.String(), "PASTED-APP-SECRET") ||
		!strings.Contains(errJSON(t, errb)["error"].(string), "app ID") {
		t.Fatalf("exit %d %s", code, errb)
	}
}

// Meta sometimes sends spend_cap and amount_spent as empty strings; that
// means no limit and nothing spent, not an unreadable account.
func TestInitEmptySpendCap(t *testing.T) {
	isolate(t)
	g := newFakeGraph(t, func(s seen) (int, string) {
		switch s.Path {
		case "/v26.0/me/adaccounts":
			return 200, oneAccount
		case "/v26.0/act_7":
			return 200, `{"id":"act_7","name":"Account 7","currency":"CHF","account_status":1,"spend_cap":"","amount_spent":""}`
		}
		return 400, `{"error":{"message":"Unsupported get request.","code":100,"error_subcode":33}}`
	})
	a, out, errb := testApp(t, g, config.Resolved{})
	a.isTerminal = func() bool { return true }
	a.prompt = &fakePrompter{answers: []string{"tok", "", "30", "300"}}
	if code := a.run([]string{"init"}); code != 0 {
		t.Fatalf("exit %d %s", code, errb)
	}
	if c, _ := config.Load(); c.AdAccountID != "7" || c.DailyCap == nil || c.LifetimeCap == nil {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(out.String(), "no spending limit") {
		t.Fatalf("stdout %q", out)
	}
}

func TestMinorAmount(t *testing.T) {
	for _, c := range []struct {
		in   string
		want minorAmount
	}{{`""`, 0}, {`null`, 0}, {`"100000"`, 100000}, {`100000`, 100000}} {
		var m minorAmount
		if err := json.Unmarshal([]byte(c.in), &m); err != nil || m != c.want {
			t.Fatalf("%s: %d %v", c.in, m, err)
		}
	}
	for _, in := range []string{`"abc"`, `"-5"`, `1.5`, `true`} {
		var m minorAmount
		if err := json.Unmarshal([]byte(in), &m); err == nil {
			t.Fatalf("%s was accepted as %d", in, m)
		}
	}
}
