package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
)

// seen is one request as the fake Graph server received it.
type seen struct {
	Method, Path, ContentType, Auth string
	Query, Form                     url.Values
	Body                            []byte
}

// fakeGraph records requests and answers with reply, or 200 {"id":"1"}.
type fakeGraph struct {
	*httptest.Server
	mu    sync.Mutex
	calls []seen
}

func newFakeGraph(t *testing.T, reply func(seen) (int, string)) *fakeGraph {
	t.Helper()
	g := &fakeGraph{}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s := seen{Method: r.Method, Path: r.URL.Path, ContentType: r.Header.Get("Content-Type"),
			Auth: r.Header.Get("Authorization"), Query: r.URL.Query(), Body: b}
		if strings.HasPrefix(s.ContentType, "application/x-www-form-urlencoded") {
			s.Form, _ = url.ParseQuery(string(b))
		}
		g.mu.Lock()
		g.calls = append(g.calls, s)
		g.mu.Unlock()
		status, body := 200, `{"id":"1"}`
		if reply != nil {
			status, body = reply(s)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *fakeGraph) all() []seen {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]seen(nil), g.calls...)
}

func defaultRes() config.Resolved {
	return config.Resolved{AccessToken: "tok", TokenFrom: "env", AdAccountID: "1", Currency: "CHF",
		DailyCap: &config.Cap{Minor: 3000, Currency: "CHF"}, LifetimeCap: &config.Cap{Minor: 30000, Currency: "CHF"}}
}

// testApp wires an app to g (or, with g nil, to the real host for tests that
// must refuse before any network I/O).
func testApp(t *testing.T, g *fakeGraph, res config.Resolved) (*app, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	base := "https://graph.facebook.com"
	if g != nil {
		base = g.URL
	}
	a := &app{res: res, stdout: &out, stderr: &errb, stdin: strings.NewReader("")}
	a.client = &api.Client{BaseURL: base, AllowCustomBase: true, Token: res.AccessToken, AppSecret: res.AppSecret,
		ReadOnly: res.ReadOnly, Sleep: func(time.Duration) {}}
	return a, &out, &errb
}

func errJSON(t *testing.T, errb *bytes.Buffer) map[string]any {
	t.Helper()
	if n := strings.Count(strings.TrimSuffix(errb.String(), "\n"), "\n"); n != 0 {
		t.Fatalf("stderr spans %d extra lines: %q", n, errb.String())
	}
	var e map[string]any
	if err := json.Unmarshal(errb.Bytes(), &e); err != nil {
		t.Fatalf("stderr is not JSON: %q", errb.String())
	}
	return e
}

// isolate points every per-user directory at a temp dir, so the tests never
// read or write the developer's real config file.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for k, v := range map[string]string{
		"HOME": home, "USERPROFILE": home,
		"XDG_CONFIG_HOME": filepath.Join(home, "config"), "XDG_CACHE_HOME": filepath.Join(home, "cache"),
		"AppData": filepath.Join(home, "appdata"), "LocalAppData": filepath.Join(home, "localappdata"),
	} {
		t.Setenv(k, v)
	}
	return home
}

func TestRootWithoutSubcommandIsUsage(t *testing.T) {
	a, _, errb := testApp(t, nil, defaultRes())
	if code := a.run(nil); code != 2 || errJSON(t, errb)["kind"] != "usage" {
		t.Fatalf("exit %d %s", code, errb)
	}
}

func TestConfigErrorOnlyBlocksAPICommands(t *testing.T) {
	a, _, errb := testApp(t, nil, defaultRes())
	a.configErr = errors.New("config.json is corrupt")
	if code := a.run([]string{"get", "me"}); code != 2 || !strings.Contains(errJSON(t, errb)["error"].(string), "cannot load the configuration") {
		t.Fatalf("exit %d %s", code, errb)
	}
	for _, args := range [][]string{{"version"}, {"commands"}} {
		if code := a.run(args); code != 0 {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
}

func TestVersion(t *testing.T) {
	a, out, _ := testApp(t, nil, defaultRes())
	if code := a.run([]string{"version"}); code != 0 || !strings.HasPrefix(out.String(), "metaads ") || !strings.Contains(out.String(), "v26.0") {
		t.Fatalf("exit %d %q", code, out)
	}
}

func TestCommandsCatalog(t *testing.T) {
	a, out, errb := testApp(t, nil, defaultRes())
	if code := a.run([]string{"commands", "--json"}); code != 0 {
		t.Fatalf("exit %d %s", code, errb)
	}
	var cat struct {
		SchemaVersion int    `json:"schema_version"`
		APIVersion    string `json:"api_version"`
		Commands      []struct {
			Command string   `json:"command"`
			Flags   []string `json:"flags"`
		} `json:"commands"`
		Guard struct {
			PostEdges map[string]string `json:"post_edges"`
			Refused   map[string]string `json:"refused"`
		} `json:"guard"`
	}
	if err := json.Unmarshal(out.Bytes(), &cat); err != nil {
		t.Fatal(err)
	}
	flags := map[string]string{}
	for _, c := range cat.Commands {
		flags[c.Command] = strings.Join(c.Flags, " ")
	}
	if cat.SchemaVersion != 1 || cat.APIVersion != "v26.0" || !strings.Contains(flags["get"], "--all") ||
		!strings.Contains(flags["post"], "--validate-only") || !strings.Contains(flags["delete"], "--force") ||
		strings.Contains(flags["get"], "--help") || cat.Guard.PostEdges["campaigns"] != "write" ||
		cat.Guard.Refused["field delete_strategy"] == "" {
		t.Fatalf("%+v", cat)
	}
}
