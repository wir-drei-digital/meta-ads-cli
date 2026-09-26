// Package route parses the Graph API paths the CLI accepts:
// [vNN.N/]<node>[/<edge>], where the node "act" stands for the configured ad
// account. Everything the CLI sends besides the path comes from flags, so a
// path carries no query string, fragment, escape or whitespace: the guard
// must see every field that is sent.
package route

import (
	"fmt"
	"regexp"
	"strings"
)

// Route is a parsed Graph API path.
type Route struct {
	Version string // "v26.0"
	Node    string // "act_123", "120330000000", "me", ...
	Edge    string // "" when the request addresses the node itself
}

// Path is the request path relative to the API base: "v26.0/act_123/campaigns".
func (r Route) Path() string {
	p := r.Version + "/" + r.Node
	if r.Edge != "" {
		p += "/" + r.Edge
	}
	return p
}

// IsAccount reports whether the node is an ad account (act_<digits>).
func (r Route) IsAccount() bool { return accountRe.MatchString(r.Node) }

var (
	versionRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+$`)
	segmentRe = regexp.MustCompile(`^[A-Za-z0-9_:-]+$`)
	accountRe = regexp.MustCompile(`^act_[0-9]+$`)
	objectRe  = regexp.MustCompile(`^[0-9]+$`)
)

// Parse reads path for verb (GET, POST or DELETE). account is the configured
// ad account's digits, "" when none is configured; defaultVersion is used
// when the path does not start with its own version.
//
// GET accepts any node. POST and DELETE need an ad account or an object ID
// as the node, and an ad account must be the configured one: writes never
// reach another account, whose currency the budget caps know nothing about.
func Parse(path, verb, account, defaultVersion string) (Route, error) {
	p := strings.TrimPrefix(strings.TrimSpace(path), "/")
	if p == "" {
		return Route{}, fmt.Errorf("a path is required, such as act/campaigns")
	}
	if strings.ContainsAny(p, "?#%\\ \t\r\n") {
		return Route{}, fmt.Errorf("path %q contains a query string, fragment, escape or whitespace; pass fields with --fields, --param or --data", path)
	}
	segs := strings.Split(p, "/")
	r := Route{Version: defaultVersion}
	if versionRe.MatchString(segs[0]) {
		r.Version, segs = segs[0], segs[1:]
	}
	if len(segs) == 0 || len(segs) > 2 {
		return Route{}, fmt.Errorf("path %q must be <node>/<edge> or <node>, optionally after a version such as v26.0", path)
	}
	for _, s := range segs {
		if !segmentRe.MatchString(s) {
			return Route{}, fmt.Errorf("path %q has an empty or invalid segment %q", path, s)
		}
	}
	r.Node = segs[0]
	if len(segs) == 2 {
		r.Edge = segs[1]
	}
	if r.Node == "act" {
		if account == "" {
			return Route{}, fmt.Errorf("the path uses act, but no ad account is configured; run `metaads config set ad-account-id <id>` or set META_ADS_AD_ACCOUNT_ID")
		}
		r.Node = "act_" + account
	}
	if verb == "GET" {
		return r, nil
	}
	switch {
	case accountRe.MatchString(r.Node):
		if account == "" {
			return Route{}, fmt.Errorf("%s on an ad account needs a configured ad account; run `metaads config set ad-account-id <id>`", verb)
		}
		if r.Node != "act_"+account {
			return Route{}, fmt.Errorf("%s goes to the configured ad account act_%s only, not %s", verb, account, r.Node)
		}
	case objectRe.MatchString(r.Node):
	default:
		return Route{}, fmt.Errorf("%s needs act, act_<id> or an object ID as the node, got %q", verb, r.Node)
	}
	return r, nil
}
