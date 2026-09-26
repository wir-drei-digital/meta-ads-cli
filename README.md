# metaads

A command-line client for the [Meta Marketing API](https://developers.facebook.com/docs/marketing-api)
(Facebook and Instagram ads), built for agents and scripts: JSON in, JSON out, one static binary, no
runtime dependencies. Every request is `get`, `post` or `delete` on a Graph API path, so the whole
API is reachable with Meta's own paths and field names, and Meta's documentation applies one to one.
stdout carries the API response and nothing else; every failure leaves one line of JSON on stderr.

It keeps the two decisions that cost money with a person: starting delivery, and how much a budget
may spend. Both need an explicit flag or a setting only a person changes; see
[Guardrails](#guardrails).

metaads is not affiliated with or endorsed by Meta.

## Why not Meta's Ads CLI

Meta's own Ads CLI (`meta ads`, Python, proprietary license, classified alpha) covers a curated set
of resources and actions and has no budget cap or spend gate: `--status ACTIVE` and a new daily
budget are plain flags. metaads reaches every endpoint the token can reach, and refuses spend it was
not told to allow.

## Install

```sh
go install github.com/wir-drei-digital/meta-ads-cli/cmd/metaads@latest
```

Or download a prebuilt binary from
[GitHub Releases](https://github.com/wir-drei-digital/meta-ads-cli/releases):

| OS      | Architectures | Archive   |
| ------- | ------------- | --------- |
| Linux   | amd64, arm64  | `.tar.gz` |
| macOS   | amd64, arm64  | `.tar.gz` |
| Windows | amd64         | `.zip`    |

Each release also ships `checksums.txt`. Unpack the archive and put `metaads` on your `PATH`.

## Setup (once)

1. In the Meta Business Portfolio that owns the ad account, set the account spending limit (for
   example CHF 1,000 for a first test) and make sure a payment method is on file. The spending
   limit is the ceiling that holds even when `--force` is used.
2. Create a Meta app of type Business in that portfolio, add the Marketing API product, and switch
   on "Require App Secret" under App Settings, Advanced.
3. Create a regular system user (not an admin system user). Assign it the ad account with the task
   Advertise only, the Facebook Page the ads run under with the permission to create ads, and the
   app.
4. Generate a token for the app with `ads_management`, `ads_read` and `pages_manage_ads`. Choose the
   60-day expiry if the portfolio requires it.
5. On each machine, run `metaads init`. It asks for the token and the app secret (hidden input)
   and the app ID, checks them with read-only calls, lets you pick the ad account, shows its
   spending limit and asks for a daily and a lifetime budget cap in the account currency. Without a
   terminal, use the `config set` commands instead:

   ```sh
   printf '%s' "$TOKEN" | metaads config set access-token
   printf '%s' "$APP_SECRET" | metaads config set app-secret
   metaads config set app-id 1234567890
   metaads config set ad-account-id act_1234567890
   metaads config set currency CHF
   metaads config set daily-budget-cap 30
   metaads config set lifetime-budget-cap 300
   ```

## Authentication

metaads authenticates with a system user access token. The first source found wins:

1. `META_ADS_ACCESS_TOKEN`;
2. the token stored in the config file with `metaads config set access-token`, which reads it from
   stdin.

The app secret comes from `META_ADS_APP_SECRET` or `metaads config set app-secret` (stdin too). When
it is set, every request the token authenticates carries `appsecret_proof`, the hex HMAC-SHA256 of
the token keyed with the secret. With "Require App Secret" switched on in the app, Meta refuses calls
without it, so a leaked token alone cannot call the API. The app ID (`META_ADS_APP_ID` or
`metaads config set app-id <id>`) is needed for `auth status --check` with expiry and scopes, and
for `auth refresh`.

`config set access-token` and `config set app-secret` never take the value on the command line,
where every process on the machine can read it. Given one there, they refuse it without repeating
it.

The token travels in the `Authorization: Bearer` header, never in the URL of an API call. The two
calls about the credential itself take it as a query parameter, because Meta defines them that way:
`debug_token` (for `auth status --check` and `init`) and the renewal in `auth refresh`. `--verbose`
and error messages print paths without query strings.

`metaads auth status` prints the configuration as one line of JSON, offline, never a token or a
secret:

```json
{"mode":"token","source":"config","app_id":"1234567890","app_secret":"set","ad_account_id":"act_1234567890","currency":"CHF","daily_budget_cap":"30.00 CHF","lifetime_budget_cap":"300.00 CHF","read_only":false,"token_expires_at":"2026-11-24T08:00:00Z"}
```

| Key | Meaning |
| --- | --- |
| `mode` | `token`, or `none` when no token is configured |
| `source` | `env` (`META_ADS_ACCESS_TOKEN`) or `config` |
| `app_id` | the app ID, if one is set |
| `app_secret` | `set` or `missing` |
| `ad_account_id` | the configured ad account, as `act_<id>` |
| `currency` | the account currency the caps are entered in |
| `daily_budget_cap`, `lifetime_budget_cap` | the caps with their currency |
| `read_only` | whether read-only mode is on |
| `token_expires_at` | the expiry as last recorded by `init`, `auth status --check` or `auth refresh`; `never` for a token that does not expire |
| `is_valid`, `scopes`, `ad_accounts` | with `--check` only |
| `missing` | what blocks calls: `access_token`, `ad_account_id` |
| `hint` | the commands that fix what is missing or weak |

`metaads auth status --check` asks Meta. With the app ID and secret it calls `debug_token` with the
app token and adds `is_valid`, `scopes` and `ad_accounts` (the ad accounts the token reaches with
`ads_management` or `ads_read`), and updates `token_expires_at`, in the config file too when the
token comes from there. Without them it calls `me` and reports validity only.

`metaads auth refresh` renews a 60-day token. It needs the app ID, the app secret and a token stored
in the config file, calls `GET /oauth/access_token` with `grant_type=fb_exchange_token` and
`set_token_expires_in_60_days=true`, stores the new token and its expiry, and prints
`{"expires_at":"..."}`. A token from `META_ADS_ACCESS_TOKEN` is refused: renew it where that
variable is set. A weekly cron job is enough.

A token is revoked only in the Business Portfolio, under Users, System users: remove the token or
the system user. `metaads config unset access-token` removes the local copy and nothing else. See
[SECURITY.md](SECURITY.md).

### Configuration

The config file lives at `<user config dir>/metaads/config.json`, written `0600` inside a `0700`
directory; `metaads config path` prints the exact location. `metaads config set <key>` and
`metaads config unset <key>` take these keys:

| Key | Value |
| --- | --- |
| `access-token` | the system user access token, from stdin only |
| `app-secret` | the app secret, from stdin only |
| `app-id` | the Meta app ID, digits |
| `ad-account-id` | the ad account, `act_1234567890` or `1234567890` |
| `currency` | the ad account's currency, an ISO code from Meta's currency table (`CHF`) |
| `daily-budget-cap` | the highest daily budget metaads accepts, in the account currency (`30`, `29.50`) |
| `lifetime-budget-cap` | the highest lifetime budget metaads accepts, in the account currency (`300`) |
| `read-only` | `true` or `false` |

A cap is entered in major units with a dot as the decimal separator, with at most as many decimals
as the currency has (`29.50` for CHF, `3000` for JPY), and stored in Meta's minor unit together with
the currency it was entered for. A comma (`29,50`) is refused. Setting a cap needs a configured
currency; when the currency changes later, budgets are refused until the caps are set again in the
new currency. Passing an environment variable name as the key (`META_ADS_READ_ONLY`) gets an error
naming the key it meant. Unsetting a key that is not set is a no-op success.

Environment variables, which win over the file: `META_ADS_ACCESS_TOKEN`, `META_ADS_APP_SECRET`,
`META_ADS_APP_ID`, `META_ADS_AD_ACCOUNT_ID`, `META_ADS_READ_ONLY`, `META_ADS_API_BASE`,
`META_ADS_ALLOW_CUSTOM_BASE`. Two exceptions:

- The caps and the currency have no environment variable and no flag. They live in the config file
  only.
- `META_ADS_READ_ONLY` (`1` or `true`) only switches read-only mode on. It cannot switch off a
  read-only mode stored in the config file; to turn that off, run `metaads config unset read-only`.

## Usage

A path is `[vNN.N/]<node>[/<edge>]`, relative to `https://graph.facebook.com/`. The node `act`
stands for the configured ad account (`act_<id>`); object IDs are nodes too. A path carries no query
string: fields and parameters come only from flags, so the guard sees everything that is sent.

Read the account, its campaigns and last week's results per ad:

```sh
metaads get act --fields name,currency,account_status,spend_cap,amount_spent
metaads get act/campaigns --fields id,name,status,effective_status,daily_budget,lifetime_budget --all
metaads get act/insights --param level=ad --param date_preset=last_7d --fields ad_name,spend,impressions,clicks,actions --all
```

`--fields a,b` sets `fields`; `--param key=value` (repeatable) adds query parameters, with JSON
values written as JSON (`--param 'time_range={"since":"2026-09-01","until":"2026-09-24"}'`).

Upload an image; the response carries its hash:

```sh
metaads post act/adimages --file filename=@motiv.png
```

Create a campaign, paused. `--validate-only` makes Meta check every field and create nothing; send it
again without the flag to create it:

```sh
metaads post act/campaigns --validate-only --data '{"name":"Test","objective":"OUTCOME_TRAFFIC","status":"PAUSED","special_ad_categories":[],"is_adset_budget_sharing_enabled":false}'
metaads post act/campaigns --data '{"name":"Test","objective":"OUTCOME_TRAFFIC","status":"PAUSED","special_ad_categories":[],"is_adset_budget_sharing_enabled":false}'
```

Switch it on. That starts delivery, so it needs `--force`, and it needs the same on the ad set and
the ad:

```sh
metaads post <campaign id> --force --data '{"status":"ACTIVE"}'
metaads post <ad set id> --force --data '{"status":"ACTIVE"}'
metaads post <ad id> --force --data '{"status":"ACTIVE"}'
```

Delete something, and find your way around:

```sh
metaads delete <id> --force
metaads delete act/adimages --param hash=<image hash> --force
metaads commands --json       # every command and flag, and the guard's rule table
metaads post --help
```

Budgets are whole numbers in the account currency's minor unit: CHF 30 is `3000`. Insights `spend`
is a decimal string in major units (`"12.34"`).

Bodies come from `--data '{...}'`, `--data @file.json` or `--data -` (stdin), up to 20 MB, and must
be one JSON object without duplicate keys. A UTF-8 byte order mark is stripped; a UTF-16 file is
refused with a message to save it as UTF-8. Each top-level key becomes one form field, the encoding
Meta documents: strings as they are, numbers, booleans and `null` as their JSON text, arrays and
objects as compact JSON. `--file field=@path` (repeatable, up to 100 MB in total) sends
`multipart/form-data` instead, with the `--data` fields as text parts. `post` takes no `--param`.
`delete` takes `--param key=value` as form fields.

The response is Meta's JSON, untouched; `--output <file>` writes it to a file instead of stdout. The
file is opened before the request, so a path that cannot be written is a usage error and nothing is
sent; a failed request leaves an existing file as it was. If the write still fails after Meta
accepted the call, the response goes to stdout and the exit is 1 with kind `output_failed`.

`--all` (on `get`) repeats the request with `after` set to the next cursor, never following Meta's
`next` URL, and prints one JSON array of the merged `data` entries. It always starts at the first
page. `--max-pages` (default 100) caps it; hitting the cap with data left prints the partial array
and exits 1 with kind `incomplete`.

Global flags: `--timeout` (per attempt, default 60s) and `--verbose` (method, path, status and
response size on stderr; never a credential or a query string).

## Guardrails

### Risk classes

| Class | Meaning | Gate |
| --- | --- | --- |
| `read` | reads or computes, changes nothing | none; allowed in read-only mode |
| `write` | creates or edits campaign objects without starting delivery or moving budget | blocked in read-only mode |
| `delete` | removes something | `--force` |
| `spend` | can start delivery or change how much is spent | `--force` |
| `admin` | outside campaign editing, or not known to the CLI | `--force` |

Without `--force`, a `delete`, `spend` or `admin` request exits 2 with kind `usage` before any
network I/O. The message names every finding and why, for example
`status "ACTIVE": anything but "PAUSED" may start delivery`. When a request has several findings,
the highest class wins, with `read < write < delete, spend, admin`. A refusal (below) wins over every
class and ignores `--force`.

### Class by verb and path

| Request | Class |
| --- | --- |
| any `GET` | `read` |
| `POST /<node>/insights` (asynchronous report run) | `read` |
| `POST` on `campaigns`, `adsets`, `ads`, `adcreatives`, `adimages`, `adlabels`, `copies` | `write`, then the field checks |
| `POST /<id>` (update of an object) | `write`, then the field checks |
| `POST /act_<id>` (account settings) | `admin`, then the field checks |
| any `DELETE` | `delete`, then the field checks on its parameters |
| `POST` on any other edge (`customaudiences`, `assigned_users`, `advideos`, ...) | `admin` |

`post` and `delete` take `act`, `act_<id>` or an object ID as the node, followed by at most one
edge; an `act_<id>` other than the configured account is refused, so writes never reach an account
whose currency the caps know nothing about. `get` accepts any node. A `POST /<id>` cannot tell a
campaign from an ad set, an ad or a creative, so the field checks key on field names, whatever the
object.

### Field checks

Applied to every `post` body and to every `delete` parameter. The checks look at the top level and
inside the nested specs Meta accepts: `campaign_spec` (ad set create), `adset_spec` (ad create) and
each entry of `adset_budgets` (campaign update). A nested spec given as a JSON string is parsed as
JSON; a string that does not parse refuses the request.

1. `status` or `configured_status` with any value other than exactly `"PAUSED"`, `"ARCHIVED"` or
   `"DELETED"` is `spend`. `"DELETED"` is `delete`. Lowercase and numeric values count as "other".
2. A create on `campaigns`, `adsets` or `ads`, and every nested `campaign_spec` or `adset_spec`,
   without `status` set to exactly `"PAUSED"` is `spend`, because Meta does not document the
   default. That includes `"ARCHIVED"` and `"DELETED"` on a create.
3. `daily_budget` is compared with `daily-budget-cap`, `lifetime_budget` with
   `lifetime-budget-cap`. Without the matching cap, without a configured currency, with a cap
   entered in another currency, or above the cap, the request is refused. `--force` and
   `--validate-only` do not change that. The value must be a JSON integer or a string of ASCII
   digits, without sign, spaces, leading zeros, decimals or exponent (`3000`, `"3000"`); anything
   else refuses the request with a message to write a whole number in the minor unit.
4. A budget within its cap on a create (the create edges and the nested specs) is `write`, because
   the object is paused or gated by rule 1 or 2. A budget anywhere else, such as `POST /<id>` or an
   `adset_budgets` entry, is `spend`, whether it raises or lowers the amount: the CLI does not look
   up the current value. Pausing is the way to stop spend without `--force`.
5. `spend_cap` on anything but the ad account is `spend`: `922337203685478` removes a campaign's
   limit. On the ad account it is refused (below).
6. `copies` with `status_option` other than exactly `"PAUSED"` is `spend`. Without
   `status_option`, Meta's documented default is `PAUSED`.
7. Everything else stays with the class from the table: bids and bid strategies (`bid_amount`,
   `bid_strategy`, `bid_constraints`, `adset_bid_amounts`), targeting, creatives, pacing, and the
   spend limits and minimum spend targets of ad sets under a campaign budget. The budget bounds what
   they can cost.

How a request is read:

- The body must be valid JSON with no duplicate keys at any depth. The CLI does not send what it
  cannot inspect, and the body that is sent is exactly the one that was checked.
- Every top-level name that is sent, whether a `--data` field, a `--param` key or a `--file` field,
  must be lowercase letters, digits and underscores (`^[a-z0-9_]+$`), for every verb. Meta could
  read brackets or dots (`targeting[geo_locations]`) as nested fields the guard does not see. Keys
  inside nested JSON values are not held to this.
- A `--param` key given twice is refused: the guard and Meta could read different values. `post`
  takes no `--param` at all, because Meta merges query parameters into a POST body.
- A `--file` part may not be named like a field the guard checks (`status`, `daily_budget`,
  `campaign_spec` and the others listed as `file_fields_refused` in `commands --json`), nor like a
  `--data` field.

### Refused

Refused before any network I/O, whatever the flags, with kind `usage` and the reason. A refused
field is refused in a `post` body, in its nested specs and in `delete` parameters; a refused edge is
refused for `post`.

| Refused | Reason |
| --- | --- |
| `spend_cap` on the ad account, and `spend_cap_action` | the account spending limit belongs to a person; `reset` frees headroom by zeroing the amount spent |
| the `budget_schedules` edge and the field `budget_schedule_specs` | high-demand periods raise a daily budget up to 8 times, outside the cap |
| the `adrules_library` edge | automated rules change budgets and status later, outside any check |
| `buying_type` other than `"AUCTION"`, and the `reachfrequencypredictions` edge | reserved buying commits spend the cap cannot check |
| `delete_strategy` | bulk deletion of every campaign, ad set or ad matching a strategy |
| `batch` and the `async_batch_requests` edge | batch requests; sequential calls do the job at the intended scale |
| a name outside `^[a-z0-9_]+$`, and a `--param` key given twice | see *How a request is read* |

### Parameters the CLI owns

`method` (the Graph API accepts `?method=post` on a GET), `access_token`, `appsecret_proof`,
`appsecret_time`, `batch`, `include_headers`, `callback`, `suppress_http_code` and
`execution_options` are refused in `--data`, `--param` and `--file`. `--fields` is the only way to
set `fields` on `get`; a `fields` key in `--param` is a usage error.

### Validate-only

`--validate-only` sets `execution_options=["validate_only"]`: Meta validates every field and applies
nothing. It is accepted only on `post` to `act/campaigns`, `act/adsets`, `act/ads` and
`act/adcreatives`, where Meta documents it. There it skips the `--force` gate and is allowed in
read-only mode; the caps and the refusals still apply. On any other path it is a usage error: an
endpoint that ignores `execution_options` would otherwise apply the change.

### Read-only mode

`META_ADS_READ_ONLY=1` or `metaads config set read-only true` allows `read`-class requests and
`--validate-only` requests only.

### Force and the caps

- **`--force`** is a per-call flag. It has no environment variable and no config setting, so every
  spend decision stays visible in the command that made it.
- **The caps** are a daily cap (`daily-budget-cap`) and a lifetime cap (`lifetime-budget-cap`), set
  in the config file only: no environment variable, no flag. They apply to every budget in every
  request, with or without `--force` and `--validate-only`. Without a cap, no budget of that kind
  can be set at all.
- A cap limits each budget, not the account: two ad sets with their own budgets can together spend
  twice the daily cap. Switching each of them on is a separate `--force` decision.
- Meta may spend up to 175% of a daily budget on a single day, and at most 7 times the daily budget
  in a Sunday to Saturday week. A lifetime budget is never exceeded.

### What the CLI cannot do

It cannot stop a caller with shell access from passing `--force` or raising a cap in the config
file; it makes both explicit and visible. The ceiling that holds regardless is outside the CLI:

- the **account spending limit**, set by a person in the Business Portfolio's billing settings;
- the system user's **task on the ad account**: Advertise rather than Manage, the task Meta
  documents for billing, the spending limit and permissions;
- the assets the system user is assigned: only the configured ad account and the Page its ads run
  under.

## Errors and exit codes

Errors are one line of JSON on stderr, never on stdout (`details` shortened here):

```json
{"kind":"validation","error":"POST v26.0/act_1234567890/campaigns: HTTP 400: 100/1885621: Budget conflict: Set the budget on one level.; hint: a budget is set on both the campaign and its ad sets; keep it on one level","status":400,"details":{"code":100,"error_subcode":1885621,"type":"OAuthException"},"request_id":"AbC123xYz"}
```

`status` is the HTTP status and is omitted when there was no response. `details` is Meta's `error`
object, or `null`. `request_id` is Meta's `fbtrace_id`, else the `x-fb-trace-id` header, and is
omitted when absent. The message is `METHOD path: HTTP status: code/subcode:` followed by Meta's
`error_user_title` and `error_user_msg` (else its `message`) and the fields Meta blamed. Branch on
`kind`:

| `kind` | Meaning |
| --- | --- |
| `auth` | the token is invalid or expired |
| `forbidden` | the system user lacks a permission or the asset assignment |
| `not_found` | the object does not exist or is not visible to the system user |
| `validation` | Meta rejected the request or a field; also a redirect, which is refused rather than followed (the message names the `Location`) |
| `rate_limited` | a rate limit that outlasted the retry budget |
| `server` | Meta failed or called the error transient |
| `transport` | a network failure that is safe to retry: the request never reached Meta, or it could not change anything (a read or a `--validate-only` call); also a call cancelled while waiting to retry |
| `outcome_unknown` | a write failed in flight and may or may not have been applied; read the object before retrying |
| `incomplete` | `--all` hit `--max-pages`; the output holds the partial array |
| `output_failed` | the call succeeded, but the response could not be written to the `--output` file (it is on stdout instead) or to stdout; do not repeat a change |
| `usage` | the command was wrong or a guardrail refused it; nothing was sent |

Meta answers almost every error with HTTP 400, so the kind comes from Meta's error code first:

| Meta code | Kind |
| --- | --- |
| 190 | `auth` |
| 10, 200 to 299, 294, 368 | `forbidden` |
| 100 with subcode 33 | `not_found` |
| 100 otherwise, 2500 | `validation` |
| 4, 17, 32, 341, 613, 80000 to 80014 | `rate_limited` |
| 1, 2, or `is_transient: true` | `server` |

Without one of these codes the HTTP status decides: 401 `auth`, 403 `forbidden`, 404 and 410
`not_found`, 429 `rate_limited`, 5xx `server`, any other status `validation`.

Hints are appended to the message (`; hint: ...`) for the errors people hit during setup:

- 190: the token is expired or invalid; check it with `metaads auth status --check`, renew a 60-day
  token with `metaads auth refresh`.
- `forbidden`, except code 368: the system user lacks `ads_management`, or is not assigned to this ad
  account (or Page) with a task that allows the call.
- 100 with subcode 33: the object does not exist, or the system user cannot see it.
- 1885621: a budget is set on both the campaign and its ad sets.
- 1885272, 2446307: the budget is below Meta's minimum; minimums are doubled in some countries,
  Switzerland among them.
- A missing `special_ad_categories`, `is_adset_budget_sharing_enabled`, `dsa_beneficiary` or
  `dsa_payor`: which field to add.
- `rate_limited` while the usage headers name the Limited access tier: Meta rate-limits that tier
  heavily per ad account; the Full tier needs App Review.

| Exit code | Meaning |
| --- | --- |
| `0` | success |
| `1` | API or network error (any `kind` except `usage`) |
| `2` | usage error, which includes every guardrail refusal |

Retries happen inside the CLI; do not add a retry loop of your own. A rate limit is retried for
every request, waiting as long as Meta's usage headers ask (`estimated_time_to_regain_access` in
`X-Business-Use-Case-Usage`, else `reset_time_duration` in `X-Ad-Account-Usage`, else
`Retry-After`, else exponential backoff with jitter), within a total budget of 60 seconds. When Meta
asks for longer than the budget left, the call fails at once with `rate_limited` and the wait in the
message. Network failures and server errors are retried only for `read`-class and `--validate-only`
requests, which includes `POST .../insights`, up to three attempts. A write that fails in flight is
never replayed. Ctrl-C and SIGTERM cancel the request in flight and any retry wait.

## API version policy

metaads calls Graph API v26.0 by default (`metaads version` prints it). A path can carry its own
version (`metaads get v25.0/act/campaigns`) and is sent as given. Marketing API versions live about
12 months. Meta upgrades calls to an expired version on endpoints that did not change, so an older
release of metaads keeps working, and a caller can put a newer version into the path.

Moving the default version is a review of Meta's changelog for what the guard checks (new money
fields, new nested specs, new edges that start delivery), then a minor release.

## Development

```sh
make build   # go build -o bin/metaads ./cmd/metaads
make test    # go test ./...
make check   # go vet + tests; CI adds staticcheck and a CGO-free cross-compile
```

`e2e/smoke_test.go` builds the real binary and drives it against a fake Graph API. `e2e/live_test.go`
runs against a real ad account with your real configuration (token, ad account, currency and caps).
It reads the account, its campaigns and insights, sends a paused campaign with `--validate-only`, and
checks that the same campaign with `"status":"ACTIVE"` is refused locally. It changes nothing and is
skipped unless you run it with:

```sh
META_ADS_LIVE=1 go test ./e2e -run TestLive -v
```

With `META_ADS_LIVE_WRITE=1` as well, it also creates a paused campaign, reads it back and deletes
it with `--force`.

This project follows SemVer. The public API is the command paths, flags, exit codes, the stderr
error schema and the `metaads commands --json` schema (`schema_version`). New optional keys may
appear in the catalog under the same `schema_version`, so consumers must ignore keys they do not
know.

## License

MIT, see [LICENSE](LICENSE).
