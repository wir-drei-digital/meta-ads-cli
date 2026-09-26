package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
	"github.com/wir-drei-digital/meta-ads-cli/internal/auth"
	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
)

func (a *app) initCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactive setup: token, app, ad account, budget caps (needs a terminal)",
		Long: "Interactive setup. Asks for the system user access token, the app ID and app secret, checks\n" +
			"them against Meta with read-only calls, lets you pick the ad account, shows its spending limit,\n" +
			"asks for a daily and a lifetime budget cap in the account currency, and saves everything to\n" +
			"the config file (0600). Needs a terminal: agents configure metaads with `metaads config set`\n" +
			"or the environment instead.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			isTTY := a.isTerminal
			if isTTY == nil {
				isTTY = stdioIsTerminal
			}
			if !isTTY() {
				return api.Usagef("metaads init is interactive and needs a terminal; use `metaads config set ...` instead")
			}
			p := a.prompt
			if p == nil {
				p = newTermPrompter(os.Stdin, a.stdout)
			}
			return a.runInit(cmd.Context(), p)
		},
	}
}

// adAccount is what init reads about an ad account. Meta sends spend_cap
// and amount_spent as strings of digits in the minor unit.
type adAccount struct {
	AccountID     string      `json:"account_id"`
	Name          string      `json:"name"`
	Currency      string      `json:"currency"`
	AccountStatus int         `json:"account_status"`
	SpendCap      json.Number `json:"spend_cap"`
	AmountSpent   json.Number `json:"amount_spent"`
}

func (a *app) runInit(ctx context.Context, p prompter) error {
	out := a.stdout
	fmt.Fprintln(out, "metaads setup: you need the system user's access token, and ideally the app ID and app secret.")
	for _, name := range []string{"META_ADS_ACCESS_TOKEN", "META_ADS_APP_SECRET", "META_ADS_APP_ID", "META_ADS_AD_ACCOUNT_ID", "META_ADS_READ_ONLY"} {
		if a.res.FromEnv[name] {
			fmt.Fprintf(out, "Note: %s is set in this shell and wins over what you save here.\n", name)
		}
	}
	token, err := ask(p, "System user access token (hidden): ", true)
	if err != nil {
		return api.Usagef("init: %v", err)
	}
	appID, err := p.Line("App ID (Enter to skip): ")
	if err != nil {
		return api.Usagef("init: %v", err)
	}
	if appID != "" {
		if appID, err = config.NormalizeAppID(appID); err != nil {
			// The answer is not quoted: this prompt echoes, so a secret
			// pasted into it by mistake must not also reach stderr.
			return api.Usagef("init: an app ID is digits only, such as 1234567890; the answer is not repeated here in case it was a secret")
		}
	}
	secret := ""
	if appID != "" {
		if secret, err = p.Secret("App secret (hidden, Enter to skip): "); err != nil {
			return api.Usagef("init: %v", err)
		}
	}
	if secret == "" {
		fmt.Fprintln(out, "Without the app secret, requests carry no appsecret_proof. Store it later with `metaads config set app-secret`.")
	}
	// Every call below is a read; read-only makes sure of it.
	client := *a.client
	client.Token, client.AppSecret, client.ReadOnly = token, secret, true
	expiry := ""
	if appID != "" && secret != "" {
		info, err := auth.DebugToken(ctx, &client, config.DefaultAPIVersion, appID, secret, token)
		if err != nil {
			return err
		}
		if !info.IsValid {
			return api.Usagef("init: Meta reports the token as not valid; generate a new one for the system user")
		}
		expiry = expiryString(info.ExpiresAt)
		fmt.Fprintf(out, "Token valid; expires: %s; scopes: %s.\n", expiry, strings.Join(info.Scopes, ", "))
	}
	var list struct {
		Data []adAccount `json:"data"`
	}
	if err := getJSON(ctx, &client, "me/adaccounts", url.Values{"fields": {"account_id,name,currency,account_status"}, "limit": {"100"}}, &list); err != nil {
		return err
	}
	if len(list.Data) == 0 {
		return api.Usagef("init: the token reaches no ad account; assign the ad account to the system user in the Business Portfolio")
	}
	acct := list.Data[0]
	if len(list.Data) > 1 {
		for i, ac := range list.Data {
			fmt.Fprintf(out, "  %d. %s (act_%s, %s)\n", i+1, ac.Name, ac.AccountID, ac.Currency)
		}
		n, err := askValid(p, out, fmt.Sprintf("Which ad account (1-%d)? ", len(list.Data)), func(s string) (int, error) {
			i, err := strconv.Atoi(s)
			if err != nil || i < 1 || i > len(list.Data) {
				return 0, fmt.Errorf("enter a number from 1 to %d", len(list.Data))
			}
			return i, nil
		})
		if err != nil {
			return err
		}
		acct = list.Data[n-1]
	}
	id, err := config.NormalizeAccountID(acct.AccountID)
	if err != nil {
		return api.Usagef("init: %v", err)
	}
	var details adAccount
	if err := getJSON(ctx, &client, "act_"+id, url.Values{"fields": {"name,currency,account_status,spend_cap,amount_spent"}}, &details); err != nil {
		return err
	}
	currency, err := config.NormalizeCurrency(details.Currency)
	if err != nil {
		return api.Usagef("init: %v", err)
	}
	fmt.Fprintf(out, "Ad account %s (act_%s), currency %s, status %s.\n", details.Name, id, currency, accountStatus(details.AccountStatus))
	if limit, _ := details.SpendCap.Int64(); limit > 0 {
		spent, _ := details.AmountSpent.Int64()
		fmt.Fprintf(out, "Account spending limit: %s, spent so far: %s.\n", config.FormatMinor(limit, currency), config.FormatMinor(spent, currency))
	} else {
		fmt.Fprintln(out, "Warning: this ad account has no spending limit. Set one in the Business Portfolio's billing settings: it is the ceiling that holds even when --force is used.")
	}
	parse := func(s string) (int64, error) { return config.ParseMajor(s, currency) }
	daily, err := askValid(p, out, fmt.Sprintf("Daily budget cap: the highest daily budget metaads may set, in %s (e.g. 30): ", currency), parse)
	if err != nil {
		return err
	}
	lifetime, err := askValid(p, out, fmt.Sprintf("Lifetime budget cap: the highest lifetime budget metaads may set, in %s (e.g. 300): ", currency), parse)
	if err != nil {
		return err
	}
	if err := a.updateConfig(func(c *config.Config) {
		c.AccessToken, c.AppID, c.AppSecret, c.AdAccountID, c.Currency = token, appID, secret, id, currency
		c.DailyCap = &config.Cap{Minor: daily, Currency: currency}
		c.LifetimeCap = &config.Cap{Minor: lifetime, Currency: currency}
		c.TokenExpiresAt = expiry
	}); err != nil {
		return err
	}
	path, _ := config.Path()
	fmt.Fprintf(out, "Saved to %s.\n", path)
	return nil
}

// getJSON reads a Graph path into v.
func getJSON(ctx context.Context, c *api.Client, path string, q url.Values, v any) error {
	resp, err := c.Do(ctx, api.Request{Method: http.MethodGet, Path: config.DefaultAPIVersion + "/" + path, Query: q, Class: api.ClassRead})
	if err != nil {
		return err
	}
	if err := json.Unmarshal(resp.Body, v); err != nil {
		return api.Usagef("init: unreadable response from %s: %.200s", path, resp.Body)
	}
	return nil
}

var accountStatuses = map[int]string{1: "active", 2: "disabled", 3: "unsettled", 7: "pending risk review",
	8: "pending settlement", 9: "in grace period", 100: "pending closure", 101: "closed"}

func accountStatus(code int) string {
	if s, ok := accountStatuses[code]; ok {
		return s
	}
	return fmt.Sprintf("code %d", code)
}

// askValid asks until parse accepts the answer, three times at most.
func askValid[T any](p prompter, out io.Writer, prompt string, parse func(string) (T, error)) (T, error) {
	var zero T
	for range 3 {
		s, err := ask(p, prompt, false)
		if err != nil {
			return zero, api.Usagef("init: %v", err)
		}
		v, err := parse(s)
		if err == nil {
			return v, nil
		}
		fmt.Fprintln(out, err)
	}
	return zero, api.Usagef("init: no valid answer after 3 attempts")
}
