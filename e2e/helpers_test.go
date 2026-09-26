// Package e2e drives the real binary as a subprocess: argv parsing, exit
// codes and stdout and stderr separation, exactly as a user gets them.
// Everything else in the repo tests in-process.
package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "metaads")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", bin, "github.com/wir-drei-digital/meta-ads-cli/cmd/metaads").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// run executes the binary with exactly env and returns stdout, stderr and the exit code.
func run(t *testing.T, bin string, env []string, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String(), stderr.String(), code
}

// scrubbedEnv drops every META_ADS_* variable and points every per-user
// directory into a temp dir, so a developer's real token or config can never
// turn these tests green or red.
func scrubbedEnv(t *testing.T, extra ...string) []string {
	t.Helper()
	home := t.TempDir()
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "META_ADS_") {
			env = append(env, kv)
		}
	}
	env = append(env, "HOME="+home, "USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, "config"), "XDG_CACHE_HOME="+filepath.Join(home, "cache"),
		"AppData="+filepath.Join(home, "appdata"), "LocalAppData="+filepath.Join(home, "localappdata"))
	return append(env, extra...)
}

// fakeGraph plays graph.facebook.com for the smoke test.
type fakeGraph struct {
	*httptest.Server
	writes   atomic.Int32
	mu       sync.Mutex
	lastForm url.Values
}

func newFakeGraph(t *testing.T) *fakeGraph {
	t.Helper()
	g := &fakeGraph{}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		b, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer smoke-token" {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"message":"Invalid OAuth access token.","type":"OAuthException","code":190}}`))
			return
		}
		switch {
		case r.URL.Path == "/v26.0/me":
			w.Write([]byte(`{"id":"9","name":"metaads smoke user"}`))
		case r.URL.Path == "/v26.0/act_1/campaigns" && r.Method == "GET":
			if r.URL.Query().Get("after") == "" {
				w.Write([]byte(`{"data":[{"id":"101"}],"paging":{"cursors":{"after":"c1"},"next":"https://graph.facebook.com/v26.0/act_1/campaigns?after=c1"}}`))
				return
			}
			w.Write([]byte(`{"data":[{"id":"102"}],"paging":{"cursors":{"after":"c2"}}}`))
		case r.URL.Path == "/v26.0/act_1/campaigns" && r.Method == "POST":
			g.writes.Add(1)
			form, _ := url.ParseQuery(string(b))
			g.mu.Lock()
			g.lastForm = form
			g.mu.Unlock()
			if form.Get("name") == "FAIL_ME" {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":{"message":"Invalid parameter","type":"OAuthException","code":100,"error_subcode":1885621,` +
					`"error_user_title":"Budget conflict","error_user_msg":"Set the budget on one level.","fbtrace_id":"trace-smoke"}}`))
				return
			}
			w.Write([]byte(`{"id":"120"}`))
		default:
			w.WriteHeader(400)
			fmt.Fprintf(w, `{"error":{"message":"Unsupported %s request: %s","code":100,"error_subcode":33}}`, r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *fakeGraph) form() url.Values {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lastForm
}

// errLine parses the single JSON line a failure leaves on stderr.
func errLine(t *testing.T, stderr string) map[string]any {
	t.Helper()
	if strings.Count(strings.TrimSuffix(stderr, "\n"), "\n") != 0 {
		t.Fatalf("stderr is not one line: %q", stderr)
	}
	var e map[string]any
	if err := json.Unmarshal([]byte(stderr), &e); err != nil {
		t.Fatalf("stderr is not JSON: %q", stderr)
	}
	return e
}
