package guard

import (
	"maps"
	"regexp"
	"sort"

	"github.com/wir-drei-digital/meta-ads-cli/internal/route"
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

const (
	accountSpendCapReason = "the account spending limit belongs to a person; change it in the Business Portfolio's billing settings"
	buyingTypeReason      = "reserved buying commits spend the budget cap cannot check; only AUCTION is allowed"
	budgetScheduleReason  = "high-demand periods raise a daily budget up to 8 times, outside the budget cap"
	adRulesReason         = "automated rules change budgets and status later, outside any check"
	batchReason           = "bulk and batch requests are not supported in v1; send the requests one by one"
)

// refusedEdges are refused whatever the flags. route.Parse holds post and
// delete edges to Meta's lowercase spelling, so this exact match cannot be
// sidestepped with capitals.
var refusedEdges = map[string]string{
	"budget_schedules":          budgetScheduleReason,
	"adrules_library":           adRulesReason,
	"reachfrequencypredictions": "reserved buying commits spend the budget cap cannot check",
	"async_batch_requests":      batchReason,
	"asyncadrequestsets":        batchReason, // creates ads in bulk from ad_specs the guard does not read
}

// refusedFields are refused wherever they appear. POST /<id> cannot see the
// object type, so an existing automated rule (execution_spec,
// evaluation_spec, schedule_spec) or budget schedule (budget_value,
// budget_value_type) would be edited like a campaign; their fields are
// refused as the edges that create them are.
var refusedFields = map[string]string{
	"budget_schedule_specs": budgetScheduleReason,
	"budget_value":          budgetScheduleReason,
	"budget_value_type":     budgetScheduleReason,
	"evaluation_spec":       adRulesReason,
	"execution_spec":        adRulesReason,
	"schedule_spec":         adRulesReason,
	"delete_strategy":       "bulk deletion removes every campaign, ad set or ad that matches a strategy",
	"spend_cap_action":      "the account spending limit belongs to a person; reset frees headroom by zeroing the amount spent",
}

// namePattern is the only shape a top-level form field name may take: the
// Graph API uses snake_case only. Keys inside nested JSON values are not
// form fields and are not held to it.
const namePattern = `^[a-z0-9_]+$`

var plainName = regexp.MustCompile(namePattern)

const (
	nameReason     = "parameter names are lowercase letters, digits and underscores; Meta could read brackets or dots as nested fields the guard does not see"
	repeatedReason = "the guard and Meta could read different values"
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
var inspected = []string{"adset_budgets", "adset_spec", "budget_schedule_specs", "budget_value", "budget_value_type",
	"buying_type", "campaign_spec", "configured_status", "daily_budget", "delete_strategy", "evaluation_spec",
	"execution_spec", "lifetime_budget", "schedule_spec", "spend_cap", "spend_cap_action", "status", "status_option"}

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
		"field spend_cap on the ad account":                     accountSpendCapReason,
		"field buying_type other than AUCTION":                  buyingTypeReason,
		"name outside " + namePattern:                           nameReason,
		"delete parameter given more than once":                 repeatedReason,
		"post or delete edge outside " + route.WriteEdgePattern: route.WriteEdgeReason,
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
