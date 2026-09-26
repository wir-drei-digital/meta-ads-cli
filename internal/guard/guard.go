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
