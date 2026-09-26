package route

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, c := range []struct {
		path, verb, account string
		want                string // Path(), or "" when an error is expected
		err                 string
	}{
		{"act/campaigns", "GET", "1", "v26.0/act_1/campaigns", ""},
		{"/act_1/campaigns", "GET", "1", "v26.0/act_1/campaigns", ""},
		{"v24.0/act/insights", "GET", "1", "v24.0/act_1/insights", ""},
		{"me", "GET", "", "v26.0/me", ""},
		{"me/adaccounts", "GET", "", "v26.0/me/adaccounts", ""},
		{"act_5/campaigns", "GET", "1", "v26.0/act_5/campaigns", ""},
		{"act/campaigns", "GET", "", "", "no ad account is configured"},
		{"act/campaigns?fields=name", "GET", "1", "", "--fields"},
		{"act_1/camp%61igns", "GET", "1", "", "escape"},
		{"act_1#x", "GET", "1", "", "fragment"},
		{"act_1/ campaigns", "GET", "1", "", "whitespace"},
		{"act_1/..", "GET", "1", "", "invalid segment"},
		{"act_1/", "GET", "1", "", "invalid segment"},
		{"act_1//campaigns", "GET", "1", "", "<node>/<edge>"},
		{"a/b/c", "GET", "1", "", "<node>/<edge>"},
		{"v26.0", "GET", "1", "", "<node>/<edge>"},
		{"", "GET", "1", "", "a path is required"},
		{"act/campaigns", "POST", "1", "v26.0/act_1/campaigns", ""},
		{"act_1/adimages", "POST", "1", "v26.0/act_1/adimages", ""},
		{"act_2/campaigns", "POST", "1", "", "configured ad account act_1 only"},
		{"act_1/campaigns", "POST", "", "", "needs a configured ad account"},
		{"120330000000", "POST", "", "v26.0/120330000000", ""},
		{"120330000000/copies", "POST", "1", "v26.0/120330000000/copies", ""},
		{"me/adaccounts", "POST", "1", "", "needs act, act_<id> or an object ID"},
		{"120330000000", "DELETE", "1", "v26.0/120330000000", ""},
		{"act/adimages", "DELETE", "1", "v26.0/act_1/adimages", ""},
	} {
		r, err := Parse(c.path, c.verb, c.account, "v26.0")
		if c.err == "" {
			if err != nil || r.Path() != c.want {
				t.Errorf("%s %q: got %q %v, want %q", c.verb, c.path, r.Path(), err, c.want)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s %q: want an error containing %q, got %q %v", c.verb, c.path, c.err, r.Path(), err)
		}
	}
}

func TestRouteParts(t *testing.T) {
	r, err := Parse("act/campaigns", "POST", "7", "v26.0")
	if err != nil || r.Version != "v26.0" || r.Node != "act_7" || r.Edge != "campaigns" || !r.IsAccount() {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = Parse("123", "POST", "7", "v26.0")
	if err != nil || r.Node != "123" || r.Edge != "" || r.IsAccount() {
		t.Fatalf("%+v %v", r, err)
	}
}
