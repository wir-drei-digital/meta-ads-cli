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
		// The cap is bound to the ad account in the config file, not to the
		// one META_ADS_AD_ACCOUNT_ID names.
		if _, errOut, code := run(t, bin, env, "", "config", "set", "currency", "CHF"); code != 0 {
			t.Fatalf("currency: exit %d %q", code, errOut)
		}
		if _, errOut, code := run(t, bin, env, "", "config", "set", "daily-budget-cap", "30"); code != 2 || !strings.Contains(errOut, "set the ad account first") {
			t.Fatalf("cap without an ad account in the file: exit %d %q", code, errOut)
		}
		for _, args := range [][]string{{"config", "set", "ad-account-id", "act_1"}, {"config", "set", "daily-budget-cap", "30"}} {
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
		writes := g.writes.Load()
		other := append(append([]string(nil), env...), "META_ADS_AD_ACCOUNT_ID=act_2")
		if _, errOut, code := run(t, bin, other, "", "post", "act/campaigns", "--data", budget("100")); code != 2 ||
			!strings.Contains(errOut, "was set for act_1, but the ad account is act_2") || g.writes.Load() != writes {
			t.Fatalf("another account from the environment: exit %d %q", code, errOut)
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
