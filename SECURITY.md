# Security

## What a system user token is

A system user access token belongs to a system user in a Meta Business Portfolio, not to a person.
It reaches exactly the assets the system user is assigned, with the tasks it holds there, and it
keeps working when people change passwords or leave. Depending on how it was generated it never
expires, or it expires after 60 days and can be renewed with `metaads auth refresh`. Anyone who has
the token can call the Marketing API as that system user, from any machine.

## Make the token useless on its own

Store the app secret too (`metaads config set app-secret`, read from stdin) and switch on "Require
App Secret" in the app's settings (App Settings, Advanced). metaads then sends `appsecret_proof`, an
HMAC of the token keyed with the secret, with every request the token authenticates, and Meta
refuses calls without it. A token that leaks into a log or a transcript is then not enough to call
the API.

## Give the system user as little as possible

- A regular system user, not an admin system user.
- Only the ad account metaads works on, with the task Advertise. Manage would also cover billing,
  the account spending limit and permissions.
- Only the Facebook Page the ads run under, with the permission to create ads.
- One system user per purpose, so revoking one does not break the others.

## The account spending limit

metaads refuses to change the ad account's spending limit, and its budget caps and `--force` gate
are only as strong as the shell they run in: someone with shell access can pass `--force` or edit
the config file. Each budget cap belongs to the ad account and the currency it was entered for, so
pointing metaads at another account, through `META_ADS_AD_ACCOUNT_ID` for example, does not carry
the caps over: budgets there are refused until a person sets caps for that account. The ceiling
that holds regardless is the account spending limit, set by a person in the Business Portfolio's
billing settings. Set one before any agent works on campaigns.

## Where the credentials live

- In the environment: `META_ADS_ACCESS_TOKEN` and `META_ADS_APP_SECRET`; or
- in the config file (`metaads config path`), written with mode 0600 inside a 0700 directory.

The token and the secret are never read from the command line, which other processes on the
machine can see. `metaads config set access-token` and `metaads config set app-secret` read stdin,
and refuse a value given on the command line without repeating it. `metaads auth status` never
prints either.

## What metaads never does

- Send the token to a host other than graph.facebook.com, unless `META_ADS_ALLOW_CUSTOM_BASE=1` is
  set for testing.
- Follow redirects.
- Put the token into the URL of an API call. It travels in the Authorization header. Only the two
  calls about the credential itself take it as a query parameter, because Meta defines them that
  way: `debug_token` (`auth status --check`, `init`) and the renewal in `auth refresh`, which also
  carries the app secret. Debug output and error messages print paths without query strings.
- Log credentials.

## Revoking access

Remove the system user's token, or the system user itself, in the Business Portfolio under Users,
System users. `metaads config unset access-token` removes the local copy only.

## Reporting a vulnerability

Please report security issues privately through GitHub's security advisories for this repository,
not in a public issue.
