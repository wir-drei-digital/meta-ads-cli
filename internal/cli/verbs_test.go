package cli

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetBuildsQuery(t *testing.T) {
	g := newFakeGraph(t, func(seen) (int, string) { return 200, `{"data":[]}` })
	a, out, errb := testApp(t, g, defaultRes())
	code := a.run([]string{"get", "act/campaigns", "--fields", "name,status", "--param", "limit=5",
		"--param", `time_range={"since":"2026-09-01","until":"2026-09-24"}`})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	c := g.all()[0]
	if c.Method != "GET" || c.Path != "/v26.0/act_1/campaigns" || c.Query.Get("fields") != "name,status" ||
		c.Query.Get("limit") != "5" || c.Query.Get("time_range") != `{"since":"2026-09-01","until":"2026-09-24"}` ||
		c.Auth != "Bearer tok" || c.Query.Has("access_token") {
		t.Fatalf("%+v", c)
	}
	if out.String() != "{\"data\":[]}\n" {
		t.Fatalf("stdout %q", out)
	}
}

func TestGetRefusals(t *testing.T) {
	res := defaultRes()
	res.AdAccountID = ""
	for _, c := range []struct {
		res  bool // true: no ad account configured
		args []string
		want string
	}{
		{true, []string{"get", "act/campaigns"}, "no ad account is configured"},
		{false, []string{"get", "me", "--param", "fields=id"}, "--fields"},
		{false, []string{"get", "me", "--param", "method=post"}, "parameter method is refused"},
		{false, []string{"get", "me", "--param", "limit"}, "key=value"},
		{false, []string{"get", "me", "--param", "a=1", "--param", "a=2"}, "given twice"},
		{false, []string{"get", "act/campaigns?limit=5"}, "query string"},
	} {
		r := defaultRes()
		if c.res {
			r = res
		}
		a, _, errb := testApp(t, nil, r)
		if code := a.run(c.args); code != 2 {
			t.Fatalf("%v: exit %d", c.args, code)
		}
		if e := errJSON(t, errb); e["kind"] != "usage" || !strings.Contains(e["error"].(string), c.want) {
			t.Fatalf("%v: %v", c.args, e)
		}
	}
}

func TestPostEncodesForm(t *testing.T) {
	g := newFakeGraph(t, nil)
	a, out, errb := testApp(t, g, defaultRes())
	code := a.run([]string{"post", "act/campaigns", "--data", `{"name":"Test <1>","objective":"OUTCOME_TRAFFIC","status":"PAUSED",` +
		`"special_ad_categories":[],"daily_budget":3000,"is_adset_budget_sharing_enabled":false}`})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	c := g.all()[0]
	want := map[string]string{"name": "Test <1>", "objective": "OUTCOME_TRAFFIC", "status": "PAUSED",
		"special_ad_categories": "[]", "daily_budget": "3000", "is_adset_budget_sharing_enabled": "false"}
	for k, v := range want {
		if c.Form.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, c.Form.Get(k), v)
		}
	}
	if c.Method != "POST" || c.Path != "/v26.0/act_1/campaigns" || c.Form.Has("execution_options") ||
		!strings.HasPrefix(c.ContentType, "application/x-www-form-urlencoded") || !strings.Contains(out.String(), `"id":"1"`) {
		t.Fatalf("%+v stdout %q", c, out)
	}
}

func TestPostActiveNeedsForce(t *testing.T) {
	g := newFakeGraph(t, nil)
	a, _, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"post", "123", "--data", `{"status":"ACTIVE"}`}); code != 2 || len(g.all()) != 0 {
		t.Fatalf("exit %d, %d calls", code, len(g.all()))
	}
	msg := errJSON(t, errb)["error"].(string)
	if !strings.HasPrefix(msg, "POST v26.0/123: ") || !strings.Contains(msg, "--force") || !strings.Contains(msg, `status "ACTIVE"`) {
		t.Fatal(msg)
	}
	errb.Reset()
	if code := a.run([]string{"post", "123", "--force", "--data", `{"status":"ACTIVE"}`}); code != 0 || len(g.all()) != 1 {
		t.Fatalf("with --force: exit %d: %s", code, errb)
	}
}

func TestPostBudgetAboveCapRefusedEvenWithForce(t *testing.T) {
	g := newFakeGraph(t, nil)
	a, _, errb := testApp(t, g, defaultRes())
	code := a.run([]string{"post", "act/adsets", "--force", "--data", `{"status":"PAUSED","daily_budget":3001}`})
	if code != 2 || len(g.all()) != 0 || !strings.Contains(errJSON(t, errb)["error"].(string), "exceeds") {
		t.Fatalf("exit %d: %s", code, errb)
	}
}

func TestValidateOnly(t *testing.T) {
	g := newFakeGraph(t, func(seen) (int, string) { return 200, `{"success":true}` })
	a, _, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"post", "act/campaigns", "--validate-only", "--data", `{"name":"x","special_ad_categories":[]}`}); code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	if got := g.all()[0].Form.Get("execution_options"); got != `["validate_only"]` {
		t.Fatalf("execution_options %q", got)
	}
	errb.Reset()
	if code := a.run([]string{"post", "123", "--validate-only", "--data", `{"name":"x"}`}); code != 2 || len(g.all()) != 1 {
		t.Fatalf("update: exit %d", code)
	}
}

func TestPostPathRefusals(t *testing.T) {
	g := newFakeGraph(t, nil)
	for _, args := range [][]string{
		{"post", "act/campaigns?status=ACTIVE", "--data", `{"status":"PAUSED"}`},
		{"post", "act_2/campaigns", "--data", `{"status":"PAUSED"}`},
		{"post", "me/adaccounts", "--data", `{}`},
		{"post", "act/campaigns", "--data", `{"status":"PAUSED","status":"ACTIVE"}`},
		{"post", "act/campaigns", "--data", `[1]`},
	} {
		a, _, errb := testApp(t, g, defaultRes())
		if code := a.run(args); code != 2 || errJSON(t, errb)["kind"] != "usage" {
			t.Fatalf("%v: exit %d %s", args, code, errb)
		}
	}
	if len(g.all()) != 0 {
		t.Fatalf("%d requests were sent", len(g.all()))
	}
}

func TestFileUpload(t *testing.T) {
	p := filepath.Join(t.TempDir(), "motiv.png")
	if err := os.WriteFile(p, []byte("PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := newFakeGraph(t, func(seen) (int, string) { return 200, `{"images":{"motiv.png":{"hash":"abc"}}}` })
	a, out, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"post", "act/adimages", "--file", "filename=@" + p}); code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	c := g.all()[0]
	mt, params, err := mime.ParseMediaType(c.ContentType)
	if err != nil || mt != "multipart/form-data" {
		t.Fatalf("content type %q", c.ContentType)
	}
	form, err := multipart.NewReader(bytes.NewReader(c.Body), params["boundary"]).ReadForm(1 << 20)
	if err != nil || len(form.File["filename"]) != 1 {
		t.Fatalf("form %+v %v", form, err)
	}
	f, _ := form.File["filename"][0].Open()
	data, _ := io.ReadAll(f)
	if string(data) != "PNG" || !strings.Contains(out.String(), `"hash":"abc"`) {
		t.Fatalf("file %q stdout %q", data, out)
	}
	for _, bad := range []string{"filename", "filename=motiv.png", "=@" + p, "status=@" + p} {
		a, _, _ := testApp(t, g, defaultRes())
		if code := a.run([]string{"post", "act/adimages", "--file", bad}); code != 2 {
			t.Fatalf("--file %q: exit %d", bad, code)
		}
	}
}

func TestDelete(t *testing.T) {
	g := newFakeGraph(t, func(seen) (int, string) { return 200, `{"success":true}` })
	a, _, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"delete", "act/adimages", "--param", "hash=abc"}); code != 2 || len(g.all()) != 0 {
		t.Fatalf("without --force: exit %d", code)
	}
	errb.Reset()
	if code := a.run([]string{"delete", "act/adimages", "--param", "hash=abc", "--force"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	c := g.all()[0]
	if c.Method != "DELETE" || c.Form.Get("hash") != "abc" {
		t.Fatalf("%+v", c)
	}
	if code := a.run([]string{"delete", "act/campaigns", "--param", "delete_strategy=DELETE_ANY", "--force"}); code != 2 {
		t.Fatalf("bulk delete: exit %d", code)
	}
}

func TestAllMergesPages(t *testing.T) {
	g := newFakeGraph(t, func(s seen) (int, string) {
		if s.Query.Get("after") == "" {
			return 200, `{"data":[{"id":"1"}],"paging":{"cursors":{"before":"b0","after":"c1"},"next":"https://graph.facebook.com/v26.0/act_1/campaigns?after=c1"}}`
		}
		return 200, `{"data":[{"id":"2"}],"paging":{"cursors":{"before":"c1","after":"c2"}}}`
	})
	a, out, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"get", "act/campaigns", "--all", "--param", "after=zzz", "--param", "limit=1"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	if out.String() != "[{\"id\":\"1\"},{\"id\":\"2\"}]\n" {
		t.Fatalf("stdout %q", out)
	}
	calls := g.all()
	if len(calls) != 2 || calls[0].Query.Has("after") || calls[1].Query.Get("after") != "c1" || calls[1].Query.Get("limit") != "1" {
		t.Fatalf("%+v", calls)
	}
}

func TestAllMaxPagesIsIncomplete(t *testing.T) {
	g := newFakeGraph(t, func(seen) (int, string) {
		return 200, `{"data":[{"id":"x"}],"paging":{"cursors":{"after":"next"},"next":"https://graph.facebook.com/next"}}`
	})
	a, out, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"get", "act/campaigns", "--all", "--max-pages", "2"}); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if errJSON(t, errb)["kind"] != "incomplete" || out.String() != "[{\"id\":\"x\"},{\"id\":\"x\"}]\n" {
		t.Fatalf("stdout %q stderr %s", out, errb)
	}
}

func TestAllNeedsDataArray(t *testing.T) {
	g := newFakeGraph(t, func(seen) (int, string) { return 200, `{"id":"act_1","name":"x"}` })
	a, _, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"get", "act", "--all"}); code != 2 || !strings.Contains(errJSON(t, errb)["error"].(string), "data array") {
		t.Fatalf("exit %d %s", code, errb)
	}
}

func TestMetaErrorIsOneJSONLine(t *testing.T) {
	g := newFakeGraph(t, func(seen) (int, string) {
		return 400, `{"error":{"message":"Invalid parameter","type":"OAuthException","code":100,"fbtrace_id":"F1"}}`
	})
	a, out, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"get", "act/campaigns"}); code != 1 || out.Len() != 0 {
		t.Fatalf("exit %d stdout %q", code, out)
	}
	if e := errJSON(t, errb); e["kind"] != "validation" || e["request_id"] != "F1" || e["status"] != float64(400) {
		t.Fatalf("%v", e)
	}
}

func TestReadOnlyMode(t *testing.T) {
	g := newFakeGraph(t, nil)
	res := defaultRes()
	res.ReadOnly = true
	a, _, errb := testApp(t, g, res)
	if code := a.run([]string{"post", "act/campaigns", "--data", `{"status":"PAUSED"}`}); code != 2 || !strings.Contains(errJSON(t, errb)["error"].(string), "read-only") {
		t.Fatalf("exit %d %s", code, errb)
	}
	errb.Reset()
	if code := a.run([]string{"post", "act/campaigns", "--validate-only", "--data", `{"status":"PAUSED"}`}); code != 0 {
		t.Fatalf("validate-only: exit %d %s", code, errb)
	}
	if code := a.run([]string{"get", "me"}); code != 0 {
		t.Fatalf("get: exit %d %s", code, errb)
	}
}

func TestDataFileBOMAndUTF16(t *testing.T) {
	g := newFakeGraph(t, nil)
	dir := t.TempDir()
	bom := filepath.Join(dir, "bom.json")
	os.WriteFile(bom, append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"name":"x","status":"PAUSED"}`)...), 0o600)
	utf16 := filepath.Join(dir, "utf16.json")
	os.WriteFile(utf16, []byte{0xFF, 0xFE, '{', 0, '}', 0}, 0o600)
	a, _, errb := testApp(t, g, defaultRes())
	if code := a.run([]string{"post", "123", "--data", "@" + bom}); code != 0 || g.all()[0].Form.Get("name") != "x" {
		t.Fatalf("BOM: exit %d %s", code, errb)
	}
	errb.Reset()
	if code := a.run([]string{"post", "123", "--data", "@" + utf16}); code != 2 || !strings.Contains(errJSON(t, errb)["error"].(string), "UTF-16") {
		t.Fatalf("UTF-16: exit %d %s", code, errb)
	}
}

func TestDataFromStdinAndOutputFile(t *testing.T) {
	g := newFakeGraph(t, nil)
	a, out, errb := testApp(t, g, defaultRes())
	a.stdin = strings.NewReader(`{"name":"from stdin"}`)
	p := filepath.Join(t.TempDir(), "resp.json")
	if code := a.run([]string{"post", "123", "--data", "-", "--output", p}); code != 0 {
		t.Fatalf("exit %d %s", code, errb)
	}
	got, _ := os.ReadFile(p)
	if g.all()[0].Form.Get("name") != "from stdin" || string(got) != `{"id":"1"}` || out.Len() != 0 {
		t.Fatalf("form %v file %q stdout %q", g.all()[0].Form, got, out)
	}
}

// A capitalised edge is refused for post and delete before anything is
// sent, --force or not: the guard matches edges exactly.
func TestWriteEdgeSpellingRefused(t *testing.T) {
	g := newFakeGraph(t, nil)
	for _, args := range [][]string{
		{"post", "123/Budget_Schedules", "--force", "--data", `{}`},
		{"post", "act/AdRules_Library", "--force"},
		{"post", "act/Async_Batch_Requests", "--force"},
		{"post", "act/ReachFrequencyPredictions", "--force"},
		{"delete", "act/Campaigns", "--force", "--param", "delete_strategy=DELETE_ANY"},
	} {
		a, _, errb := testApp(t, g, defaultRes())
		if code := a.run(args); code != 2 || !strings.Contains(errJSON(t, errb)["error"].(string), "^[a-z0-9_]+$") {
			t.Fatalf("%v: exit %d %s", args, code, errb)
		}
	}
	if len(g.all()) != 0 {
		t.Fatalf("%d requests were sent", len(g.all()))
	}
}
