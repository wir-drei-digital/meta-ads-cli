package api

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMetaKinds(t *testing.T) {
	for _, c := range []struct {
		e      metaError
		status int
		want   string
	}{
		{metaError{Code: 190}, 400, KindAuth},
		{metaError{Code: 10}, 400, KindForbidden},
		{metaError{Code: 200}, 400, KindForbidden},
		{metaError{Code: 294}, 400, KindForbidden},
		{metaError{Code: 368}, 400, KindForbidden},
		{metaError{Code: 100, Subcode: 33}, 400, KindNotFound},
		{metaError{Code: 100}, 400, KindValidation},
		{metaError{Code: 2500}, 400, KindValidation},
		{metaError{Code: 4}, 400, KindRateLimited},
		{metaError{Code: 17, Subcode: 2446079}, 400, KindRateLimited},
		{metaError{Code: 613}, 400, KindRateLimited},
		{metaError{Code: 80004}, 400, KindRateLimited},
		{metaError{Code: 2}, 400, KindServer},
		{metaError{Code: 3, IsTransient: true}, 400, KindServer},
		{metaError{}, 500, KindServer},
		{metaError{}, 404, KindNotFound},
		{metaError{}, 401, KindAuth},
		{metaError{Code: 3}, 400, KindValidation},
	} {
		if got := c.e.kind(c.status); got != c.want {
			t.Errorf("code %d/%d status %d: got %s, want %s", c.e.Code, c.e.Subcode, c.status, got, c.want)
		}
	}
}

func TestBlameFields(t *testing.T) {
	obj := metaError{ErrorData: []byte(`{"blame_field_specs":[["daily_budget"],["targeting","geo_locations"]]}`)}
	str := metaError{ErrorData: []byte(`"{\"blame_field_specs\":[[\"daily_budget\"]]}"`)}
	if got := strings.Join(obj.blameFields(), ","); got != "daily_budget,targeting.geo_locations" {
		t.Fatalf("object form: %s", got)
	}
	if got := strings.Join(str.blameFields(), ","); got != "daily_budget" {
		t.Fatalf("string form: %s", got)
	}
	if got := (metaError{}).blameFields(); got != nil {
		t.Fatalf("no error_data: %v", got)
	}
}

func TestUsageWait(t *testing.T) {
	h := http.Header{}
	if usageWait(h) != 0 {
		t.Fatal("no headers must mean no wait")
	}
	h.Set("X-Ad-Account-Usage", `{"acc_id_util_pct":100,"reset_time_duration":30,"ads_api_access_tier":"standard_access"}`)
	if got := usageWait(h); got != 30*time.Second {
		t.Fatalf("account usage: %s", got)
	}
	h.Set("X-Business-Use-Case-Usage", `{"123":[{"type":"ads_management","call_count":100,"estimated_time_to_regain_access":2},{"type":"ads_insights","estimated_time_to_regain_access":5}]}`)
	if got := usageWait(h); got != 5*time.Minute {
		t.Fatalf("business use case usage wins and takes the longest wait: %s", got)
	}
}
