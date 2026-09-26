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
