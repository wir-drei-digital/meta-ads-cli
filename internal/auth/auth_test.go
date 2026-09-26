package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
)

func TestDebugToken(t *testing.T) {
	var gotAuth, gotInput, gotProof, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		gotInput, gotProof = r.URL.Query().Get("input_token"), r.URL.Query().Get("appsecret_proof")
		w.Write([]byte(`{"data":{"app_id":"42","type":"SYSTEM_USER","is_valid":true,"expires_at":0,` +
			`"scopes":["ads_management","ads_read"],"granular_scopes":[{"scope":"ads_management","target_ids":["8","7"]},` +
			`{"scope":"ads_read","target_ids":["7"]},{"scope":"pages_manage_ads","target_ids":["99"]}],"user_id":"5"}}`))
	}))
	defer srv.Close()
	c := &api.Client{BaseURL: srv.URL, AllowCustomBase: true, AppSecret: "sec", Sleep: func(time.Duration) {}}
	info, err := DebugToken(context.Background(), c, "v26.0", "42", "sec", "user-tok")
	if err != nil || !info.IsValid || info.ExpiresAt != 0 || strings.Join(info.AdAccounts(), ",") != "act_7,act_8" {
		t.Fatalf("%+v %v", info, err)
	}
	if gotPath != "/v26.0/debug_token" || gotAuth != "Bearer 42|sec" || gotInput != "user-tok" || gotProof != "" {
		t.Fatalf("path %q auth %q input %q proof %q", gotPath, gotAuth, gotInput, gotProof)
	}
}

func TestExchange(t *testing.T) {
	var q map[string]string
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		q = map[string]string{}
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		if r.URL.Path != "/v26.0/oauth/access_token" {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte(`{"access_token":"new-tok","token_type":"bearer","expires_in":5184000}`))
	}))
	defer srv.Close()
	c := &api.Client{BaseURL: srv.URL, AllowCustomBase: true, Sleep: func(time.Duration) {}}
	ex, err := Exchange(context.Background(), c, "v26.0", "42", "sec", "old-tok")
	if err != nil || ex.AccessToken != "new-tok" || ex.ExpiresIn != 5184000 {
		t.Fatalf("%+v %v", ex, err)
	}
	if gotAuth != "" || q["grant_type"] != "fb_exchange_token" || q["client_id"] != "42" || q["client_secret"] != "sec" ||
		q["fb_exchange_token"] != "old-tok" || q["set_token_expires_in_60_days"] != "true" {
		t.Fatalf("auth %q query %v", gotAuth, q)
	}
}

func TestExchangeWithoutTokenInResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer srv.Close()
	c := &api.Client{BaseURL: srv.URL, AllowCustomBase: true, Sleep: func(time.Duration) {}}
	if _, err := Exchange(context.Background(), c, "v26.0", "42", "sec", "old"); err == nil {
		t.Fatal("an empty response was accepted")
	}
}
