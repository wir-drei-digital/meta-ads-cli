package guard

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
	"github.com/wir-drei-digital/meta-ads-cli/internal/route"
)

func pol() Policy {
	return Policy{AdAccount: "1", Currency: "CHF", DailyCap: &config.Cap{Minor: 3000, Currency: "CHF", Account: "1"},
		LifetimeCap: &config.Cap{Minor: 30000, Currency: "CHF", Account: "1"}}
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
	eur.DailyCap = &config.Cap{Minor: 3000, Currency: "EUR", Account: "1"}
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
	for _, edge := range []string{"budget_schedules", "adrules_library", "reachfrequencypredictions", "async_batch_requests", "asyncadrequestsets"} {
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
		r.Refused["post or delete edge outside ^[a-z0-9_]+$"] == "" ||
		strings.Join(r.ValidateOnlyEdges, ",") != "adcreatives,ads,adsets,campaigns" {
		t.Fatalf("%+v", r)
	}
	r.PostEdges["campaigns"] = Admin
	if Rules().PostEdges["campaigns"] != Write {
		t.Fatal("Rules must return a copy")
	}
}

func TestFieldNames(t *testing.T) {
	const reason = "is refused: parameter names are lowercase letters, digits and underscores; " +
		"Meta could read brackets or dots as nested fields the guard does not see"
	for _, k := range []string{"campaign_spec[daily_budget]", "adset_budgets[0][daily_budget]", "daily.budget", "daily budget", "Status", ""} {
		body := `{"status":"PAUSED","` + k + `":"99999999"}`
		check(t, "body key "+k, force(post(t, "act/adsets", body)), pol(), want{err: `field "` + k + `" ` + reason})
	}
	del := Request{Verb: "DELETE", Route: rt(t, "act/campaigns", "DELETE"), Params: url.Values{"delete.strategy": {"DELETE_ANY"}}}
	check(t, "delete param delete.strategy", force(del), pol(), want{err: `parameter "delete.strategy" ` + reason})
	get := Request{Verb: "GET", Route: rt(t, "me", "GET"), Params: url.Values{"Method": {"post"}}}
	check(t, "get param Method", get, pol(), want{err: `parameter "Method" ` + reason})
	files := post(t, "act/adimages", `{}`)
	files.Files = []string{"image[0]"}
	check(t, "file image[0]", files, pol(), want{err: `--file "image[0]" ` + reason})

	allowed := `{"name":"c","objective":"OUTCOME_SALES","status":"PAUSED","special_ad_categories":[],` +
		`"is_adset_budget_sharing_enabled":false,"bid_strategy":"LOWEST_COST_WITHOUT_CAP"}`
	check(t, "plain names", post(t, "act/campaigns", allowed), pol(), want{class: Write})
	check(t, "status_option", post(t, "123/copies", `{"status_option":"PAUSED"}`), pol(), want{class: Write})
	check(t, "nested keys are not form fields", post(t, "act/ads",
		`{"status":"PAUSED","adset_spec":{"status":"PAUSED","targeting":{"Geo.Locations[0]":1}},`+
			`"creative":"{\"object_story_spec\":{\"Page Id\":\"1\"}}"}`), pol(), want{class: Write})
	if Rules().Refused["name outside "+namePattern] == "" {
		t.Fatal("the catalog must publish the name rule")
	}
}

func TestDeleteParams(t *testing.T) {
	del := func(params url.Values) Request {
		return Request{Verb: "DELETE", Route: rt(t, "123", "DELETE"), Params: params}
	}
	check(t, "budget above cap", force(del(url.Values{"daily_budget": {"99999999"}})), pol(), want{err: "daily_budget 99999999: exceeds"})
	check(t, "lifetime budget without a cap", force(del(url.Values{"lifetime_budget": {"100"}})), noCaps(pol()), want{err: "no lifetime-budget-cap is configured"})
	check(t, "amount form", force(del(url.Values{"daily_budget": {"30.00"}})), pol(), want{err: "whole number in the account currency's minor unit"})
	check(t, "nested spec above cap", force(del(url.Values{"adset_spec": {`{"status":"PAUSED","daily_budget":5000}`}})), pol(), want{err: "adset_spec.daily_budget 5000: exceeds"})
	check(t, "nested spec unparseable", force(del(url.Values{"adset_spec": {`{`}})), pol(), want{err: "adset_spec: is a string that is not a JSON object"})
	check(t, "adset_budgets above cap", force(del(url.Values{"adset_budgets": {`[{"adset_id":"9","daily_budget":4000}]`}})), pol(), want{err: "adset_budgets[0].daily_budget 4000: exceeds"})
	check(t, "buying_type", force(del(url.Values{"buying_type": {"RESERVED"}})), pol(), want{err: `buying_type "RESERVED" is refused`})
	check(t, "repeated parameter", force(del(url.Values{"daily_budget": {"100", "99999999"}})), pol(), want{err: "daily_budget is given more than once"})
	check(t, "plain parameter", force(Request{Verb: "DELETE", Route: rt(t, "act/adimages", "DELETE"), Params: url.Values{"hash": {"abc"}}}), pol(), want{class: Delete})

	d, err := Check(force(del(url.Values{"daily_budget": {"100"}})), pol())
	if err != nil || d.Class != Delete || len(d.Findings) != 2 || !strings.Contains(d.Findings[1], "daily_budget 100 changes the budget") {
		t.Fatalf("a budget within the cap on a forced delete adds a spend finding and stays delete: %+v %v", d, err)
	}
	_, err = Check(del(url.Values{"status": {"ACTIVE"}}), pol())
	if err == nil || !strings.Contains(err.Error(), "needs --force") ||
		!strings.Contains(err.Error(), "DELETE removes what the path names") || !strings.Contains(err.Error(), `status "ACTIVE"`) {
		t.Fatalf("every finding must be named: %v", err)
	}
}

func TestPostParams(t *testing.T) {
	r := post(t, "123", `{"name":"x"}`)
	r.Params = url.Values{"daily_budget": {"99999999"}}
	check(t, "post with a parameter", r, pol(), want{err: "post takes no query parameters; put every field in --data"})
	r.Params = url.Values{"method": {"delete"}}
	check(t, "post with an owned parameter", force(r), pol(), want{err: "post takes no query parameters"})
	r.Params = url.Values{}
	check(t, "post with empty parameters", r, pol(), want{class: Write})
}

func TestDeleteAccountSpendCap(t *testing.T) {
	del := func(path string) Request {
		return force(Request{Verb: "DELETE", Route: rt(t, path, "DELETE"), Params: url.Values{"spend_cap": {"1000"}}})
	}
	check(t, "spend_cap on DELETE act", del("act"), pol(), want{err: "spend_cap on the ad account is refused"})
	check(t, "spend_cap_action on DELETE act", force(Request{Verb: "DELETE", Route: rt(t, "act", "DELETE"),
		Params: url.Values{"spend_cap_action": {"reset"}}}), pol(), want{err: "spend_cap_action is refused"})
	check(t, "spend_cap on DELETE 123", del("123"), pol(), want{class: Delete})
	d, err := Check(del("123"), pol())
	if err != nil || len(d.Findings) != 2 || !strings.Contains(d.Findings[1], "spend_cap changes or removes a spending limit") {
		t.Fatalf("a campaign spend_cap on a delete is a spend finding: %+v %v", d, err)
	}
	check(t, "spend_cap on DELETE act/adimages", del("act/adimages"), pol(), want{class: Delete})
}

// POST /<id> cannot see the object type, so an existing automated rule or
// budget schedule (high-demand period) is reached with the same verb as a
// campaign. The fields that make them change budgets later are refused
// wherever they appear, whatever the flags.
func TestRuleAndScheduleFieldsRefused(t *testing.T) {
	const rules = "automated rules change budgets and status later, outside any check"
	const schedules = "high-demand periods raise a daily budget up to 8 times, outside the budget cap"
	for _, c := range []struct{ name, body, err string }{
		{"execution_spec", `{"execution_spec":{"execution_type":"CHANGE_BUDGET","execution_options":[{"field":"change_spec","value":{"amount":100,"unit":"PERCENTAGE"},"operator":"EQUAL"}]}}`,
			"execution_spec is refused: " + rules},
		{"schedule_spec", `{"schedule_spec":{"schedule_type":"SEMI_HOURLY"}}`, "schedule_spec is refused: " + rules},
		{"budget_value", `{"budget_value":800,"budget_value_type":"MULTIPLIER"}`, "budget_value is refused: " + schedules},
		{"evaluation_spec", `{"evaluation_spec":{"evaluation_type":"SCHEDULE"}}`, "evaluation_spec is refused: " + rules},
		{"budget_value_type", `{"budget_value_type":"ABSOLUTE"}`, "budget_value_type is refused: " + schedules},
	} {
		check(t, c.name, post(t, "123", c.body), pol(), want{err: c.err})
		check(t, c.name+" forced", force(post(t, "123", c.body)), pol(), want{err: c.err})
	}
	check(t, "nested", force(post(t, "act/ads", `{"status":"PAUSED","adset_spec":{"status":"PAUSED","budget_value":800}}`)), pol(),
		want{err: "adset_spec.budget_value is refused"})
	del := Request{Verb: "DELETE", Route: rt(t, "123", "DELETE"), Params: url.Values{"schedule_spec": {`{"schedule_type":"DAILY"}`}}}
	check(t, "delete parameter", force(del), pol(), want{err: "schedule_spec is refused"})
	files := post(t, "act/adimages", `{}`)
	for _, f := range []string{"budget_value", "budget_value_type", "evaluation_spec", "execution_spec", "schedule_spec"} {
		files.Files = []string{f}
		check(t, "--file "+f, force(files), pol(), want{err: "--file " + f + " is refused"})
		if Rules().Refused["field "+f] == "" || !slices.Contains(Rules().FileFieldsRefused, f) {
			t.Errorf("the catalog must publish %s as refused, also as a --file name", f)
		}
	}
}

// Every refused field is also refused as a --file name: a file part would
// carry it unread.
func TestRefusedFieldsAreInspected(t *testing.T) {
	for k := range refusedFields {
		if !slices.Contains(inspected, k) {
			t.Errorf("refused field %s is missing from inspected", k)
		}
	}
}

// asyncadrequestsets creates ads in bulk from ad_specs the guard never reads.
func TestAsyncAdRequestSetsRefused(t *testing.T) {
	body := `{"name":"bulk","ad_specs":[{"name":"a","adset_spec":{"status":"ACTIVE","daily_budget":9999999},"creative":{"creative_id":"1"},"status":"ACTIVE"}]}`
	check(t, "asyncadrequestsets", force(post(t, "act/asyncadrequestsets", body)), pol(),
		want{err: "the asyncadrequestsets edge is refused: bulk and batch requests are not supported in v1; send the requests one by one"})
	if Rules().Refused["edge asyncadrequestsets"] == "" {
		t.Fatal("the catalog must publish the asyncadrequestsets refusal")
	}
}

// Each cap belongs to the ad account it was entered for. The environment can
// point the CLI at another account; a cap never carries over to it.
func TestCapsAreBoundToTheirAccount(t *testing.T) {
	body := `{"status":"PAUSED","daily_budget":100}`
	other := pol()
	other.AdAccount = "2"
	check(t, "another account", post(t, "act/adsets", body), other,
		want{err: "daily_budget 100: the daily-budget-cap was set for act_1, but the ad account is act_2; set the cap again"})
	check(t, "another account, forced update", force(post(t, "123", body)), other, want{err: "but the ad account is act_2"})
	check(t, "another account, validate-only", validateOnly(post(t, "act/adsets", body)), other, want{err: "but the ad account is act_2"})
	check(t, "lifetime, another account", post(t, "act/adsets", `{"status":"PAUSED","lifetime_budget":100}`), other,
		want{err: "the lifetime-budget-cap was set for act_1, but the ad account is act_2"})
	check(t, "nested, another account", post(t, "act/ads", `{"status":"PAUSED","adset_spec":{"status":"PAUSED","daily_budget":100}}`), other,
		want{err: "adset_spec.daily_budget 100: the daily-budget-cap was set for act_1"})
	check(t, "delete parameter, another account", force(Request{Verb: "DELETE", Route: rt(t, "123", "DELETE"),
		Params: url.Values{"daily_budget": {"100"}}}), other, want{err: "but the ad account is act_2"})
	unbound := pol()
	unbound.DailyCap = &config.Cap{Minor: 3000, Currency: "CHF"}
	check(t, "cap without an account", post(t, "act/adsets", body), unbound, want{err: "the daily-budget-cap names no ad account; set the cap again"})
	none := pol()
	none.AdAccount = ""
	check(t, "no ad account", force(post(t, "123", body)), none, want{err: "the daily-budget-cap was set for act_1, but no ad account is configured"})
	check(t, "same account", post(t, "act/adsets", body), pol(), want{class: Write})
}
