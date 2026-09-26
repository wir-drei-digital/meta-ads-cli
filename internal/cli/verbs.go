package cli

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
	"github.com/wir-drei-digital/meta-ads-cli/internal/config"
	"github.com/wir-drei-digital/meta-ads-cli/internal/guard"
	"github.com/wir-drei-digital/meta-ads-cli/internal/route"
)

func (a *app) getCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <path>",
		Short: "Read a node or an edge (GET), including insights",
		Long: "Read a node or an edge of the Graph API. The node act is the configured ad account.\n\n" +
			"  metaads get act --fields name,currency,account_status,spend_cap,amount_spent\n" +
			"  metaads get act/campaigns --fields id,name,status,daily_budget --all\n" +
			"  metaads get act/insights --param level=ad --param date_preset=last_7d --fields ad_name,spend,clicks\n\n" +
			"--param values that are JSON (time_range, filtering, breakdowns) are written as JSON.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return a.runGet(cmd, args[0]) },
	}
	f := cmd.Flags()
	f.String("fields", "", "fields to return, comma-separated")
	f.StringArray("param", nil, "query parameter as key=value (repeatable)")
	f.Bool("all", false, "follow the after cursor and print one JSON array of every data entry")
	f.Int("max-pages", 100, "page limit for --all")
	f.String("output", "", "write the response body to a file instead of stdout")
	return cmd
}

func (a *app) postCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "post <path>",
		Short: "Create or change something (POST); guarded",
		Long: "Create or change something through the Graph API. --data is one JSON object and is sent\n" +
			"as form fields, the encoding Meta documents; nested values become JSON strings.\n\n" +
			"  metaads post act/campaigns --validate-only --data @campaign.json\n" +
			"  metaads post act/adimages --file filename=@motiv.png\n" +
			"  metaads post <id> --force --data '{\"status\":\"ACTIVE\"}'\n\n" +
			"Starting delivery, changing an existing budget and anything outside campaign editing need\n" +
			"--force. Budgets above the configured caps are refused whatever the flags.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return a.runPost(cmd, args[0]) },
	}
	f := cmd.Flags()
	f.String("data", "", "the fields as one JSON object: literal, @file.json, or - for stdin")
	f.StringArray("file", nil, "upload a file as field=@path (repeatable); sends multipart/form-data")
	f.Bool("validate-only", false, "let Meta validate a create without applying it (campaigns, adsets, ads, adcreatives)")
	f.Bool("force", false, "confirm a delete-, spend- or admin-class request")
	f.String("output", "", "write the response body to a file instead of stdout")
	return cmd
}

func (a *app) deleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <path>",
		Short: "Delete something (DELETE); needs --force",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return a.runDelete(cmd, args[0]) },
	}
	f := cmd.Flags()
	f.StringArray("param", nil, "form field as key=value (repeatable), such as hash=<image hash>")
	f.Bool("force", false, "confirm the delete")
	f.String("output", "", "write the response body to a file instead of stdout")
	return cmd
}

func (a *app) route(path, verb string) (route.Route, error) {
	rt, err := route.Parse(path, verb, a.res.AdAccountID, config.DefaultAPIVersion)
	if err != nil {
		return route.Route{}, api.Usagef("%v", err)
	}
	return rt, nil
}

// parseParams reads --param key=value flags. A key given twice is refused:
// the Graph API would read one of them, and the guard must know which.
func parseParams(cmd *cobra.Command) (url.Values, error) {
	raw, _ := cmd.Flags().GetStringArray("param")
	params := url.Values{}
	for _, p := range raw {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, api.Usagef("--param wants key=value, got %q", p)
		}
		if params.Has(k) {
			return nil, api.Usagef("--param %s is given twice", k)
		}
		params.Set(k, v)
	}
	return params, nil
}

// parseFiles reads --file field=@path flags.
func parseFiles(cmd *cobra.Command) ([]api.File, error) {
	raw, _ := cmd.Flags().GetStringArray("file")
	var files []api.File
	seen := map[string]bool{}
	for _, f := range raw {
		field, ref, ok := strings.Cut(f, "=")
		if !ok || field == "" || !strings.HasPrefix(ref, "@") || len(ref) < 2 {
			return nil, api.Usagef("--file wants field=@path, got %q", f)
		}
		if seen[field] {
			return nil, api.Usagef("--file %s is given twice", field)
		}
		seen[field] = true
		files = append(files, api.File{Field: field, Path: ref[1:]})
	}
	return files, nil
}

func (a *app) send(cmd *cobra.Command, req api.Request) error {
	out, err := openOutput(flagString(cmd, "output"))
	if err != nil {
		return err
	}
	defer out.discard()
	resp, err := a.client.Do(cmd.Context(), req)
	if err != nil {
		return err
	}
	return a.writeResponse(resp, out)
}

func (a *app) runGet(cmd *cobra.Command, path string) error {
	rt, err := a.route(path, http.MethodGet)
	if err != nil {
		return err
	}
	params, err := parseParams(cmd)
	if err != nil {
		return err
	}
	dec, err := guard.Check(guard.Request{Verb: http.MethodGet, Route: rt, Params: params}, a.policy())
	if err != nil {
		return api.Usagef("GET %s: %v", rt.Path(), err)
	}
	if fields := flagString(cmd, "fields"); fields != "" {
		params.Set("fields", fields)
	}
	req := api.Request{Method: http.MethodGet, Path: rt.Path(), Query: params, Class: dec.Class}
	if all, _ := cmd.Flags().GetBool("all"); all {
		out, err := openOutput(flagString(cmd, "output"))
		if err != nil {
			return err
		}
		defer out.discard()
		return a.runAll(cmd, req, out)
	}
	return a.send(cmd, req)
}

func (a *app) runPost(cmd *cobra.Command, path string) error {
	rt, err := a.route(path, http.MethodPost)
	if err != nil {
		return err
	}
	raw, err := a.readJSONBody(cmd)
	if err != nil {
		return err
	}
	body := map[string]any{}
	if raw != nil {
		if body, err = guard.ParseBody(raw); err != nil {
			return api.Usagef("--data: %v", err)
		}
	}
	files, err := parseFiles(cmd)
	if err != nil {
		return err
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Field
	}
	validateOnly, _ := cmd.Flags().GetBool("validate-only")
	force, _ := cmd.Flags().GetBool("force")
	dec, err := guard.Check(guard.Request{Verb: http.MethodPost, Route: rt, Body: body, Files: names,
		ValidateOnly: validateOnly, Force: force}, a.policy())
	if err != nil {
		return api.Usagef("POST %s: %v", rt.Path(), err)
	}
	// The form carries exactly the object the guard checked; the only
	// addition is execution_options, which --validate-only owns.
	form, err := api.EncodeForm(body)
	if err != nil {
		return api.Usagef("--data: %v", err)
	}
	if validateOnly {
		form.Set("execution_options", `["validate_only"]`)
	}
	req := api.Request{Method: http.MethodPost, Path: rt.Path(), Form: form, Class: dec.Class, ValidateOnly: validateOnly}
	if len(files) > 0 {
		mp, ct, err := api.EncodeMultipart(form, files)
		if err != nil {
			return api.Usagef("--file: %v", err)
		}
		req.Form, req.Multipart, req.ContentType = nil, mp, ct
	}
	return a.send(cmd, req)
}

func (a *app) runDelete(cmd *cobra.Command, path string) error {
	rt, err := a.route(path, http.MethodDelete)
	if err != nil {
		return err
	}
	params, err := parseParams(cmd)
	if err != nil {
		return err
	}
	force, _ := cmd.Flags().GetBool("force")
	dec, err := guard.Check(guard.Request{Verb: http.MethodDelete, Route: rt, Params: params, Force: force}, a.policy())
	if err != nil {
		return api.Usagef("DELETE %s: %v", rt.Path(), err)
	}
	return a.send(cmd, api.Request{Method: http.MethodDelete, Path: rt.Path(), Form: params, Class: dec.Class})
}
