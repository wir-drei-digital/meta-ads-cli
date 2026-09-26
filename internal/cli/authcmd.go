package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
	"github.com/wir-drei-digital/meta-ads-cli/internal/auth"
	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
)

// authStatus is `metaads auth status`: what is configured, never a secret.
type authStatus struct {
	Mode              string   `json:"mode"` // token or none
	Source            string   `json:"source,omitempty"`
	AppID             string   `json:"app_id,omitempty"`
	AppSecret         string   `json:"app_secret"` // set or missing
	AdAccountID       string   `json:"ad_account_id,omitempty"`
	Currency          string   `json:"currency,omitempty"`
	DailyBudgetCap    string   `json:"daily_budget_cap,omitempty"`
	LifetimeBudgetCap string   `json:"lifetime_budget_cap,omitempty"`
	ReadOnly          bool     `json:"read_only"`
	TokenExpiresAt    string   `json:"token_expires_at,omitempty"`
	IsValid           *bool    `json:"is_valid,omitempty"` // --check only
	Scopes            []string `json:"scopes,omitempty"`
	AdAccounts        []string `json:"ad_accounts,omitempty"`
	Missing           []string `json:"missing,omitempty"`
	Hint              string   `json:"hint,omitempty"`
}

func (a *app) authCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Inspect and renew the configured credential", RunE: groupRunE}
	var check bool
	status := &cobra.Command{
		Use:   "status",
		Short: "Show the configured credential as one line of JSON (never prints a token or secret)",
		Long: "Show the configured credential as one line of JSON. Offline by default. With --check it asks\n" +
			"Meta: with the app ID and secret through debug_token (validity, expiry, scopes, ad accounts),\n" +
			"otherwise with a call to me (validity only).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s := a.authStatus()
			if check {
				if err := a.checkToken(cmd.Context(), &s); err != nil {
					return err
				}
			}
			enc := json.NewEncoder(a.stdout)
			enc.SetEscapeHTML(false)
			return enc.Encode(s)
		},
	}
	status.Flags().BoolVar(&check, "check", false, "ask Meta whether the token is valid, when it expires and what it reaches")
	refresh := &cobra.Command{
		Use:   "refresh",
		Short: "Renew a 60-day token and store the new one (needs the app ID and secret)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return a.refreshToken(cmd.Context()) },
	}
	cmd.AddCommand(status, refresh)
	return cmd
}

func (a *app) authStatus() authStatus {
	s := authStatus{Mode: "none", Source: a.res.TokenFrom, AppID: a.res.AppID, AppSecret: "missing",
		Currency: a.res.Currency, ReadOnly: a.res.ReadOnly, TokenExpiresAt: a.res.TokenExpiresAt}
	if a.res.AppSecret != "" {
		s.AppSecret = "set"
	}
	if a.res.AdAccountID != "" {
		s.AdAccountID = "act_" + a.res.AdAccountID
	}
	if c := a.res.DailyCap; c != nil {
		s.DailyBudgetCap = capString(c)
	}
	if c := a.res.LifetimeCap; c != nil {
		s.LifetimeBudgetCap = capString(c)
	}
	var hints []string
	if a.res.AccessToken == "" {
		s.Missing = append(s.Missing, "access_token")
		hints = append(hints, "run `metaads init`, or store a token with `metaads config set access-token` (reads stdin)")
	} else {
		s.Mode = "token"
	}
	if s.AdAccountID == "" {
		s.Missing = append(s.Missing, "ad_account_id")
		hints = append(hints, "set the ad account with `metaads config set ad-account-id <id>`")
	}
	if s.Currency == "" {
		hints = append(hints, "no currency: set it with `metaads config set currency <code>` before the budget caps")
	}
	if s.DailyBudgetCap == "" || s.LifetimeBudgetCap == "" {
		hints = append(hints, "a budget without a matching cap is refused; a person sets the caps with `metaads config set daily-budget-cap <amount>` and `metaads config set lifetime-budget-cap <amount>`")
	}
	for _, c := range []*config.Cap{a.res.DailyCap, a.res.LifetimeCap} {
		if c != nil && a.res.AdAccountID != "" && c.Account != a.res.AdAccountID {
			hints = append(hints, fmt.Sprintf("a budget cap was set for another ad account than act_%s; budgets are refused until a person sets the caps again for it", a.res.AdAccountID))
			break
		}
	}
	if s.AppSecret == "missing" {
		hints = append(hints, "no app secret: requests carry no appsecret_proof; store it with `metaads config set app-secret` (reads stdin)")
	}
	s.Hint = strings.Join(hints, "; ")
	return s
}

// checkToken asks Meta about the token and fills the --check fields.
func (a *app) checkToken(ctx context.Context, s *authStatus) error {
	if a.res.AccessToken == "" {
		return api.Usagef("no access token to check; run `metaads init` or `metaads config set access-token`")
	}
	if a.res.AppID != "" && a.res.AppSecret != "" {
		info, err := auth.DebugToken(ctx, a.client, config.DefaultAPIVersion, a.res.AppID, a.res.AppSecret, a.res.AccessToken)
		if err != nil {
			return err
		}
		valid := info.IsValid
		s.IsValid, s.Scopes, s.AdAccounts = &valid, info.Scopes, info.AdAccounts()
		if !valid {
			// An invalid token's expires_at is often 0, which would read as
			// "never"; the recorded expiry stays as it was.
			return nil
		}
		s.TokenExpiresAt = expiryString(info.ExpiresAt)
		return a.recordExpiry(s.TokenExpiresAt)
	}
	_, err := a.client.Do(ctx, api.Request{Method: http.MethodGet, Path: config.DefaultAPIVersion + "/me",
		Query: url.Values{"fields": {"id,name"}}, Class: api.ClassRead})
	valid := err == nil
	var e *api.Error
	if err != nil && (!errors.As(err, &e) || e.Kind != api.KindAuth) {
		return err
	}
	s.IsValid = &valid
	note := "without the app ID and secret only validity can be checked, not expiry or scopes"
	if !valid {
		note = e.Message
	}
	s.Hint = strings.TrimPrefix(s.Hint+"; "+note, "; ")
	return nil
}

// capString shows a cap with the ad account it belongs to: "30.00 CHF for act_1".
func capString(c *config.Cap) string {
	s := config.FormatMinor(c.Minor, c.Currency)
	if c.Account == "" {
		return s + " (bound to no ad account; set it again)"
	}
	return s + " for act_" + c.Account
}

func expiryString(unix int64) string {
	if unix == 0 {
		return "never"
	}
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}

// recordExpiry stores the expiry when the token comes from the config file;
// an environment token's expiry belongs to whoever sets the environment.
func (a *app) recordExpiry(exp string) error {
	if a.res.TokenFrom != "config" {
		return nil
	}
	return a.updateConfig(func(c *config.Config) { c.TokenExpiresAt = exp })
}

func (a *app) refreshToken(ctx context.Context) error {
	switch {
	case a.res.TokenFrom == "env":
		return api.Usagef("the token comes from META_ADS_ACCESS_TOKEN; renew it where that variable is set, or store the token with `metaads config set access-token` so metaads can renew it")
	case a.res.TokenFrom == "":
		return api.Usagef("no stored token to renew; run `metaads init`")
	case a.res.AppID == "" || a.res.AppSecret == "":
		return api.Usagef("renewing a token needs the app ID and app secret; set them with `metaads config set app-id <id>` and `metaads config set app-secret` (reads stdin)")
	}
	ex, err := auth.Exchange(ctx, a.client, config.DefaultAPIVersion, a.res.AppID, a.res.AppSecret, a.res.AccessToken)
	if err != nil {
		return err
	}
	exp := "never"
	if ex.ExpiresIn > 0 {
		exp = time.Now().Add(time.Duration(ex.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	if err := a.updateConfig(func(c *config.Config) { c.AccessToken, c.TokenExpiresAt = ex.AccessToken, exp }); err != nil {
		return err
	}
	return json.NewEncoder(a.stdout).Encode(map[string]string{"expires_at": exp})
}
