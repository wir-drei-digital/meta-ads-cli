package cli

import (
	"bytes"
	"encoding/json"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/wir-drei-digital/meta-ads-cli/internal/api"
)

// runAll follows the after cursor and writes ONE JSON array of the merged
// data entries. It repeats the request with the original parameters instead
// of following Meta's next URL, which keeps the host pinned and credentials
// out of URLs. Hitting --max-pages is not silent success: the partial array
// is written and the run ends as incomplete.
func (a *app) runAll(cmd *cobra.Command, base api.Request, out *outputFile) error {
	maxPages, _ := cmd.Flags().GetInt("max-pages")
	if maxPages < 1 {
		return api.Usagef("--max-pages must be at least 1, got %d", maxPages)
	}
	var rows []json.RawMessage
	after, complete := "", false
	for page := 0; page < maxPages; page++ {
		q := url.Values{}
		for k, v := range base.Query {
			q[k] = append([]string(nil), v...)
		}
		// A cursor in --param would start mid-stream and drop the pages
		// before it, so the walk always starts at the first page.
		q.Del("after")
		q.Del("before")
		if after != "" {
			q.Set("after", after)
		}
		req := base
		req.Query = q
		resp, err := a.client.Do(cmd.Context(), req)
		if err != nil {
			return err
		}
		var env struct {
			Data   json.RawMessage `json:"data"`
			Paging struct {
				Cursors struct {
					After string `json:"after"`
				} `json:"cursors"`
				Next string `json:"next"`
			} `json:"paging"`
		}
		if err := json.Unmarshal(resp.Body, &env); err != nil {
			return api.Usagef("--all expects a JSON object response, got: %.100s", resp.Body)
		}
		var pageRows []json.RawMessage
		if env.Data == nil || json.Unmarshal(env.Data, &pageRows) != nil {
			return api.Usagef("--all needs a response with a data array; %s returns something else", base.Path)
		}
		rows = append(rows, pageRows...)
		if env.Paging.Next == "" || env.Paging.Cursors.After == "" {
			complete = true
			break
		}
		after = env.Paging.Cursors.After
	}
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, r := range rows {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(r)
	}
	buf.WriteString("]\n")
	if err := a.writeResponse(&api.Response{Body: buf.Bytes()}, out); err != nil {
		return err
	}
	if !complete {
		return &api.Error{Kind: api.KindIncomplete, Message: "hit --max-pages before the last page; the output is partial"}
	}
	return nil
}
