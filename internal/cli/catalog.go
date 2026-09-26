package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
	"github.com/wir-drei-digital/meta-ads-cli/internal/guard"
)

// catalogCommand is one command as `metaads commands --json` publishes it.
// schema_version is the contract a consumer pins to; new optional keys may
// appear under the same version.
type catalogCommand struct {
	Command string   `json:"command"`
	Use     string   `json:"use"`
	Summary string   `json:"summary"`
	Flags   []string `json:"flags,omitempty"`
}

func (a *app) commandsCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "commands",
		Short: "List every command (--json: the machine-readable catalog with the guard's rules)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmds := catalogOf(cmd.Root())
			if !asJSON {
				for _, c := range cmds {
					fmt.Fprintf(a.stdout, "%-28s %s\n", c.Command, c.Summary)
				}
				return nil
			}
			out := struct {
				SchemaVersion int              `json:"schema_version"`
				APIVersion    string           `json:"api_version"`
				Commands      []catalogCommand `json:"commands"`
				Guard         guard.Catalog    `json:"guard"`
			}{SchemaVersion: 1, APIVersion: config.DefaultAPIVersion, Commands: cmds, Guard: guard.Rules()}
			enc := json.NewEncoder(a.stdout)
			enc.SetEscapeHTML(false)
			return enc.Encode(out)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the machine-readable catalog")
	return cmd
}

// catalogOf walks the command tree and lists every leaf command with its own
// flags, so the catalog is derived from the code and cannot drift from it.
func catalogOf(root *cobra.Command) []catalogCommand {
	var out []catalogCommand
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			if sub.HasSubCommands() {
				walk(sub)
				continue
			}
			e := catalogCommand{Command: strings.TrimPrefix(sub.CommandPath(), root.Name()+" "), Use: sub.UseLine(), Summary: sub.Short}
			sub.LocalFlags().VisitAll(func(f *pflag.Flag) {
				if f.Name != "help" {
					e.Flags = append(e.Flags, "--"+f.Name)
				}
			})
			out = append(out, e)
		}
	}
	walk(root)
	return out
}
