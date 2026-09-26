// Package api is the HTTP client for the Graph API. It owns the host guard,
// the credentials on the wire, the request encoding, the retry policy and
// the error shape.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client performs Graph API requests. BaseURL is required; Timeout,
// RetryBudget and MaxAttempts fall back to defaults.
type Client struct {
	BaseURL                   string
	Token                     string // system user access token; "" means none is configured
	AppSecret                 string // when set, every request carries appsecret_proof
	ReadOnly, AllowCustomBase bool
	Timeout                   time.Duration // per attempt; default 60s
	RetryBudget               time.Duration // total wait on rate limits; default 60s
	MaxAttempts               int           // attempts for read-safe transient failures; default 3
	Verbose                   io.Writer     // nil = silent; never receives a credential or a query string
	HTTP                      *http.Client
	Sleep                     func(time.Duration)
}

// ClassRead is the guard's read class: the only class retried after a
// transient failure, and, with validate-only requests, the only one
// read-only mode lets through.
const ClassRead = "read"

// Request is one API call.
type Request struct {
	Method string     // GET, POST or DELETE
	Path   string     // relative to BaseURL: "v26.0/act_123/campaigns"
	Query  url.Values // query parameters
	Form   url.Values // POST and DELETE: sent as application/x-www-form-urlencoded
	// Multipart, when set, is a complete multipart/form-data body with its
	// ContentType; it replaces Form.
	Multipart   []byte
	ContentType string
	// Class is the guard's risk class; every POST and DELETE must carry one.
	// ValidateOnly says --validate-only was given, so Meta applies nothing.
	Class        string
	ValidateOnly bool
	// Bearer replaces the client's token for this request (the app token for
	// debug_token). NoAuth sends no credential at all (the token exchange,
	// whose credentials are its parameters). NoProof leaves out
	// appsecret_proof.
	Bearer  string
	NoAuth  bool
	NoProof bool
}

// Response is a successful (2xx) response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// readSafe reports whether the call cannot change anything: a read, or a
// validate-only call. Read-only mode allows exactly these, and only these
// are retried after a transient failure.
func (r Request) readSafe() bool { return r.Class == ClassRead || r.ValidateOnly }

// clientParams are the credential parameters only the client sets.
var clientParams = []string{"access_token", "appsecret_proof", "appsecret_time"}

func isLoopback(host string) bool { return host == "localhost" || host == "127.0.0.1" || host == "::1" }

func (c *Client) logf(format string, a ...any) {
	if c.Verbose != nil {
		fmt.Fprintf(c.Verbose, format+"\n", a...)
	}
}

// shape rejects a request that could change something the guard never saw.
func (r Request) shape() *Error {
	switch r.Method {
	case http.MethodGet:
		if r.Form != nil || r.Multipart != nil {
			return Usagef("refusing to send a body with GET %s", r.Path)
		}
	case http.MethodPost, http.MethodDelete:
		if r.Class == "" {
			return Usagef("refusing to send %s %s: no risk class was assigned, so the guard has not checked it", r.Method, r.Path)
		}
		if r.Method == http.MethodDelete && r.Multipart != nil {
			return Usagef("refusing to send a multipart body with DELETE %s", r.Path)
		}
	default:
		return Usagef("unsupported method %q", r.Method)
	}
	return nil
}

// guard rejects a request locally, before anything is sent.
func (c *Client) guard(r Request) *Error {
	if err := r.shape(); err != nil {
		return err
	}
	if !r.NoAuth && r.Bearer == "" && c.Token == "" {
		return Usagef("no access token: set META_ADS_ACCESS_TOKEN, store one with `metaads config set access-token` (reads stdin), or run `metaads init`")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" {
		return Usagef("invalid API base URL %q", c.BaseURL)
	}
	host := u.Hostname()
	if host != "graph.facebook.com" && !c.AllowCustomBase {
		return Usagef("refusing to send credentials to %s; set META_ADS_ALLOW_CUSTOM_BASE=1 if intentional", host)
	}
	if u.Scheme != "https" && !isLoopback(host) {
		return Usagef("refusing non-HTTPS API base %s", c.BaseURL)
	}
	if c.ReadOnly && !r.readSafe() {
		return Usagef("read-only mode (META_ADS_READ_ONLY or config read-only) blocks %s %s; reads and --validate-only creates are still allowed", r.Method, r.Path)
	}
	for _, k := range clientParams {
		if r.Query.Has(k) || r.Form.Has(k) {
			return Usagef("parameter %q carries a credential; the client owns authentication", k)
		}
	}
	return nil
}

// refuseRedirects never follows a redirect: Go keeps Authorization across a
// same-host https to http hop, and a 307/308 would replay a write.
func refuseRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// heldLocation carries a 3xx response's Location past net/http.
const heldLocation = "X-Metaads-Held-Location"

// holdRedirects hides the Location of a 3xx response from net/http. The
// client parses Location before it asks CheckRedirect, and one it cannot
// parse fails the call with an error that quotes it, query and all. Without
// a Location the client hands the 3xx back untouched, and Do refuses it.
type holdRedirects struct{ next http.RoundTripper }

func (t holdRedirects) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil || resp.StatusCode < 300 || resp.StatusCode >= 400 || resp.Header == nil {
		return resp, err
	}
	resp.Header.Del(heldLocation)
	if loc := resp.Header.Values("Location"); len(loc) > 0 {
		resp.Header[heldLocation] = loc
		resp.Header.Del("Location")
	}
	return resp, nil
}

// redirectTarget names where a redirect points by scheme, host and path
// only: a hop that echoes the request URI would otherwise print
// appsecret_proof, input_token, client_secret or fb_exchange_token.
func redirectTarget(loc string) string {
	if loc == "" {
		return "an empty Location"
	}
	u, err := url.Parse(loc)
	if err != nil {
		return "an unparseable Location"
	}
	return strconv.Quote((&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String())
}

// stripURL drops the URL from a transport error. Its query can carry
// appsecret_proof, debug_token's input token or the exchange's client
// secret, and the message is printed.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// attempt performs one round trip; Do wraps it with the retry loop.
func (c *Client) attempt(ctx context.Context, r Request) (*Response, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	q := url.Values{}
	for k, v := range r.Query {
		q[k] = append([]string(nil), v...)
	}
	token := c.Token
	if r.Bearer != "" {
		token = r.Bearer
	}
	if c.AppSecret != "" && !r.NoProof && !r.NoAuth {
		q.Set("appsecret_proof", Proof(token, c.AppSecret))
	}
	u := strings.TrimRight(c.BaseURL, "/") + "/" + strings.TrimLeft(r.Path, "/")
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var body io.Reader
	contentType := ""
	switch {
	case r.Multipart != nil:
		body, contentType = bytes.NewReader(r.Multipart), r.ContentType
	case r.Method != http.MethodGet:
		body, contentType = strings.NewReader(r.Form.Encode()), "application/x-www-form-urlencoded"
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, u, body)
	if err != nil {
		// The error text would quote the URL; name the path only.
		return nil, &Error{Kind: KindUsage, Message: fmt.Sprintf("%s %s: the request could not be built", r.Method, r.Path)}
	}
	if !r.NoAuth {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c.logf("> %s %s", r.Method, r.Path)
	httpClient := &http.Client{}
	if c.HTTP != nil {
		cp := *c.HTTP
		httpClient = &cp
	}
	next := httpClient.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	httpClient.Transport = holdRedirects{next}
	httpClient.CheckRedirect = refuseRedirects
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, stripURL(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, stripURL(err)
	}
	c.logf("< %d (%d bytes)", resp.StatusCode, len(raw))
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: raw}, nil
}

// errorDetails is Meta's error object when the body has one, else the body.
func errorDetails(resp *Response) any {
	if len(resp.Body) == 0 {
		return nil
	}
	var env map[string]any
	if json.Unmarshal(resp.Body, &env) == nil {
		if e, ok := env["error"]; ok {
			return e
		}
		return env
	}
	var v any
	if json.Unmarshal(resp.Body, &v) == nil {
		return v
	}
	return string(resp.Body)
}

// errorFor builds the error for a non-2xx response.
func (c *Client) errorFor(r Request, resp *Response, me metaError, kind, extra string) *Error {
	rid := me.TraceID
	if rid == "" {
		rid = resp.Header.Get("x-fb-trace-id")
	}
	return &Error{
		Kind:      kind,
		Message:   fmt.Sprintf("%s %s: HTTP %d%s%s%s", r.Method, r.Path, resp.Status, me.summary(), extra, hint(me, kind, resp.Header)),
		Status:    resp.Status,
		Details:   errorDetails(resp),
		RequestID: rid,
	}
}

// Do sends a request with the retry policy: rate limits are retried for
// every call within the retry budget, waiting as long as Meta's usage
// headers ask; network failures and server errors are retried for read-safe
// calls only. A write is never replayed. The error is always *Error.
func (c *Client) Do(ctx context.Context, r Request) (*Response, error) {
	if err := c.guard(r); err != nil {
		return nil, err
	}
	sleep := c.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	budget := c.RetryBudget
	if budget == 0 {
		budget = 60 * time.Second
	}
	maxAttempts := c.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 3
	}
	var waited time.Duration
	transientTries := 0
	backoff := time.Second
	// pause waits before the next attempt. A context cancelled meanwhile
	// ends the call: another attempt would only fail, and for a write it
	// would turn a clean refusal into an unknown outcome.
	pause := func(d time.Duration) *Error {
		sleep(d)
		if err := ctx.Err(); err != nil {
			return &Error{Kind: KindTransport, Message: fmt.Sprintf("%s %s: cancelled while waiting to retry: %v", r.Method, r.Path, err)}
		}
		return nil
	}
	for {
		resp, err := c.attempt(ctx, r)
		if err != nil {
			var apiErr *Error
			if errors.As(err, &apiErr) {
				return nil, apiErr
			}
			e := classifyTransport(err, r.readSafe())
			e.Message = r.Method + " " + r.Path + ": " + e.Message
			if e.Kind == KindTransport && r.readSafe() {
				transientTries++
				if transientTries < maxAttempts {
					if pe := pause(backoff); pe != nil {
						return nil, pe
					}
					backoff *= 2
					continue
				}
			}
			return nil, e
		}
		if resp.Status >= 200 && resp.Status < 300 {
			return resp, nil
		}
		if resp.Status >= 300 && resp.Status < 400 {
			// No details: the body of a redirect can echo the request URI.
			return nil, &Error{
				Kind: KindValidation, Status: resp.Status, RequestID: resp.Header.Get("x-fb-trace-id"),
				Message: fmt.Sprintf("%s %s: HTTP %d redirect to %s refused: metaads never follows redirects "+
					"(they can strip HTTPS off the token and replay a write); check META_ADS_API_BASE",
					r.Method, r.Path, resp.Status, redirectTarget(resp.Header.Get(heldLocation))),
			}
		}
		me, _ := parseMetaError(resp.Body)
		kind := me.kind(resp.Status)
		switch {
		case kind == KindRateLimited:
			wait := backoff
			if d := usageWait(resp.Header); d > 0 {
				wait = max(d, backoff)
			} else if ra := retryAfter(resp.Header); ra > 0 {
				wait = max(ra, backoff)
			}
			wait += jitter()
			if waited+wait > budget {
				return nil, c.errorFor(r, resp, me, kind, fmt.Sprintf("; waiting %s would exceed the %s retry budget",
					wait.Round(time.Second), budget))
			}
			waited += wait
			c.logf("* rate limited, waiting %s", wait.Round(time.Millisecond))
			if pe := pause(wait); pe != nil {
				return nil, pe
			}
			backoff *= 2
			continue
		case kind == KindServer && r.readSafe():
			transientTries++
			if transientTries < maxAttempts {
				if pe := pause(backoff); pe != nil {
					return nil, pe
				}
				backoff *= 2
				continue
			}
		}
		return nil, c.errorFor(r, resp, me, kind, "")
	}
}
