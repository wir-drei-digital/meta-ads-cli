package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type hit struct {
	Method, Path, Auth, ContentType string
	Query                           url.Values
	Body                            []byte
}

type recorder struct {
	mu   sync.Mutex
	hits []hit
}

func (r *recorder) all() []hit {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]hit(nil), r.hits...)
}

// server records every request and answers with reply(n), n counting from 1.
func server(t *testing.T, reply func(w http.ResponseWriter, n int)) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.hits = append(rec.hits, hit{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.URL.Query(), b})
		n := len(rec.hits)
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		reply(w, n)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func ok(body string) func(http.ResponseWriter, int) {
	return func(w http.ResponseWriter, _ int) { w.Write([]byte(body)) }
}

func fail(status int, body string, headers map[string]string) func(http.ResponseWriter, int) {
	return func(w http.ResponseWriter, _ int) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}
}

func newClient(base string, sleeps *[]time.Duration) *Client {
	return &Client{BaseURL: base, Token: "tok", AllowCustomBase: true, Sleep: func(d time.Duration) {
		if sleeps != nil {
			*sleeps = append(*sleeps, d)
		}
	}}
}

var ctx = context.Background()

func apiErr(t *testing.T, err error) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want *Error, got %T %v", err, err)
	}
	return e
}

func TestBearerProofAndNoTokenInURL(t *testing.T) {
	srv, rec := server(t, ok(`{"id":"1"}`))
	c := newClient(srv.URL, nil)
	c.Token, c.AppSecret = "token", "secret"
	if _, err := c.Do(ctx, Request{Method: "GET", Path: "v26.0/me", Query: url.Values{"fields": {"id"}}, Class: ClassRead}); err != nil {
		t.Fatal(err)
	}
	h := rec.all()[0]
	if h.Auth != "Bearer token" || h.Path != "/v26.0/me" || h.Query.Get("fields") != "id" || h.Query.Has("access_token") ||
		h.Query.Get("appsecret_proof") != "e941110e3d2bfe82621f0e3e1434730d7305d106c5f68c87165d0b27a4611a4a" {
		t.Fatalf("%+v", h)
	}
}

func TestNoProofWithoutSecretAndOverrides(t *testing.T) {
	srv, rec := server(t, ok(`{}`))
	c := newClient(srv.URL, nil)
	c.Do(ctx, Request{Method: "GET", Path: "v26.0/me", Class: ClassRead})
	c.AppSecret = "secret"
	c.Do(ctx, Request{Method: "GET", Path: "v26.0/debug_token", Class: ClassRead, Bearer: "42|secret", NoProof: true})
	c.Token = ""
	c.Do(ctx, Request{Method: "GET", Path: "v26.0/oauth/access_token", Class: ClassRead, NoAuth: true})
	hits := rec.all()
	if len(hits) != 3 || hits[0].Query.Has("appsecret_proof") || hits[1].Auth != "Bearer 42|secret" ||
		hits[1].Query.Has("appsecret_proof") || hits[2].Auth != "" || hits[2].Query.Has("appsecret_proof") {
		t.Fatalf("%+v", hits)
	}
}

func TestFormAndMultipartBodies(t *testing.T) {
	srv, rec := server(t, ok(`{"id":"1"}`))
	c := newClient(srv.URL, nil)
	form := url.Values{"name": {"x"}, "special_ad_categories": {"[]"}}
	if _, err := c.Do(ctx, Request{Method: "POST", Path: "v26.0/act_1/campaigns", Form: form, Class: "write"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(ctx, Request{Method: "DELETE", Path: "v26.0/act_1/adimages", Form: url.Values{"hash": {"abc"}}, Class: "delete"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(ctx, Request{Method: "POST", Path: "v26.0/act_1/adimages", Multipart: []byte("--b--\r\n"), ContentType: "multipart/form-data; boundary=b", Class: "write"}); err != nil {
		t.Fatal(err)
	}
	hits := rec.all()
	got, _ := url.ParseQuery(string(hits[0].Body))
	if hits[0].ContentType != "application/x-www-form-urlencoded" || got.Get("special_ad_categories") != "[]" || got.Get("name") != "x" {
		t.Fatalf("POST form: %+v %v", hits[0], got)
	}
	got, _ = url.ParseQuery(string(hits[1].Body))
	if hits[1].Method != "DELETE" || got.Get("hash") != "abc" {
		t.Fatalf("DELETE form: %+v", hits[1])
	}
	if hits[2].ContentType != "multipart/form-data; boundary=b" || !bytes.Equal(hits[2].Body, []byte("--b--\r\n")) {
		t.Fatalf("multipart: %+v", hits[2])
	}
}

func TestLocalRefusals(t *testing.T) {
	read := Request{Method: "GET", Path: "v26.0/me", Class: ClassRead}
	for _, c := range []struct {
		name string
		c    Client
		r    Request
		want string
	}{
		{"custom host", Client{BaseURL: "https://example.com", Token: "t"}, read, "refusing to send credentials to example.com"},
		{"plain http", Client{BaseURL: "http://example.com", Token: "t", AllowCustomBase: true}, read, "non-HTTPS"},
		{"no token", Client{BaseURL: "https://graph.facebook.com"}, read, "no access token"},
		{"post without class", Client{BaseURL: "https://graph.facebook.com", Token: "t"}, Request{Method: "POST", Path: "v26.0/act_1/campaigns"}, "no risk class"},
		{"credential in query", Client{BaseURL: "https://graph.facebook.com", Token: "t"}, Request{Method: "GET", Path: "v26.0/me", Query: url.Values{"access_token": {"x"}}, Class: ClassRead}, "carries a credential"},
		{"credential in form", Client{BaseURL: "https://graph.facebook.com", Token: "t"}, Request{Method: "POST", Path: "v26.0/1", Form: url.Values{"appsecret_proof": {"x"}}, Class: "write"}, "carries a credential"},
		{"read-only write", Client{BaseURL: "https://graph.facebook.com", Token: "t", ReadOnly: true}, Request{Method: "POST", Path: "v26.0/1", Class: "write"}, "read-only mode"},
		{"get with body", Client{BaseURL: "https://graph.facebook.com", Token: "t"}, Request{Method: "GET", Path: "v26.0/me", Form: url.Values{"a": {"b"}}, Class: ClassRead}, "body"},
		{"patch", Client{BaseURL: "https://graph.facebook.com", Token: "t"}, Request{Method: "PATCH", Path: "v26.0/1", Class: "write"}, "unsupported method"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.c.Do(ctx, c.r)
			e := apiErr(t, err)
			if e.Kind != KindUsage || !strings.Contains(e.Message, c.want) {
				t.Fatalf("got %s %q, want usage containing %q", e.Kind, e.Message, c.want)
			}
		})
	}
}

func TestReadOnlyAllowsValidateOnly(t *testing.T) {
	srv, rec := server(t, ok(`{"success":true}`))
	c := newClient(srv.URL, nil)
	c.ReadOnly = true
	if _, err := c.Do(ctx, Request{Method: "POST", Path: "v26.0/act_1/campaigns", Class: "spend", ValidateOnly: true}); err != nil {
		t.Fatal(err)
	}
	if len(rec.all()) != 1 {
		t.Fatal("the validate-only request was not sent")
	}
}

func TestErrorMapping(t *testing.T) {
	tier := `{"123":[{"type":"ads_management","call_count":100,"estimated_time_to_regain_access":10,"ads_api_access_tier":"development_access"}]}`
	for _, c := range []struct {
		name    string
		status  int
		body    string
		headers map[string]string
		kind    string
		rid     string
		want    []string
	}{
		{"token", 400, `{"error":{"message":"Invalid OAuth access token.","type":"OAuthException","code":190,"fbtrace_id":"T1"}}`, nil,
			KindAuth, "T1", []string{"HTTP 400: 190: Invalid OAuth access token.", "auth status --check"}},
		{"permission", 400, `{"error":{"message":"(#200) Permissions error","code":200}}`, nil,
			KindForbidden, "", []string{"ads_management"}},
		{"missing object", 400, `{"error":{"message":"Unsupported post request.","code":100,"error_subcode":33}}`, nil,
			KindNotFound, "", []string{"100/33", "does not exist"}},
		{"budget level", 400, `{"error":{"message":"Invalid parameter","code":100,"error_subcode":1885621,"error_user_title":"Budget conflict","error_user_msg":"Set the budget on one level.","error_data":"{\"blame_field_specs\":[[\"daily_budget\"]]}"}}`, nil,
			KindValidation, "", []string{"100/1885621: Budget conflict: Set the budget on one level. (fields: daily_budget)", "keep it on one level"}},
		{"special ad categories", 400, `{"error":{"message":"Invalid parameter","code":100,"error_data":{"blame_field_specs":[["special_ad_categories"]]}}}`, nil,
			KindValidation, "", []string{"send []"}},
		{"transient", 500, `{"error":{"message":"Service temporarily unavailable","code":2,"is_transient":true}}`, nil,
			KindServer, "", []string{"HTTP 500: 2: Service temporarily unavailable"}},
		{"html gateway", 502, `<html>bad gateway</html>`, map[string]string{"x-fb-trace-id": "H1"},
			KindServer, "H1", []string{"HTTP 502"}},
		{"rate limit beyond budget", 400, `{"error":{"message":"User request limit reached","code":17,"error_subcode":2446079}}`, map[string]string{"X-Business-Use-Case-Usage": tier},
			KindRateLimited, "", []string{"retry budget", "Limited access tier"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv, rec := server(t, fail(c.status, c.body, c.headers))
			_, err := newClient(srv.URL, nil).Do(ctx, Request{Method: "POST", Path: "v26.0/act_1/campaigns", Class: "write"})
			e := apiErr(t, err)
			if e.Kind != c.kind || e.Status != c.status || e.RequestID != c.rid || len(rec.all()) != 1 {
				t.Fatalf("kind %s status %d rid %q calls %d: %s", e.Kind, e.Status, e.RequestID, len(rec.all()), e.Message)
			}
			for _, w := range c.want {
				if !strings.Contains(e.Message, w) {
					t.Errorf("message lacks %q: %s", w, e.Message)
				}
			}
			if !strings.HasPrefix(e.Message, "POST v26.0/act_1/campaigns: ") {
				t.Errorf("message must start with the method and path: %s", e.Message)
			}
		})
	}
}

func TestErrorDetailsIsMetaErrorObject(t *testing.T) {
	srv, _ := server(t, fail(400, `{"error":{"message":"bad","code":100}}`, nil))
	_, err := newClient(srv.URL, nil).Do(ctx, Request{Method: "GET", Path: "v26.0/me", Class: ClassRead})
	d, ok := apiErr(t, err).Details.(map[string]any)
	if !ok || d["message"] != "bad" {
		t.Fatalf("details %#v", apiErr(t, err).Details)
	}
}

func TestRateLimitWaitsThenSucceeds(t *testing.T) {
	srv, rec := server(t, func(w http.ResponseWriter, n int) {
		if n == 1 {
			w.Header().Set("X-Ad-Account-Usage", `{"acc_id_util_pct":100,"reset_time_duration":2}`)
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"message":"limit","code":80004}}`))
			return
		}
		w.Write([]byte(`{"id":"1"}`))
	})
	var sleeps []time.Duration
	c := newClient(srv.URL, &sleeps)
	if _, err := c.Do(ctx, Request{Method: "POST", Path: "v26.0/act_1/campaigns", Class: "write"}); err != nil {
		t.Fatal(err)
	}
	if len(rec.all()) != 2 || len(sleeps) != 1 || sleeps[0] < 2*time.Second || sleeps[0] >= 2*time.Second+500*time.Millisecond {
		t.Fatalf("calls %d sleeps %v", len(rec.all()), sleeps)
	}
}

func TestServerErrorsRetriedForReadsOnly(t *testing.T) {
	flaky := func(w http.ResponseWriter, n int) {
		if n < 3 {
			w.WriteHeader(500)
			w.Write([]byte(`{"error":{"message":"oops","code":1}}`))
			return
		}
		w.Write([]byte(`{"id":"1"}`))
	}
	srv, rec := server(t, flaky)
	if _, err := newClient(srv.URL, nil).Do(ctx, Request{Method: "GET", Path: "v26.0/me", Class: ClassRead}); err != nil || len(rec.all()) != 3 {
		t.Fatalf("read: %v after %d calls", err, len(rec.all()))
	}
	srv, rec = server(t, flaky)
	_, err := newClient(srv.URL, nil).Do(ctx, Request{Method: "POST", Path: "v26.0/1", Class: "write"})
	if apiErr(t, err).Kind != KindServer || len(rec.all()) != 1 {
		t.Fatalf("write was retried: %d calls", len(rec.all()))
	}
}

func TestTransportFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	c := newClient(srv.URL, nil)
	c.AppSecret = "the-app-secret"
	_, err := c.Do(ctx, Request{Method: "POST", Path: "v26.0/act_1/campaigns", Form: url.Values{"name": {"x"}}, Class: "write"})
	e := apiErr(t, err)
	if e.Kind != KindOutcomeUnknown || !strings.HasPrefix(e.Message, "POST v26.0/act_1/campaigns: ") {
		t.Fatalf("write: %s %s", e.Kind, e.Message)
	}
	_, err = c.Do(ctx, Request{Method: "GET", Path: "v26.0/debug_token", Query: url.Values{"input_token": {"SECRET-INPUT"}}, Class: ClassRead})
	e = apiErr(t, err)
	if e.Kind != KindTransport {
		t.Fatalf("read: %s %s", e.Kind, e.Message)
	}
	for _, leak := range []string{"appsecret_proof", "SECRET-INPUT", "?"} {
		if strings.Contains(e.Message, leak) {
			t.Fatalf("the message leaks %q: %s", leak, e.Message)
		}
	}
}

func TestRedirectRefused(t *testing.T) {
	srv, _ := server(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Location", "https://evil.example/x")
		w.WriteHeader(302)
	})
	_, err := newClient(srv.URL, nil).Do(ctx, Request{Method: "GET", Path: "v26.0/me", Class: ClassRead})
	if e := apiErr(t, err); e.Kind != KindValidation || !strings.Contains(e.Message, "redirect") {
		t.Fatalf("%s %s", e.Kind, e.Message)
	}
}

func TestVerboseNeverLogsQuery(t *testing.T) {
	srv, _ := server(t, ok(`{}`))
	var log bytes.Buffer
	c := newClient(srv.URL, nil)
	c.AppSecret, c.Verbose = "secret", &log
	c.Do(ctx, Request{Method: "GET", Path: "v26.0/debug_token", Query: url.Values{"input_token": {"SECRET-INPUT"}}, Class: ClassRead})
	if strings.Contains(log.String(), "SECRET-INPUT") || strings.Contains(log.String(), "appsecret_proof") || !strings.Contains(log.String(), "GET v26.0/debug_token") {
		t.Fatalf("verbose log: %q", log.String())
	}
}
