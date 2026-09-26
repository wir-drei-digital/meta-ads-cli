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

func force(r Request) Request        { r.Force = true; return r }
func validateOnly(r Request) Request { r.ValidateOnly = true; return r }
func readOnly(p Policy) Policy       { p.ReadOnly = true; return p }
func noCaps(p Policy) Policy         { p.DailyCap, p.LifetimeCap = nil, nil; return p }

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
