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
