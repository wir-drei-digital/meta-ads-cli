package api

import (
	"encoding/json"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"time"
)

// classifyTransport maps a network failure. A dial or DNS failure never
// reached Meta, and a read-safe call changes nothing, so both are safe to
// retry. Anything else may have been applied: the caller must verify state.
func classifyTransport(err error, readSafe bool) *Error {
	var opErr *net.OpError
	var dnsErr *net.DNSError
	presend := (errors.As(err, &opErr) && opErr.Op == "dial") || errors.As(err, &dnsErr)
	if readSafe || presend {
		return &Error{Kind: KindTransport, Message: err.Error()}
	}
	return &Error{Kind: KindOutcomeUnknown,
		Message: "the request may or may not have been applied: " + err.Error() + "; verify state before retrying"}
}

// retryAfter returns the Retry-After header as a duration, or 0.
func retryAfter(h http.Header) time.Duration {
	if secs, err := strconv.Atoi(h.Get("Retry-After")); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// jitter spreads retries so concurrent callers do not resynchronize.
func jitter() time.Duration { return time.Duration(rand.Int63n(int64(500 * time.Millisecond))) }

// usageWait is how long Meta asks to wait before calling again: the longest
// estimated_time_to_regain_access (minutes) in X-Business-Use-Case-Usage,
// else reset_time_duration (seconds) in X-Ad-Account-Usage. 0 when neither
// header says.
func usageWait(h http.Header) time.Duration {
	var buc map[string][]struct {
		ETA float64 `json:"estimated_time_to_regain_access"`
	}
	var longest time.Duration
	if json.Unmarshal([]byte(h.Get("X-Business-Use-Case-Usage")), &buc) == nil {
		for _, entries := range buc {
			for _, e := range entries {
				if d := time.Duration(e.ETA * float64(time.Minute)); d > longest {
					longest = d
				}
			}
		}
	}
	if longest > 0 {
		return longest
	}
	var acc struct {
		Reset float64 `json:"reset_time_duration"`
	}
	if json.Unmarshal([]byte(h.Get("X-Ad-Account-Usage")), &acc) == nil && acc.Reset > 0 {
		return time.Duration(acc.Reset * float64(time.Second))
	}
	return 0
}
