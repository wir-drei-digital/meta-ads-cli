package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
)

const configKeys = "access-token, app-secret, app-id, ad-account-id, currency, daily-budget-cap, lifetime-budget-cap and read-only"

func (a *app) configCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Manage the metaads config file", RunE: groupRunE}
	set := &cobra.Command{Use: "set", Short: "Set a config value", Args: cobra.ArbitraryArgs, RunE: keyGroupRunE("set")}
	set.AddCommand(
		a.secretSetter("access-token", "system user access token", "META_ADS_ACCESS_TOKEN", func(c *config.Config, v string) {
			c.AccessToken, c.TokenExpiresAt = v, ""
		}, "access token saved; check it with `metaads auth status --check`"),
		a.secretSetter("app-secret", "app secret", "META_ADS_APP_SECRET", func(c *config.Config, v string) { c.AppSecret = v },
			"app secret saved; requests now carry appsecret_proof"),
		a.idSetter("app-id", "Meta app ID", "META_ADS_APP_ID", config.NormalizeAppID, func(c *config.Config, v string) string {
			c.AppID = v
			return ""
		}, ""),
		a.idSetter("ad-account-id", "ad account (e.g. act_1234567890)", "META_ADS_AD_ACCOUNT_ID", config.NormalizeAccountID,
			func(c *config.Config, v string) string {
				c.AdAccountID = v
				if (c.DailyCap != nil && c.DailyCap.Account != v) || (c.LifetimeCap != nil && c.LifetimeCap.Account != v) {
					return "note: a budget cap was set for another ad account; budgets are refused until you set the caps again for act_" + v
				}
				return ""
			}, "act_"),
		&cobra.Command{
			Use:   "currency <code>",
			Short: "Set the ad account's currency (ISO code, e.g. CHF); budget caps are entered in it",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				cur, err := config.NormalizeCurrency(args[0])
				if err != nil {
					return api.Usagef("currency: %v", err)
				}
				var stale bool
				if err := a.updateConfig(func(c *config.Config) {
					c.Currency = cur
					stale = (c.DailyCap != nil && c.DailyCap.Currency != cur) || (c.LifetimeCap != nil && c.LifetimeCap.Currency != cur)
				}); err != nil {
					return err
				}
				if stale {
					fmt.Fprintf(a.stderr, "note: a budget cap was entered in another currency; budgets are refused until you set the caps again in %s\n", cur)
				}
				fmt.Fprintf(a.stdout, "currency = %s\n", cur)
				return nil
			},
		},
		a.capSetter("daily-budget-cap", "daily", func(c *config.Config, cp *config.Cap) { c.DailyCap = cp }),
		a.capSetter("lifetime-budget-cap", "lifetime", func(c *config.Config, cp *config.Cap) { c.LifetimeCap = cp }),
		&cobra.Command{
			Use:   "read-only <true|false>",
			Short: "Persist read-only mode",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				v, err := strconv.ParseBool(args[0])
				if err != nil {
					return api.Usagef("read-only wants true or false, got %q", args[0])
				}
				if err := a.updateConfig(func(c *config.Config) { c.ReadOnly = v }); err != nil {
					return err
				}
				fmt.Fprintf(a.stdout, "read_only = %v\n", v)
				return nil
			},
		},
	)

	unset := &cobra.Command{Use: "unset", Short: "Remove a config value", Args: cobra.ArbitraryArgs, RunE: keyGroupRunE("unset")}
	for _, k := range []struct {
		use, done string
		stored    func(config.Config) bool
		clear     func(*config.Config)
	}{
		{"access-token", "access token removed from this machine; it stays valid until you remove it, or the system user, in the Business Portfolio",
			func(c config.Config) bool { return c.AccessToken != "" }, func(c *config.Config) { c.AccessToken, c.TokenExpiresAt = "", "" }},
		{"app-secret", "app secret removed; requests no longer carry appsecret_proof",
			func(c config.Config) bool { return c.AppSecret != "" }, func(c *config.Config) { c.AppSecret = "" }},
		{"app-id", "app-id removed",
			func(c config.Config) bool { return c.AppID != "" }, func(c *config.Config) { c.AppID = "" }},
		{"ad-account-id", "ad-account-id removed",
			func(c config.Config) bool { return c.AdAccountID != "" }, func(c *config.Config) { c.AdAccountID = "" }},
		{"currency", "currency removed; no budget can be set until it is set again",
			func(c config.Config) bool { return c.Currency != "" }, func(c *config.Config) { c.Currency = "" }},
		{"daily-budget-cap", "daily-budget-cap removed; no daily budget can be set until a new cap is configured",
			func(c config.Config) bool { return c.DailyCap != nil }, func(c *config.Config) { c.DailyCap = nil }},
		{"lifetime-budget-cap", "lifetime-budget-cap removed; no lifetime budget can be set until a new cap is configured",
			func(c config.Config) bool { return c.LifetimeCap != nil }, func(c *config.Config) { c.LifetimeCap = nil }},
		{"read-only", "read-only removed",
			func(c config.Config) bool { return c.ReadOnly }, func(c *config.Config) { c.ReadOnly = false }},
	} {
		unset.AddCommand(&cobra.Command{
			Use: k.use, Short: "Remove " + k.use, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := config.Load()
				if err != nil {
					return api.Usagef("%v", err)
				}
				if !k.stored(c) {
					fmt.Fprintf(a.stdout, "%s was not set\n", k.use)
					return nil
				}
				k.clear(&c)
				if err := config.Save(c); err != nil {
					return api.Usagef("%v", err)
				}
				fmt.Fprintln(a.stdout, k.done)
				return nil
			},
		})
	}

	path := &cobra.Command{
		Use: "path", Short: "Print the config file location", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := config.Path()
			if err != nil {
				return api.Usagef("%v", err)
			}
			fmt.Fprintln(a.stdout, p)
			return nil
		},
	}
	cmd.AddCommand(set, unset, path)
	return cmd
}

// secretSetter reads a secret from stdin, never from the command line, which
// every process on the machine can see.
func (a *app) secretSetter(use, what, envName string, assign func(*config.Config, string), done string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: "Read the " + what + " from stdin and store it (0600)",
		Long: "Read the " + what + " from stdin and store it in the config file (0600). It never comes from\n" +
			"the command line:\n  printf '%s' \"$VALUE\" | metaads config set " + use + "\n\n" + envName + ", when set, wins.",
		// Not cobra.NoArgs: its message quotes the argument, which here is
		// the secret itself, onto stderr.
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return api.Usagef("the %s never comes from the command line, where every process on the machine can read it; "+
					"pipe it on stdin: printf '%%s' \"$VALUE\" | metaads config set %s", what, use)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := io.ReadAll(io.LimitReader(a.stdin, 64<<10))
			if err != nil {
				return api.Usagef("reading stdin: %v", err)
			}
			v := strings.TrimSpace(strings.TrimPrefix(string(raw), "\ufeff"))
			if v == "" {
				return api.Usagef("no value on stdin; usage: printf '%%s' \"$VALUE\" | metaads config set %s", use)
			}
			if strings.ContainsAny(v, " \t\r\n") {
				return api.Usagef("the %s on stdin contains whitespace; pass exactly one value", what)
			}
			if err := a.updateConfig(func(c *config.Config) { assign(c, v) }); err != nil {
				return err
			}
			a.warnEnv(envName)
			fmt.Fprintln(a.stdout, done)
			return nil
		},
	}
	// cobra's flag errors quote the argument ("unknown shorthand flag: 'S'
	// in -SECRET"), which here may be the secret.
	cmd.SetFlagErrorFunc(func(*cobra.Command, error) error {
		return api.Usagef("config set %s takes no flags or arguments; pipe the value on stdin: printf '%%s' \"$VALUE\" | metaads config set %s", use, use)
	})
	return cmd
}

// idSetter stores an identifier; assign returns a note for stderr, or "".
func (a *app) idSetter(use, what, envName string, normalize func(string) (string, error), assign func(*config.Config, string) string, prefix string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <id>",
		Short: "Save the " + what,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := normalize(args[0])
			if err != nil {
				return api.Usagef("%s: %v", use, err)
			}
			note := ""
			if err := a.updateConfig(func(c *config.Config) { note = assign(c, id) }); err != nil {
				return err
			}
			a.warnEnv(envName)
			if note != "" {
				fmt.Fprintln(a.stderr, note)
			}
			fmt.Fprintf(a.stdout, "%s = %s%s\n", strings.ReplaceAll(use, "-", "_"), prefix, id)
			return nil
		},
	}
}

func (a *app) capSetter(use, kind string, assign func(*config.Config, *config.Cap)) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <amount>",
		Short: "Set the highest " + kind + " budget metaads may ever set, in the account currency",
		Long: "Set the " + use + ": the highest " + kind + " budget metaads accepts in any request, in the\n" +
			"configured currency (for example 30 or 29.50, a dot as the decimal separator). --force does not\n" +
			"override it, and no environment variable or flag can change it. Without it no " + kind + " budget\n" +
			"can be set at all.\n\n" +
			"The cap belongs to the ad account and the currency in the config file when it is set: for any\n" +
			"other account, META_ADS_AD_ACCOUNT_ID included, budgets are refused until it is set again.\n\n" +
			"Meta may spend up to 75% more than a daily budget on a single day, and at most 7 times the daily\n" +
			"budget in a week. A lifetime budget is never exceeded.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return api.Usagef("%v", err)
			}
			if c.Currency == "" {
				return api.Usagef("%s: set the currency first with `metaads config set currency <code>` (or run `metaads init`)", use)
			}
			// The file's account, never the environment's: an agent can set
			// META_ADS_AD_ACCOUNT_ID, and a cap is a person's decision for one
			// account.
			if c.AdAccountID == "" {
				return api.Usagef("%s: set the ad account first with `metaads config set ad-account-id <id>`; "+
					"a cap belongs to the ad account in the config file, not to META_ADS_AD_ACCOUNT_ID", use)
			}
			minor, err := config.ParseMajor(args[0], c.Currency)
			if err != nil {
				return api.Usagef("%s: %v", use, err)
			}
			cp := &config.Cap{Minor: minor, Currency: c.Currency, Account: c.AdAccountID}
			assign(&c, cp)
			if err := config.Save(c); err != nil {
				return api.Usagef("%v", err)
			}
			fmt.Fprintf(a.stdout, "%s = %s (%d in Meta's minor unit)\n", strings.ReplaceAll(use, "-", "_"), capString(cp), minor)
			return nil
		},
	}
}

// updateConfig loads the config file, applies edit and saves it.
func (a *app) updateConfig(edit func(*config.Config)) error {
	c, err := config.Load()
	if err != nil {
		return api.Usagef("%v", err)
	}
	edit(&c)
	if err := config.Save(c); err != nil {
		return api.Usagef("%v", err)
	}
	return nil
}

// warnEnv says on stderr when an environment variable overrides what was
// just saved.
func (a *app) warnEnv(name string) {
	if a.res.FromEnv[name] {
		fmt.Fprintf(a.stderr, "note: %s is set in this environment and wins over the config file\n", name)
	}
}

// keyGroupRunE answers `config set` or `config unset` given something that is
// not one of their keys. An environment-variable name is the expected
// mistake, so it is answered with the key it plainly means.
func keyGroupRunE(verb string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			if k := suggestConfigKey(args[0]); k != "" {
				return api.Usagef("config keys are %s, not environment variable names; did you mean `metaads config %s %s`?", configKeys, verb, k)
			}
		}
		return groupRunE(cmd, args)
	}
}

func suggestConfigKey(given string) string {
	switch strings.TrimPrefix(strings.ToLower(given), "meta_ads_") {
	case "access_token", "token":
		return "access-token"
	case "app_secret", "secret":
		return "app-secret"
	case "app_id", "appid":
		return "app-id"
	case "ad_account_id", "account_id", "account":
		return "ad-account-id"
	case "read_only", "readonly":
		return "read-only"
	case "daily_budget_cap", "budget_cap":
		return "daily-budget-cap"
	case "lifetime_budget_cap":
		return "lifetime-budget-cap"
	}
	return ""
}
