package cli

import (
	"time"

	"github.com/spf13/cobra"
)

func (a *app) newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "metaads",
		Short: "CLI for the Meta Marketing API, built for agents: JSON in, JSON out",
		Long: "CLI for the Meta Graph and Marketing API, built for agents: JSON in, JSON out.\n\n" +
			"Every request is get, post or delete on a Graph path such as act/campaigns, where act\n" +
			"is the configured ad account. stdout carries the API response and nothing else; errors\n" +
			"are one line of JSON on stderr. Exit codes: 0 success, 1 API or network error, 2 usage\n" +
			"error or guardrail refusal.\n\n" +
			"Run `metaads commands --json` for the catalog and the guard's rules.",
		// The root names no request. Printing help and exiting 0 would read as
		// success and break the stdout contract, so it is a usage error.
		RunE:              groupRunE,
		PersistentPreRunE: a.prepare,
		SilenceErrors:     true,
		SilenceUsage:      true,
	}
	pf := root.PersistentFlags()
	pf.Bool("verbose", false, "log requests to stderr (credentials and query strings are never logged)")
	pf.Duration("timeout", 60*time.Second, "per-attempt HTTP timeout")
	root.AddCommand(a.getCommand(), a.postCommand(), a.deleteCommand(), a.versionCommand(), a.commandsCommand(),
		a.configCommand(), a.authCommand(), a.initCommand())
	return root
}
