// Package cli builds the metaads command tree and turns process arguments
// into one guarded API call, or one merged page walk.
//
// Two rules shape everything here: stdout carries the API response and
// nothing else, and every failure leaves as one line of JSON on stderr so an
// agent can parse it without heuristics.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
	"github.com/wir-drei-digital/meta-ads-cli/internal/guard"
)

type app struct {
	client *api.Client
	res    config.Resolved
	// configErr is why the configuration could not be resolved. Only
	// version, commands, help and the config commands run without it, so a
	// person can find and repair the file; every other command reports it.
	configErr      error
	stdout, stderr io.Writer
	stdin          io.Reader
}

// Execute is the process entry point: it resolves the configuration, runs
// the command tree and returns the exit code (0 success, 1 API or network
// failure, 2 usage error).
func Execute(args []string) int {
	// Ctrl-C and SIGTERM cancel the request in flight and any retry wait.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a := &app{stdout: os.Stdout, stderr: os.Stderr, stdin: os.Stdin}
	a.configure(os.Getenv, contextSleeper(ctx))
	return a.runContext(ctx, args)
}

// configure resolves the config file and the environment and builds the
// client. A failure is kept in configErr rather than returned: prepare
// reports it for every command that needs the configuration.
func (a *app) configure(getenv func(string) string, sleep func(time.Duration)) {
	a.res, a.configErr = config.Resolve(getenv)
	a.client = &api.Client{BaseURL: a.res.APIBase, Token: a.res.AccessToken, AppSecret: a.res.AppSecret,
		ReadOnly: a.res.ReadOnly, AllowCustomBase: a.res.AllowCustomBase, Sleep: sleep}
}

// prepare is the root's persistent pre-run: it reports an unresolvable
// configuration and applies --timeout and --verbose to the client.
func (a *app) prepare(cmd *cobra.Command, args []string) error {
	if err := a.requireConfig(cmd); err != nil {
		return err
	}
	if d, err := cmd.Flags().GetDuration("timeout"); err == nil {
		a.client.Timeout = d
	}
	if v, _ := cmd.Flags().GetBool("verbose"); v {
		a.client.Verbose = a.stderr
	}
	return nil
}

// requireConfig lets only version, commands, help and the config commands
// run with an unresolvable configuration.
func (a *app) requireConfig(cmd *cobra.Command) error {
	if a.configErr == nil {
		return nil
	}
	for c := cmd; c.HasParent(); c = c.Parent() {
		if !c.Parent().HasParent() { // c is a top-level command
			switch c.Name() {
			case "version", "commands", "config", "help":
				return nil
			}
		}
	}
	return api.Usagef("cannot load the configuration: %v", a.configErr)
}

// policy is what the guard reads from the configuration.
func (a *app) policy() guard.Policy {
	return guard.Policy{ReadOnly: a.res.ReadOnly, Currency: a.res.Currency, DailyCap: a.res.DailyCap, LifetimeCap: a.res.LifetimeCap}
}

// contextSleeper returns a sleep that gives up as soon as ctx is done.
func contextSleeper(ctx context.Context) func(time.Duration) {
	return func(d time.Duration) {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
		case <-t.C:
		}
	}
}

func (a *app) run(args []string) int { return a.runContext(context.Background(), args) }

func (a *app) runContext(ctx context.Context, args []string) int {
	// cobra reads os.Args when the arguments are nil; an empty call must
	// stay an empty call.
	if args == nil {
		args = []string{}
	}
	root := a.newRoot()
	root.SetArgs(args)
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		return a.renderError(err)
	}
	return 0
}

// renderError writes err as one line of JSON on stderr and returns the exit
// code: 2 for usage errors, 1 for everything the API or the network produced.
func (a *app) renderError(err error) int {
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		apiErr = api.Usagef("%v", err) // anything else is a cobra parse or usage failure
	}
	raw, mErr := jsonCompact(apiErr)
	if mErr != nil {
		raw, mErr = jsonCompact(&api.Error{Kind: apiErr.Kind, Message: apiErr.Message, Status: apiErr.Status, RequestID: apiErr.RequestID})
		if mErr != nil {
			raw = []byte(`{"error":"internal: cannot render error","kind":"usage","details":null}`)
		}
	}
	fmt.Fprintln(a.stderr, string(raw))
	if apiErr.Kind == api.KindUsage {
		return 2
	}
	return 1
}
