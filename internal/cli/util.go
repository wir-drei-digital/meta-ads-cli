package cli

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
)

func flagString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

// groupRunE answers a namespace invoked without a subcommand: that is a
// usage error, not a success that prints help.
func groupRunE(cmd *cobra.Command, args []string) error {
	var names []string
	for _, sub := range cmd.Commands() {
		if !sub.Hidden && sub.Name() != "help" && sub.Name() != "completion" {
			names = append(names, sub.Name())
		}
	}
	return api.Usagef("%s needs a subcommand: %s", cmd.CommandPath(), strings.Join(names, ", "))
}

// jsonCompact marshals v without HTML escaping.
func jsonCompact(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
