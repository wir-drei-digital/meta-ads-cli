// Package auth talks to Meta about the credential itself: whether a token is
// valid (debug_token), and exchanging a 60-day token for a new one.
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
)

// TokenInfo is debug_token's answer about a token.
type TokenInfo struct {
	IsValid        bool     `json:"is_valid"`
	AppID          string   `json:"app_id"`
	Type           string   `json:"type"`
	ExpiresAt      int64    `json:"expires_at"` // Unix seconds; 0 means the token never expires
	Scopes         []string `json:"scopes"`
	UserID         string   `json:"user_id"`
	GranularScopes []struct {
		Scope     string   `json:"scope"`
		TargetIDs []string `json:"target_ids"`
	} `json:"granular_scopes"`
}

// AdAccounts lists the ad accounts the token reaches through ads_management
// or ads_read, as act_<id>, sorted.
func (t TokenInfo) AdAccounts() []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range t.GranularScopes {
		if g.Scope != "ads_management" && g.Scope != "ads_read" {
			continue
		}
		for _, id := range g.TargetIDs {
			a := "act_" + strings.TrimPrefix(id, "act_")
			if !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	sort.Strings(out)
	return out
}

// DebugToken asks Meta about token, authenticated with the app token
// <appID>|<secret>. The token under inspection travels as input_token, the
// parameter debug_token defines; the client never logs or prints query
// strings.
func DebugToken(ctx context.Context, c *api.Client, version, appID, secret, token string) (TokenInfo, error) {
	resp, err := c.Do(ctx, api.Request{Method: http.MethodGet, Path: version + "/debug_token",
		Query: url.Values{"input_token": {token}}, Class: api.ClassRead, Bearer: appID + "|" + secret, NoProof: true})
	if err != nil {
		return TokenInfo{}, err
	}
	var env struct {
		Data TokenInfo `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &env); err != nil {
		return TokenInfo{}, &api.Error{Kind: api.KindValidation, Message: "debug_token returned an unreadable response"}
	}
	return env.Data, nil
}

// Exchanged is a renewed token.
type Exchanged struct {
	AccessToken string
	ExpiresIn   int64 // seconds; 0 when Meta sent no expiry
}

// Exchange renews a 60-day token with grant_type=fb_exchange_token. Its
// credentials are its parameters, so the request carries no Authorization
// header and no appsecret_proof.
func Exchange(ctx context.Context, c *api.Client, version, appID, secret, token string) (Exchanged, error) {
	q := url.Values{"grant_type": {"fb_exchange_token"}, "client_id": {appID}, "client_secret": {secret},
		"fb_exchange_token": {token}, "set_token_expires_in_60_days": {"true"}}
	resp, err := c.Do(ctx, api.Request{Method: http.MethodGet, Path: version + "/oauth/access_token", Query: q,
		Class: api.ClassRead, NoAuth: true})
	if err != nil {
		return Exchanged{}, err
	}
	var r struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(resp.Body, &r); err != nil || r.AccessToken == "" {
		return Exchanged{}, &api.Error{Kind: api.KindValidation, Message: "the token exchange returned no access_token"}
	}
	return Exchanged{AccessToken: r.AccessToken, ExpiresIn: r.ExpiresIn}, nil
}
