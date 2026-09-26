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
