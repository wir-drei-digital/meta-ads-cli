# metaads for agents

Paste this into the instructions of an agent that works on Meta ads through metaads.

## Contract

- Every request is `metaads get|post|delete <path>`. The path is a Graph API path, `<node>` or
  `<node>/<edge>`, optionally after a version (`v26.0/...`). The node `act` is the configured ad
  account: `act/campaigns`, `act/insights`, `act/adimages`. Object IDs are nodes too:
  `metaads get 120330000000 --fields name,status`.
- stdout carries the API response exactly as Meta sent it. A failure is one line of JSON on stderr:
  `{"kind","error","status","details","request_id"}`. Exit codes: 0 success, 1 API or network
  error, 2 usage error or guardrail refusal (nothing was sent).
- `metaads commands --json` lists every command and the guard's rules.

## Reading

```
metaads get act --fields name,currency,account_status,spend_cap,amount_spent
metaads get act/campaigns --fields id,name,status,effective_status,daily_budget,lifetime_budget --all
metaads get act/adsets --fields id,name,status,daily_budget,lifetime_budget,end_time,targeting --all
metaads get act/ads --fields id,name,status,effective_status,creative --all
metaads get act/insights --param level=ad --param date_preset=last_7d --fields ad_id,ad_name,spend,impressions,clicks,actions --all
metaads get act/insights --param level=campaign --param 'time_range={"since":"2026-09-01","until":"2026-09-24"}' --param time_increment=1 --fields campaign_name,spend,clicks
```

- `--fields` selects fields; `--param key=value` adds query parameters. Write JSON-valued ones
  (`time_range`, `filtering`, `breakdowns`) as JSON.
- `--all` follows the cursor and prints one JSON array of every `data` entry.
- Drafts in Ads Manager are invisible to the API until a person publishes them. An empty
  `act/campaigns` while someone sees a draft in Ads Manager is expected, not an error.
- Insights `spend` is a decimal string in the account currency (major units). Budgets are whole
  numbers in the minor unit.
- A large insights query can run as a report: `metaads post act/insights --data '{"level":"ad","date_preset":"last_30d","fields":"ad_name,spend"}'`
  returns `report_run_id`; poll `metaads get <report_run_id> --fields async_status,async_percent_completion`
  until `async_status` is `Job Completed`, then `metaads get <report_run_id>/insights --all`.

## Writing

- `--data` is one JSON object; metaads sends it as form fields, the encoding Meta documents (nested
  values become JSON strings). Field names are Meta's, in snake_case: top-level names, like every
  `--param` key and `--file` field, may hold only lowercase letters, digits and underscores, so
  write nested values as JSON, never as `targeting[geo_locations]`. `post` takes no `--param`.
- `delete` takes form fields as `--param key=value`, such as `hash=<image hash>` on `act/adimages`.
  They get the same checks as `--data` fields, and a key given twice is refused.
- Money: budgets are whole numbers in the account currency's minor unit. For CHF, `3000` is CHF
  30.00. Never write decimals.
- Create everything with `"status": "PAUSED"`; a create without it needs `--force`. On an existing
  object, `PAUSED` and `ARCHIVED` need no `--force`; any other status needs it, `DELETED` included.
- Check a create first with `--validate-only` (campaigns, ad sets, ads and ad creatives on `act`):
  Meta validates every field and creates nothing.
- Never pass `--force`, and never change a budget cap, without a person's go-ahead in this
  conversation. `--force` is required to start delivery, change an existing budget, delete, and
  call anything outside campaign editing.
- Budgets above the configured caps are refused whatever the flags, and so is a budget without a
  matching cap. Each cap belongs to one ad account: on another account, one named by
  `META_ADS_AD_ACCOUNT_ID` included, every budget is refused until a person sets caps for it. The
  account spending limit cannot be changed through metaads.
- Refused outright: budget schedules and automated rules (creating or editing them), reserved
  buying, bulk deletes, bulk ad creation (`asyncadrequestsets`), batch requests, and the parameters
  metaads owns (`method`, `access_token`, `execution_options` and the others listed in
  `commands --json`).
- Write edges as Meta spells them, in lowercase: `post` and `delete` refuse `act/Campaigns`.

## Worked example: a paused campaign with one image ad

1. Upload the image; the response carries its hash.

   ```
   metaads post act/adimages --file filename=@motiv.png
   ```

2. The campaign, paused, no special ad category, budgets on the ad set. Validate first, then send
   the same without `--validate-only`; the response carries the campaign ID.

   ```
   metaads post act/campaigns --validate-only --data '{"name":"Office work solved","objective":"OUTCOME_TRAFFIC","status":"PAUSED","special_ad_categories":[],"is_adset_budget_sharing_enabled":false}'
   ```

3. The ad set: a lifetime budget of CHF 300 over 14 days, paused.

   ```
   metaads post act/adsets --data '{"name":"Switzerland","campaign_id":"<campaign id>","status":"PAUSED","lifetime_budget":30000,"start_time":"2026-10-01T08:00:00+0200","end_time":"2026-10-15T08:00:00+0200","billing_event":"IMPRESSIONS","optimization_goal":"LINK_CLICKS","bid_strategy":"LOWEST_COST_WITHOUT_CAP","targeting":{"geo_locations":{"countries":["CH"]},"age_min":25}}'
   ```

4. The creative, with the image hash and the Page.

   ```
   metaads post act/adcreatives --data '{"name":"Motif 1","object_story_spec":{"page_id":"<page id>","link_data":{"image_hash":"<hash>","link":"https://example.com/check","message":"<text>","call_to_action":{"type":"LEARN_MORE"}}}}'
   ```

5. The ad, paused.

   ```
   metaads post act/ads --data '{"name":"Motif 1","adset_id":"<ad set id>","creative":{"creative_id":"<creative id>"},"status":"PAUSED"}'
   ```

6. Starting delivery is a person's decision. With their go-ahead, set `"status":"ACTIVE"` with
   `--force` on the campaign, the ad set and the ad: `metaads post <id> --force --data '{"status":"ACTIVE"}'`.

## Errors

- `usage`: refused locally, nothing was sent; the message says why.
- `validation`: Meta rejected a field; the message names Meta's code, its text and the blamed
  fields.
- `auth`: the token is invalid or expired. `forbidden`: the system user lacks a permission or the
  assignment. `not_found`: the object does not exist or is not visible.
- `rate_limited`: metaads already waited as long as Meta asked, within 60 seconds; try later.
- `server`, `transport`: Meta or the network failed; metaads already retried reads.
- `outcome_unknown`: a write may or may not have landed; read the object before retrying.
- `incomplete`: `--all` hit `--max-pages`; the output is partial.
- `output_failed`: the call succeeded, but its response could not be written where it was asked to
  go (a failed `--output` file puts it on stdout instead); do not repeat a change.
- The message may end with `hint: ...`, naming the next step for the person.
- Do not add a retry loop of your own.

## Notes for the person wiring this up

- Give agents that only report a read-only posture: `META_ADS_READ_ONLY=1` in their environment.
  `--validate-only` still works there.
- `--force` has no environment variable or config setting on purpose: every spend decision stays a
  visible flag in the transcript.
- Set the account spending limit and both caps (`metaads config set daily-budget-cap <amount>`,
  `metaads config set lifetime-budget-cap <amount>`, after `metaads config set ad-account-id <id>`
  and `metaads config set currency <code>`) before any agent writes. Each cap belongs to that ad
  account; for another one, budgets are refused until you set caps for it.
- The token and the app secret come from `META_ADS_ACCESS_TOKEN` and `META_ADS_APP_SECRET`, or on
  stdin: `printf '%s' "$VALUE" | metaads config set access-token`. A value on the command line is
  refused.
