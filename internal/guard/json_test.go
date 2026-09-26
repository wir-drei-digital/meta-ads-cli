package guard

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseBody(t *testing.T) {
	obj, err := ParseBody([]byte(`{"name":"x","daily_budget":3000,"adset_spec":{"status":"PAUSED"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := obj["daily_budget"].(json.Number); !ok || n.String() != "3000" {
		t.Fatalf("numbers must stay json.Number: %#v", obj["daily_budget"])
	}
	for body, want := range map[string]string{
		`{"status":"PAUSED","status":"ACTIVE"}`:           "duplicate key",
		`{"adset_spec":{"status":"PAUSED","status":"X"}}`: "adset_spec",
		`{"a":1} {"b":2}`: "trailing data",
		`[1,2]`:           "JSON object",
		`"x"`:             "JSON object",
		`{"a":`:           "EOF",
	} {
		if _, err := ParseBody([]byte(body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want an error containing %q, got %v", body, want, err)
		}
	}
}
