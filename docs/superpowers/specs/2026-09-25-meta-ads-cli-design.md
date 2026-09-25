# Meta Ads CLI: Design

**Date:** 2026-09-25
**Status:** Draft for review (design approved in conversation on 2026-09-25)

## Goal

A single-binary CLI, written in Go, that lets AI agents and humans read and manage a Meta ad account
(Facebook and Instagram) through the Graph API and the Marketing API from the shell: read campaigns,
ad sets, ads and insights, upload images, and build and edit campaigns. The two decisions that cost
money stay with a person: switching delivery on, and how much a budget may spend. Runs on Linux, macOS
and Windows with no runtime dependencies. Public open-source project under the MIT license.

- Binary: `metaads`.
- Module: `github.com/wir-drei-digital/meta-ads-cli`.
- Not affiliated with or endorsed by Meta; the README says so.

It is a sibling of `google-ads-cli`, `bexio-cli`, `klara-cli`, `cash-ctrl-cli` and `grav-cms-cli` and
keeps their stack and contracts on purpose, so an agent that knows one of them knows this one.

## Why not Meta's own Ads CLI

Meta published an Ads CLI on 2026-04-29 (`pip install meta-ads`, command `meta ads <resource>
<action>`, version 1.1.0 of 2026-06-17, classified Alpha on PyPI) together with a hosted Ads MCP
server. It was evaluated first and not adopted:

- **No guardrail on spend.** `meta ads campaign update <id> --status ACTIVE --daily-budget 50000`
  runs without any gate. Its `--force` only skips the confirmation prompt of a delete. Objects are
  created `PAUSED` by default, but `--status ACTIVE` on create or update is a plain flag.
- **Curated coverage without an escape hatch.** Campaigns, ad sets, ads, creatives, insights,
  catalogs, datasets, ad accounts and pages. No custom audiences, automated rules, lead forms or
  raw call.
- **Runtime and license.** Python 3.12 or newer on every host; proprietary license
  (`LicenseRef-Proprietary`), so it cannot be forked or adapted.
- **A different contract** from our other CLIs: table output by default, its own exit codes 0 to 5.

The hosted MCP server needs a per-runner OAuth setup, which is what our CLIs exist to avoid.

A generated command tree, as in `google-ads-cli`, was also considered and rejected. The only complete
machine-readable description of the API is the spec inside `facebook/facebook-business-sdk-codegen`
(v26.0, refreshed about monthly). It carries no descriptions, types a tenth of all parameters as
`Object` or `map`, has wrong return types in 159 entries and two dialects, and 310 to 420 ad-related
operations would each need a reviewed risk class, reviewed again with every Marketing API version
(about every four months). The Graph API has one shape instead: every call is `GET`, `POST` or
`DELETE` on `/<node>` or `/<node>/<edge>`. A generic client over that shape covers the whole API,
matches Meta's documentation one to one, and lets the guard key on what a request sets rather than
on which of hundreds of methods it calls.

## What was verified before designing this

Checked on 2026-09-25 against Meta's documentation (fetched as Markdown from
`developers.facebook.com/documentation/ads-commerce/...`; the pages carry no "last updated" stamp),
the codegen repository at `8420be6` (2026-09-17), PyPI, and a few unauthenticated calls to
`graph.facebook.com`. Nothing below was tested with credentials yet; see *Open questions*.

| Question | Result |
| --- | --- |
| Current API version | v26.0, released 2026-07-29. Marketing API versions live about 12 months (v23: 2025-05-29 to 2026-06-09); v24.0 expires on 2026-10-06. v26.0 breaking changes apply to all versions from 2026-10-27. Calls to an expired version on unchanged endpoints are upgraded, and the response header `facebook-api-version` names the version that answered |
| Credential for unattended use | A system user access token from the Business Portfolio. Tokens never expire unless created with `set_token_expires_in_60_days=true`; Meta recommends the 60-day kind and forces it on some businesses. Renewal: `GET /oauth/access_token?grant_type=fb_exchange_token` with app ID and secret |
| appsecret_proof | Hex HMAC-SHA256 of the access token, keyed with the app secret. Mandatory on every call when the app has "Require App Secret" switched on |
| Account tasks | A system user is assigned to an ad account with tasks `MANAGE` (campaigns, billing, permissions), `ADVERTISE` (create ads, use the funding source) or `ANALYZE` (reporting) |
| Access tiers | Renamed on 2026-05-04: "Marketing API Access Tier", Limited (default, heavily rate-limited per ad account) and Full (App Review, 500 calls in 15 days, error rate under 15%). Managing only your own ad account needs standard access to `ads_read` and `ads_management`, no permission review |
| Writes | Form-encoded fields; nested values as JSON strings. Create on `POST /act_<id>/<edge>`, update on `POST /<id>`, delete with `DELETE /<id>` (same as `status=DELETED`, terminal) |
| Budgets | `daily_budget` and `lifetime_budget` in the account currency's minor unit, using Meta's offset table (offset 100 for CHF, EUR, USD and most others; offset 1 for CLP, COP, CRC, HUF, ISK, IDR, JPY, KRW, PYG, TWD, VND). A budget sits on the campaign or on its ad sets, never both (error 1885621). Campaign updates can set child budgets through `adset_budgets` |
| Overspend | Up to 175% of the daily budget on a single day, at most 7 times the daily budget per Sunday to Saturday week (210% and 8.4 times with ad set budget sharing). A lifetime budget is never exceeded unless delivery settings change |
| Spend limits | Account `spend_cap`: minor units on read, `0` means none; a float in major units on write; `spend_cap_action=reset` zeroes the amount spent, `delete` removes the limit; at most 10 changes per day. Campaign `spend_cap`: minor units, `922337203685478` removes it |
| Budget schedules | `POST /<campaign or ad set>/budget_schedules` and `budget_schedule_specs` raise a daily budget for high-demand periods, up to 8 times |
| Default status on create | Not documented for campaigns, ad sets or ads. Only `ACTIVE` and `PAUSED` are valid on create. Delivery needs all three levels `ACTIVE`, a reviewed ad, an active account and a funding source |
| Required declarations | `special_ad_categories` on every campaign create (`[]` or `NONE` when none applies). Since v24 `is_adset_budget_sharing_enabled` when budgets sit on ad sets. Ad sets targeting the EU need `dsa_beneficiary` and `dsa_payor` or account defaults. Political, electoral and social issue ads stopped in the EU on 2025-10-06 |
| Dry run | `execution_options=["validate_only"]` on create and update of campaigns, ad sets and ads, and on ad creatives: validates every field, applies nothing, returns `{"success": true}` or an error |
| Paging | `{"data": [...], "paging": {"cursors": {"before", "after"}, "next"}}`; stop when `next` is absent |
| Batch | `POST /` with `batch` (up to 50 sub-requests, bodies URL-encoded) and `POST /act_<id>/async_batch_requests` (up to 1000) |
| Rate limits | Business use case quotas per ad account and hour (Limited: `ads_management` 300 + 40 per active ad). Headers `X-Business-Use-Case-Usage` (with `estimated_time_to_regain_access` in minutes) and `X-Ad-Account-Usage` (`reset_time_duration` in seconds) |
| Errors | `{"error": {"message", "type", "code", "error_subcode", "error_user_title", "error_user_msg", "fbtrace_id", "is_transient", "error_data"}}`, almost always with HTTP 400; branch on codes, never on text |
| Hosts | `graph.facebook.com` for everything this design uses |
| Go client | No official Go SDK |

Sources: [versions](https://developers.facebook.com/docs/graph-api/changelog/),
[Marketing API versioning](https://developers.facebook.com/documentation/ads-commerce/marketing-api/overview/versioning),
[system users](https://developers.facebook.com/docs/business-management-apis/system-users/install-apps-and-generate-tokens/),
[securing requests](https://developers.facebook.com/docs/graph-api/securing-requests/),
[ad account users](https://developers.facebook.com/docs/marketing-api/reference/ad-account/assigned_users/),
[authorization](https://developers.facebook.com/documentation/ads-commerce/marketing-api/get-started/authorization),
[campaign](https://developers.facebook.com/documentation/ads-commerce/marketing-api/reference/ad-campaign-group),
[ad set](https://developers.facebook.com/documentation/ads-commerce/marketing-api/reference/ad-campaign),
[ad account](https://developers.facebook.com/documentation/ads-commerce/marketing-api/reference/ad-account),
[budgets](https://developers.facebook.com/documentation/ads-commerce/marketing-api/bidding/overview/budgets),
[currencies](https://developers.facebook.com/documentation/ads-commerce/marketing-api/currencies),
[high-demand periods](https://developers.facebook.com/documentation/ads-commerce/marketing-api/reference/high-demand-period),
[overspend](https://www.facebook.com/business/help/190490051321426),
[special ad categories](https://developers.facebook.com/documentation/ads-commerce/marketing-api/audiences/special-ad-category),
[object status](https://developers.facebook.com/documentation/ads-commerce/marketing-api/best-practices/manage-your-ad-object-status),
[batch requests](https://developers.facebook.com/docs/graph-api/batch-requests/),
[rate limiting](https://developers.facebook.com/documentation/ads-commerce/marketing-api/overview/rate-limiting),
[error reference](https://developers.facebook.com/documentation/ads-commerce/marketing-api/error-reference),
[Ads CLI](https://developers.facebook.com/documentation/ads-commerce/ads-ai-connectors/ads-cli/ads-cli-overview),
[meta-ads on PyPI](https://pypi.org/project/meta-ads/),
[codegen spec](https://github.com/facebook/facebook-business-sdk-codegen).

## Requirements

- **Full reach, generic.** Every Graph API node and edge the token can reach is callable through
  `get`, `post` and `delete`, with Meta's own paths and field names.
- **System user token only.** No browser login.
- **Agent-first UX.** JSON in, JSON out, one line of JSON per failure, stable exit codes,
  self-describing help and a machine-readable catalog.
- **Spend stays with a person.** Field-aware risk classes, `--force`, a daily and a lifetime budget
  cap, server-side validation without effect, a read-only mode, and the account spending limit as
  the ceiling outside the CLI.
- **Public OSS.** README, SECURITY.md, `docs/agents.md`, CI, versioned multi-platform releases.

## What is different from google-ads-cli

| Concern | google-ads-cli | meta-ads-cli |
| --- | --- | --- |
| Command source | generated from the Discovery document | three generic verbs over Graph paths; no generator, no vendored spec |
| Credential | service-account key, exchanged for one-hour tokens | system user access token, used directly; optional app secret for `appsecret_proof` |
| Account selection | `--customer-id` per call | one configured ad account; the path segment `act` stands for it |
| Request encoding | JSON bodies | `--data` JSON, sent as form fields (nested values as JSON strings) or multipart with `--file` |
| Risk | per method from a reviewed table, plus a mutate payload check | per verb and edge from a small reviewed table, plus a check of every field that moves money, including nested specs |
| Budget cap | one daily cap in micros | a daily and a lifetime cap, in the account currency's minor unit |
| Dry run | `--validate-only` on 91 methods | `--validate-only` on the four create edges where Meta documents it |
| Paging | page tokens | `after` cursors |
| API version | pinned `v25`, spec refresh per version | default `v26.0` as a constant; a path may carry its own version |
| Error mapping | by HTTP status, Google failure details | by Meta error code, since Meta answers almost everything with HTTP 400 |
| Dropped | | `schema`, `api`, the generator, `auth token`, `partial_failure` and `conflict` kinds |

Everything else is carried over deliberately: Go and cobra, a single static binary
(`CGO_ENABLED=0`), stdout reserved for the response, one line of JSON on stderr per failure, exit
codes 0/1/2, `commands --json`, `--data` from an argument, a file or stdin, `--output`, `--all` with
`--max-pages`, refusal to follow redirects, a pinned host with an explicit opt-in for others, secrets
never from argv, the config file conventions and goreleaser releases.

## Architecture

```
meta-ads-cli/
├── cmd/metaads/            # main: hands argv to internal/cli
├── internal/cli/           # command tree: get, post, delete, auth, config, init, version, commands
├── internal/route/         # path parsing: version prefix, act alias, node and edge
├── internal/api/           # HTTP client: host pinning, credentials, form and multipart encoding,
│                           #   retries, rate-limit headers, errors, cursor paging
├── internal/auth/          # token sources, appsecret_proof, debug_token, token refresh
├── internal/guard/         # request classification, field checks, caps, refusals, rule table
├── internal/config/        # config file and environment, account ID, money with Meta's offsets
├── e2e/                    # smoke test against a fake Graph server; manual live test
└── docs/agents.md
```

Code is copied from `google-ads-cli` and adapted where the behaviour is the same: the API client
skeleton, the retry loop, error output, the strict JSON reader (duplicate keys), config file handling,
money parsing, and the CLI scaffolding for `--data`, `--all`, `--output`, `commands` and `config`.
Multipart handling comes from `bexio-cli`. The sibling CLIs share no module. `internal/route`, the
field-aware guard, the form encoding and the token handling are new.

## CLI contract

### Commands

| Command | Purpose |
| --- | --- |
| `metaads get <path> [--fields a,b] [--param k=v]... [--all] [--max-pages N]` | read any node or edge, including insights |
| `metaads post <path> [--data <json>] [--file field=@path]... [--validate-only] [--force]` | create and change |
| `metaads delete <path> [--param k=v]... [--force]` | delete |
| `metaads auth status [--check]` | credential state as one line of JSON |
| `metaads auth refresh` | renew a 60-day token and store it |
| `metaads config set/unset/path` | configuration |
| `metaads init` | interactive setup |
| `metaads commands --json` | the catalog |
| `metaads version` | CLI version and default API version |

Common flags: `--output <file>` writes the response body to a file, `--timeout` sets the per-attempt
deadline. Shell completion comes from cobra.

### Paths

- Grammar: `[vNN.N/]<node>[/<edge>]`, relative to `https://graph.facebook.com/`. A leading `/` is
  accepted and dropped. A leading version (`v27.0/act_123/campaigns`) is used as given; otherwise
  the default version is prefixed.
- The node `act` stands for the configured ad account (`act_<id>`); `act_<digits>` is accepted as
  well. A missing configured account makes `act` a usage error.
- A path may not contain `?`, `#`, `%`, `..` or empty segments. Query and body fields come only from
  flags, so the guard sees everything that is sent.
- For `post` and `delete` the node must be `act`, `act_<digits>` or an all-digit object ID, followed
  by at most one edge. `act_<digits>` must be the configured account: a write to another ad account
  is a usage error. Other shapes (`me/...`, named nodes, deeper paths) are usage errors for these
  verbs. `get` accepts any path of this grammar.

### Requests

- **`get`:** `--fields a,b` becomes `fields=a,b`; `--param k=v` (repeatable) adds query parameters,
  nested values written as JSON (`--param 'time_range={"since":"2026-09-01","until":"2026-09-24"}'`).
- **`post`:** `--data '{...}'`, `--data @file.json` or `--data -`, capped at 20 MB, must be one JSON
  object. Each top-level key becomes one form field: strings as they are, numbers, booleans and
  `null` as their JSON text, arrays and objects as compact JSON. Without `--file` the body is
  `application/x-www-form-urlencoded`. With `--file field=@path` (repeatable) the request is
  `multipart/form-data`: the `--data` fields as text parts, each file as a file part named `field`.
  `post` takes no `--param`.
- **`delete`:** `--param k=v` becomes form fields of the DELETE request.
- A `--data` file with a UTF-8 byte order mark is accepted with the mark stripped; a UTF-16 file is a
  usage error saying to save it as UTF-8, as in `google-ads-cli`.

### Output

- The API response, untouched, on stdout. `--output <file>` writes it to a file instead.
- **`--all`** (only on `get`): the response must carry a `data` array. While `paging.next` is
  present, the CLI repeats the request with the original parameters and `after` set to
  `paging.cursors.after`. It never follows the `next` URL itself, which keeps the host pinned and
  credentials out of URLs. It prints one JSON array of the merged `data` entries, the only
  transformation the CLI performs. `--max-pages` (default 100) caps it; hitting the cap with data
  remaining prints the partial array and exits 1 with kind `incomplete`.

### Catalog

`metaads commands --json` prints `schema_version` 1, `api_version`, and:

- every command with its flags and positional arguments;
- the edge table with each edge's class (see *Guardrails*);
- the refused edges, fields and requests, each with its reason;
- the parameters the CLI owns;
- the edges where `--validate-only` is accepted.

## Authentication

- **Credential:** a system user access token. The first source found wins:
  1. `META_ADS_ACCESS_TOKEN`;
  2. the token stored in the config file by `metaads config set access-token`, which reads it from
     stdin only.
- **App secret (optional, recommended):** from `META_ADS_APP_SECRET` or `config set app-secret`
  (stdin only). When present, every request carries `appsecret_proof`, the hex HMAC-SHA256 of the
  token keyed with the secret. Together with "Require App Secret" in the app settings, a leaked token
  alone cannot call the API.
- **App ID:** `META_ADS_APP_ID` or `config set app-id <id>` (an identifier, allowed on argv). Needed
  for `auth status --check` and `auth refresh`.
- **Transport:** the token goes in `Authorization: Bearer <token>`, never into a URL. If the live
  check shows that Meta refuses the header for some call, the fallback is the `access_token` form
  field for POST and DELETE; see *Open questions*. Error messages and debug output print paths
  without query strings.
- **`metaads auth status`:** offline, one line of JSON: `mode` (`token` or `none`), `source` (`env`
  or `config`), `app_id`, `app_secret` (`set` or `missing`), `ad_account_id`, `currency`,
  `daily_budget_cap`, `lifetime_budget_cap`, `read_only`, `token_expires_at` as last recorded by
  `init`, `--check` or `refresh` (`never` for a non-expiring token), and `missing` plus `hint` when
  the configuration cannot make a call. Never a token or a secret.
- **`metaads auth status --check`:** with app ID and secret, calls `GET /debug_token` with the app
  token `<app_id>|<app_secret>` and adds `is_valid`, `expires_at`, `scopes` and the ad accounts in
  `granular_scopes`; it records the expiry in the config file. Without them it calls
  `GET /me?fields=id,name` and reports validity only.
- **`metaads auth refresh`:** needs app ID and secret and a token stored in the config file. Calls
  `GET /oauth/access_token` with `grant_type=fb_exchange_token`, `client_id`, `client_secret`,
  `fb_exchange_token` and `set_token_expires_in_60_days=true`, stores the new token and its expiry,
  and prints `{"expires_at": ...}`. With a token from the environment it refuses and says to
  renew the environment's source instead. A cron job can run it weekly.
- **Revocation** is removing the token or the system user in the Business Portfolio. `metaads config
  unset access-token` removes the local copy only, and says so.

## Configuration

- File: `<user config dir>/metaads/config.json`, `0600` inside a `0700` directory, written through a
  temp file and rename. `metaads config path` prints the location.
- Keys for `config set` and `config unset`: `access-token` (stdin only), `app-secret` (stdin only),
  `app-id`, `ad-account-id` (`act_123` or `123`, stored as digits), `currency`,
  `daily-budget-cap`, `lifetime-budget-cap`, `read-only` (`true` or `false`). Passing an environment
  variable name or an unknown key is an error that names the valid keys.
- Environment variables, which win over the file: `META_ADS_ACCESS_TOKEN`, `META_ADS_APP_SECRET`,
  `META_ADS_APP_ID`, `META_ADS_AD_ACCOUNT_ID`, `META_ADS_READ_ONLY`, `META_ADS_API_BASE`,
  `META_ADS_ALLOW_CUSTOM_BASE`.
- **The caps and the currency have no environment variable and no flag.** They live in the config
  file only.
- The file also holds the recorded token expiry (`token_expires_at`), written by `init`,
  `auth status --check` and `auth refresh`; it is not a `config set` key.
- **Money.** `currency` is an ISO code from Meta's currency table, which the CLI embeds with each
  currency's offset. A cap is entered in major units with a dot and at most as many decimals as the
  offset allows (`30`, `29.50` for CHF; `3000` for JPY) and stored as an integer in the minor unit
  together with the currency it was entered for. A comma (`29,50`) is refused with a message to use a
  dot, never read as 2950 or 29. `config set` of a cap needs a configured currency. When the
  configured currency differs from a cap's currency, every request that sets that budget is refused
  until the cap is set again.
- **`metaads init`** needs a terminal and refuses without one. It asks for the token (hidden input),
  the app ID and the app secret (hidden; may be skipped with a warning). It verifies the token with
  `debug_token` when it has the secret, otherwise with `me`; lists the ad accounts the token reaches
  (`me/adaccounts?fields=account_id,name,currency,account_status`) and lets the user pick one; then
  shows the account's name, currency, status, spending limit (`spend_cap`) and amount spent, with a
  warning when no spending limit is set. It stores the currency and asks for the daily and the
  lifetime cap in that currency. When verification fails it prints the error with its hint and saves
  nothing; the non-interactive `config set` path stays available.

## Guardrails

### Risk classes

| Class | Meaning | Gate |
| --- | --- | --- |
| `read` | reads or computes, changes nothing | none; allowed in read-only mode |
| `write` | creates or edits campaign objects without starting delivery or moving budget | blocked in read-only mode |
| `delete` | removes something | `--force` |
| `spend` | can start delivery or change how much is spent | `--force` |
| `admin` | outside campaign editing, or not known to the CLI | `--force` |

`--force` is a per-call flag with no environment or config equivalent. Without it, a `delete`,
`spend` or `admin` request exits 2 with kind `usage` before any network I/O. The message names every
finding and why, for example `adset_spec.status "ACTIVE": starting delivery needs --force` or
`daily_budget 5000 exceeds daily-budget-cap 3000 (CHF 30.00)`. When a request has several findings,
the highest class wins, with `read < write < delete, spend, admin`. A refusal (below) wins over every
class and ignores `--force`.

### Class by verb and path

The edge table lives in `internal/guard/rules.go`, is reviewed like the classification table of
`google-ads-cli`, and is published through `commands --json`.

| Request | Class |
| --- | --- |
| any `GET` | `read` |
| `POST /<node>/insights` (asynchronous report run) | `read` |
| `POST` on `campaigns`, `adsets`, `ads`, `adcreatives`, `adimages`, `adlabels`, `copies` | `write`, then the field checks |
| `POST /<id>` (update of an object) | `write`, then the field checks |
| `POST /act_<id>` (account settings) | `admin`, then the field checks |
| any `DELETE` | `delete` |
| `POST` on any other edge (`assigned_users`, `customaudiences`, `users`, `events`, `advideos`, ...) | `admin` |

A `POST /<id>` cannot tell a campaign from an ad set, an ad, a creative or a page. The field checks
therefore key on field names, whatever the object; the system user's asset assignment bounds what an
ID can reach.

### Field checks

Applied to every `post` body and to every `delete` parameter. The checks look at the top level and
inside the nested specs Meta accepts: `campaign_spec` (ad set create), `adset_spec` (ad create) and
each entry of `adset_budgets` (campaign update). A nested spec given as a JSON string is parsed as
JSON; a string that does not parse refuses the request.

1. `status` or `configured_status` with any value other than exactly `"PAUSED"`, `"ARCHIVED"` or
   `"DELETED"` is `spend`. `"DELETED"` is `delete`. Lowercase and numeric values count as "other".
2. A create on `campaigns`, `adsets` or `ads`, and every nested `campaign_spec` or `adset_spec`,
   without `status` set to exactly `"PAUSED"` is `spend`, because Meta does not document the default.
3. `daily_budget` is compared with `daily-budget-cap`, `lifetime_budget` with `lifetime-budget-cap`.
   Without the matching cap, or above it, the request is refused. `--force` and `--validate-only` do
   not change that. The value must be a JSON integer or a string of ASCII digits, without sign,
   spaces, leading zeros, decimals or exponent (`3000`, `"3000"`); anything else refuses the request
   with a message to write a whole number in the minor unit (CHF 30 is `3000`).
4. A budget within its cap on a create (the create edges and the nested specs) is `write`, because
   the object is paused or gated by rule 1 or 2. A budget on `POST /<id>` or inside `adset_budgets`
   is `spend`, whether it raises or lowers the amount: the CLI does not look up the current value.
   Pausing is the way to stop spend without `--force`.
5. `spend_cap` on anything but the ad account is `spend`: `922337203685478` removes a campaign's
   limit.
6. `copies` with `status_option` other than exactly `"PAUSED"` is `spend`. Without `status_option`
   Meta's documented default is `PAUSED`.
7. Everything else stays with the class from the table: bids and bid strategies (`bid_amount`,
   `bid_strategy`, `bid_constraints`, `adset_bid_amounts`), targeting, creatives, pacing, and the
   spend limits and minimum spend targets of ad sets under a campaign budget. The budget bounds what
   they can cost, the same decision as in `google-ads-cli`.

How the body is read:

- The body must be valid JSON with no duplicate keys, parsed with the strict reader from
  `google-ads-cli`. The CLI does not send what it cannot inspect.
- Field names are matched exactly; the Graph API uses snake_case only.
- The body that is sent is exactly the one that was checked, encoded as described under *Requests*.

### Refused in v1

Refused before any network I/O, whatever the flags, with kind `usage` and the reason:

| Refused | Reason |
| --- | --- |
| `spend_cap` and `spend_cap_action` on the ad account | the account spending limit belongs to a person; `reset` frees headroom by zeroing the amount spent |
| the `budget_schedules` edge and the field `budget_schedule_specs` | high-demand periods raise a daily budget up to 8 times, outside the cap |
| the `adrules_library` edge | automated rules change budgets and status later, outside any check |
| `buying_type` other than `"AUCTION"` and the `reachfrequencypredictions` edge | reserved buying commits spend the cap cannot check |
| `delete_strategy` | bulk deletion of every campaign, ad set or ad matching a strategy |
| `POST /` with `batch`, and the `async_batch_requests` edge | batch requests; sequential calls do the job at the intended scale |

### Parameters the CLI owns

`method` (the Graph API accepts `?method=post` on a GET), `access_token`, `appsecret_proof`,
`appsecret_time`, `batch`, `include_headers`, `callback`, `suppress_http_code` and
`execution_options` are refused in `--data` and `--param`. `--fields` is the only way to set
`fields` on `get`; a `fields` key in `--param` is a usage error.

### Validate-only

`--validate-only` sets `execution_options=["validate_only"]`. It is accepted only on `post` to the
create edges `campaigns`, `adsets`, `ads` and `adcreatives` of the configured account, where Meta
documents it. There it skips the `--force` gate and is allowed in read-only mode; the caps and the
refusals still apply. On any other path it is a usage error: an endpoint that ignores
`execution_options` would otherwise apply the change.

### Read-only mode

`META_ADS_READ_ONLY=1` or `config set read-only true` allows `read`-class requests and
`--validate-only` requests only.

### What the CLI cannot do

It cannot stop a caller with shell access from passing `--force` or editing the caps in the config
file. It makes both explicit and visible. The ceiling that holds regardless is outside the CLI:

- the **account spending limit**, set by a person in the Business Portfolio's billing settings;
- the system user's **task on the ad account**, `ADVERTISE` rather than `MANAGE`, so its token cannot
  change billing, the spending limit or permissions (to be confirmed by the live check);
- the assets the system user is assigned: only the configured ad account and the Page its ads run
  under.

The README states Meta's overspend rule next to the caps: up to 175% of the daily budget on a single
day, at most 7 times the daily budget per week, and a lifetime budget that is never exceeded. A daily
cap limits each budget, not the account: two ad sets with their own budgets can together spend twice
the cap, and switching each of them on is a separate `--force` decision.

Integration into an operator's own workspace, such as agent rules and pointers to where the token
lives, happens in that workspace and is not part of this repository.

## Errors, exit codes, retries

- **Exit codes:** `0` success, `1` API or network error, `2` usage error (including every guardrail
  refusal).
- **Error line** on stderr: `{"kind","error","status","details","request_id"}`. `status` is the HTTP
  status when a response exists. `details` is Meta's `error` object. `request_id` is its
  `fbtrace_id`, or the `x-fb-trace-id` response header, and is omitted when absent.
- **Kinds:** `auth`, `forbidden`, `not_found`, `validation`, `rate_limited`, `server`, `transport`,
  `outcome_unknown`, `incomplete`, `usage`.
- **Mapping by Meta error code first**, because Meta answers almost every error with HTTP 400:

  | Meta code | Kind |
  | --- | --- |
  | 190 | `auth` |
  | 10, 200 to 299, 294, 368 | `forbidden` |
  | 100 with subcode 33 | `not_found` |
  | 100 otherwise, 2500 | `validation` |
  | 4, 17, 32, 341, 613, 80000 to 80014 | `rate_limited` |
  | 1, 2, or `is_transient: true` | `server` |

  Without a Meta error body the HTTP status decides: 401 `auth`, 403 `forbidden`, 404 `not_found`,
  429 `rate_limited`, other 4xx `validation`, 5xx `server`, 3xx `validation` (redirects are refused
  and the message names the `Location`).
- **Message:** `METHOD path: HTTP status: code/subcode: <error_user_title>: <error_user_msg>`,
  falling back to `message`, plus the fields from `error_data.blame_field_specs` when present.
- **Hints** appended to the message for errors expected during setup:
  - 190: the token is expired or invalid; run `metaads auth status --check`.
  - 200 to 299, 294, 10: the system user lacks `ads_management` or is not assigned to this ad
    account with a task that allows the call.
  - 100 with subcode 33: the object does not exist or is not visible to the system user.
  - 1885621: a budget is set on both the campaign and its ad sets.
  - 1885272, 2446307: the budget is below Meta's minimum; minimums are doubled for some countries,
    Switzerland among them.
  - a missing `special_ad_categories`, `is_adset_budget_sharing_enabled` or DSA field: which field to
    add and where.
  - `rate_limited` while the usage headers' `ads_api_access_tier` names the development or Limited
    tier: the app is on the Limited tier, which Meta rate-limits heavily per ad account.
- **Retries:**
  - `rate_limited` is retried for every request. The wait comes from `estimated_time_to_regain_access`
    in `X-Business-Use-Case-Usage` (minutes), else from `reset_time_duration` in `X-Ad-Account-Usage`
    (seconds), else exponential backoff with jitter, within a total budget of 60 seconds. When the
    wait exceeds the remaining budget the request fails at once with `rate_limited` and the wait in
    the message.
  - `transport` and `server` are retried only for `read`-class requests, which includes
    `POST .../insights`, up to three attempts.
  - A write that fails in flight is never replayed and ends as `outcome_unknown`.
- **Deadlines:** `--timeout` per attempt, default 60 seconds. Ctrl-C and SIGTERM cancel the request
  and any retry wait.

## Testing

- **Route:** version prefixes, the `act` alias with and without a configured account, forbidden
  characters and shapes per verb, a write to a foreign `act_` ID.
- **Guard**, table-driven: each rule and refusal; nested specs as objects and as JSON strings,
  including a string that does not parse; amounts exactly at the cap and one unit above; no cap; a
  cap in another currency; loose amount forms (`" 3000"`, `"+3000"`, `3e3`, `"3000.0"`, `"03000"`);
  lowercase status; several findings where the highest class wins; owned parameters in `--data` and
  `--param`, including `method`; `--validate-only` on allowed and forbidden paths; read-only mode.
- **Money:** Meta's offset table (CHF, JPY, HUF), dot versus comma, too many decimals.
- **Encoding:** form fields for every JSON value type, multipart with `--file`, the byte order mark
  and UTF-16 cases.
- **Auth:** `appsecret_proof` against a known HMAC; `debug_token` and refresh against an `httptest`
  server; refresh refused for an environment token; no token or secret in any output.
- **API client**, against a fake Graph server: the Bearer header, host pinning and the custom-base
  opt-in, error mapping by code with hints and `request_id`, retries driven by both usage headers,
  an exhausted retry budget, no replay of writes, refused redirects, `--all` with `after` cursors and
  `--max-pages`.
- **CLI:** `config set`, `unset` and `path` semantics, stdin-only secrets, `init` refusing without a
  terminal, `auth status` output, the `commands --json` schema.
- **End to end:** the built binary against the fake server.
- **Live, manual, not in CI** (`META_ADS_LIVE=1` with a configured token and account):
  1. `get me` and `get act --fields name,currency,account_status,spend_cap,amount_spent`;
  2. `get act/campaigns --all` and `get act/insights --param date_preset=last_7d`;
  3. `post act/campaigns --validate-only` with a paused campaign (`special_ad_categories: []`,
     `objective: OUTCOME_TRAFFIC`) returns `{"success": true}` and creates nothing;
  4. the same body with `status: ACTIVE` is refused locally without `--force`;
  5. with `META_ADS_LIVE_WRITE=1` as well: create that paused campaign, read it back, delete it with
     `--force`.

## CI and release

- GitHub Actions on Ubuntu, macOS and Windows: `go vet` and tests (`make check`).
- goreleaser on tag: darwin amd64/arm64, linux amd64/arm64, windows amd64, `checksums.txt`, GitHub
  Release. `go install github.com/wir-drei-digital/meta-ads-cli/cmd/metaads@latest` works too.
- SemVer. The public API is the command paths, flags, exit codes, the stderr error schema and the
  `commands --json` schema. Additive optional fields are allowed under the same `schema_version`.
- First release `v0.1.0` after the live checks pass.
- **API version policy:** the default version is a constant. Moving it is a review of Meta's
  changelog for fields the guard checks (new money fields, new nested specs, new edges that start
  delivery), then a minor release. Marketing API versions live about 12 months; an old release keeps
  working through Meta's auto-upgrade for unchanged endpoints, and a caller can put a newer version
  into the path. The README says so.

## Documentation

- **README:** install, operator setup, authentication, usage, guardrails with the overspend rule, the
  refused list, error contract, API version policy, and that the project is not affiliated with
  Meta.
- **SECURITY.md:** what a system user token is (non-expiring or 60 days, works from any machine that
  has it, revoked in the Business Portfolio), why the app secret and "Require App Secret", one
  system user per purpose with only the assets it needs, `ADVERTISE` rather than `MANAGE`, the
  account spending limit, and what the CLI never does (other hosts, redirects, logging credentials,
  secrets from argv, tokens in URLs).
- **docs/agents.md:** the paste-in agent reference: the catalog, paths and the `act` alias, `--data`
  and form encoding, minor units (CHF 30 is `3000`), insights with `fields`, `level`, `date_preset`
  and `time_range`, the asynchronous report run in three calls, `--validate-only` first, never
  `--force` or a cap change without a person's go-ahead, error kinds and hints, and one worked
  example: upload an image, create a paused campaign, a paused ad set with a lifetime budget and an
  end date, a creative and a paused ad.

## Operator setup (once)

1. In the Meta Business Portfolio that owns the ad account, set the account spending limit (for
   example CHF 1,000 for a first test) and make sure a payment method is on file.
2. Create a Meta app of type Business in that portfolio, add the Marketing API product, and switch on
   "Require App Secret" under App Settings, Advanced.
3. Create a regular system user (not an admin system user). Assign it the ad account with the task
   `ADVERTISE` only, the Facebook Page the ads run under with the permission to create ads, and the
   app.
4. Generate a token for the app with `ads_management`, `ads_read` and `pages_manage_ads`, choosing
   the 60-day expiry if the portfolio requires it.
5. On each machine, run `metaads init`, or `config set` for each key, including both caps.

## Out of scope for v1

- Video upload (resumable and chunked uploads, `graph-video.facebook.com`).
- Batch requests, automated rules, budget schedules and reserved buying (refused, see above).
- Tested support for custom audience uploads, Conversions API events and lead forms. They are
  reachable as `admin` with `--force` but not tested.
- A helper that starts and waits for an asynchronous insights report.
- Several ad accounts or currencies in one configuration, and profiles.
- Looking up an object's type before a write.
- Browser (OAuth) login for people, OS keychain storage, human-readable table output, an MCP server
  mode, a Homebrew tap, artifact signing.

## Open questions

These need credentials and are settled by the live checks:

- Does Meta accept the token as `Authorization: Bearer` on every call this design uses, including
  multipart uploads and `debug_token`? The fallback is the `access_token` form field for POST and
  DELETE.
- Does the `ADVERTISE` task really prevent changing the account spending limit, and does it allow
  everything the worked example needs (image upload, creative with a Page, ad)?
- Is the Limited access tier enough for one account with a handful of ads?
- Does Meta force this portfolio onto 60-day tokens, and does `fb_exchange_token` renew a system user
  token as documented?
- Which status does Meta assign when a create omits `status`? The CLI gates it either way.
