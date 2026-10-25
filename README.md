# ChrisCoin

Money, by pull request. A silly currency whose ledger lives in Git.

**[Open ChrisCoin](https://chrislewis.au/ChrisCoin/index.html)** · [Public ledger](ledger/events.jsonl)

## How it works

1. Every GitHub account automatically starts with **100 ChrisCoin** on its first valid ledger entry. The **Get 100 CC** button is an optional way to open an account.
2. Enter a recipient, like Amanda, and the amount to send. Ordinary accounts reserve the coins until acceptance.
3. Share the offer URL with the recipient. Acceptance happens on that page while signed into GitHub; the first acceptance successfully merged into the ledger settles it.
4. If nobody has accepted, the sender can cancel and recover the reserved coins.

The website opens a prefilled GitHub issue. Confirm it while signed in; a Go worker creates, validates, and automatically merges a ledger PR, then publishes the updated site. You do not need to fork the repository, install an app, or give the website a token. Requests usually take a short time to process; GitHub Actions and Pages can be delayed.

ChrisCoin has no monetary value. There are no fees, expiration dates, or mining. The starting balance is per account, not per human; multiple accounts each receive it.

Transfers from `cdlewis` create new coins when accepted. That account's offers do not reserve or deduct its balance, and cancellations do not refund anything. The rule uses numeric GitHub ID `553949`, so a rename preserves the privilege and a different account using the login cannot gain it. Receipts from ordinary accounts remain normal funded transfers. Issued supply includes each starting balance and each accepted mint-account offer; amounts and total supply stay within the exact integer range supported by the website. This is a replay rule for the ledger, including existing entries; no special mint event or separate state is stored.

## Requests

Use the website or [open a request](https://github.com/cdlewis/ChrisCoin/issues/new?template=request.md) with exactly one fenced `chriscoin` JSON block:

````markdown
```chriscoin
{"action":"offer","recipient":"Amanda","amount":10}
```
````

Other commands:

```json
{"action":"claim"}
{"action":"accept","offerId":"offer-123"}
{"action":"cancel","offerId":"offer-123"}
```

Use one command per issue. Offer IDs use the original request number: issue #123 creates `offer-123`. Amounts are positive whole coins. The GitHub issue author's numeric account ID is authoritative; there is no sender field. Offering or accepting coins can be your first entry; either automatically includes the 100 CC starting balance. There is no separate claimed flag or allowance registry. The `claim` command simply opens an account and is rejected if that numeric ID is already present. Invalid requests never create an account or issue coins. Usernames are display snapshots, so a rename does not move or reset a balance. User pages use numeric IDs.

An issue is a request, not a ledger entry. Coins change only after the bot's PR merges. The bot freezes the command on its request branch; editing the issue afterward cannot change that PR. Correct a rejected request by opening a new issue. Closing an unprocessed issue withdraws it, but closing an issue after processing starts is not a cancellation: use a cancellation request for a recorded offer. Raw hand-written ledger PRs are not processed automatically.

## Implementation

All backend logic is Go. Name moderation uses the pinned [go-away](https://github.com/TwiN/go-away) library, and social cards use the Go image/font packages. No browser, external image service, or runtime font download is required:

- `internal/ledger`: strict command parsing, append-only events, deterministic replay, and accounting.
- `internal/processor`: authenticated request validation, checkpointing, PR creation and merging, and recovery.
- `internal/github`: repository-scoped REST API client.
- `internal/site`: Go HTML templates and plain CSS/JavaScript. The browser only filters accounts and prepares GitHub request URLs.
- `internal/social`: deterministic 1200×630 PNG cards with the sender, recipient, and amount, using embedded Go fonts and a retro starfield design.

The interface uses a GeoCities-era design: tiled stars, rainbow headings, beveled panels, and web badges. Labels stay factual. Fonts are system fonts; decorative assets are local SVG/CSS. Animations respect reduced-motion preferences. The footer counter reports actual ledger entries, not fabricated visitor counts.

The canonical ledger is `ledger/events.jsonl`. Each event records its sequence, authenticated numeric actor ID, source issue, original body hash, frozen command, timestamp, and PR number. Available and reserved balances are rebuilt from that ledger. JSON exports are at `/data/state.json` and `/data/events.jsonl` beneath the site's base path.

New recipient names and authenticated GitHub usernames must be 1–10 ASCII letters or digits, with single internal hyphens permitted. Spaces, underscores, Unicode, and leading, trailing, or repeated hyphens are rejected. The Go worker rejects names matched by go-away's profanity/slur dictionary plus a small common-insult extension, including common case, digit, and hyphen obfuscations. This applies to every action and to retries before a ledger PR is created or updated. Rejection messages do not repeat the submitted name. Browser validation mirrors the format rules; the backend remains authoritative for moderation and account usernames.

Name policy is checked when admitting new entries, including frozen requests awaiting a retry. Existing ledger records retain their original validation and accounting rules so a stricter limit or a later dictionary change cannot break replay or alter history. The filter is dictionary-based and primarily English; it can have false positives and miss unsupported terms.

The worker runs with one repository-wide concurrency lock, validates against current `main`, and rewrites its own deterministic branch with only the ledger change and its offer PNG before merging its exact head SHA. A request ID can appear only once. The first successful merge determines the winner of competing accept/cancel requests; submission order is not guaranteed. A five-minute scheduled recovery scans open requests because Actions can coalesce pending runs or interrupt execution. Schedules are best-effort and may be delayed; GitHub can disable schedules after 60 days of public-repository inactivity. Manually run the ledger workflow to recover.

Idle recovery runs skip deployment. A failed worker still attempts to publish current `main` in case a merge succeeded before the failure. If Pages publication itself fails, rerun the workflow or dispatch it manually to retry.

Internal page and asset links include the published commit as a cache key. While visible, the browser checks the Go-generated JSON snapshot once per minute and on return to the tab, navigating to the latest version if necessary. This keeps balances fresh behind custom-domain caches. A pre-deployment cached 404 must expire or be bypassed with a versioned homepage URL; the app cannot execute inside an intermediary's cached error page.

Only trusted `main` code runs with write permissions. Fork PR code is never executed by the worker; the independent PR CI has a read-only token. GitHub request data is never interpolated into shell commands. Administrators and other trusted repository writers can change code or history; this is not a decentralized or tamper-proof financial system. Keep ordinary participants at public read access.

Ledger/offer-image PRs are validated by the worker and skip separate PR CI, which GitHub otherwise puts behind approval for bot-created PRs. Code PRs run the full test suite; PRs that change code and ledger data also check the append-only ledger prefix.

## Offer sharing images

An offer PR commits `social/offers/offer-<issue>.png` in the same Git commit as its ledger entry. The card uses immutable offer fields, so retries are byte-for-byte reproducible and acceptance/cancellation does not rewrite it. A static offer page serves Open Graph and Twitter large-image metadata with an absolute HTTPS URL, image dimensions, alt text, and a content-hash cache key. The site also provides a direct share-image link.

`SITE_URL` / `--site-url` sets the absolute public site URL including its base path; the Pages workflow derives it from `configure-pages`, including inherited custom domains. Without it, local builds default to the repository's GitHub Pages URL.

Both validation and site builds verify that committed PNGs match the ledger and renderer. To backfill an older offer or update the card design/font dependencies, regenerate the assets and commit them with the change:

```sh
go run ./cmd/chriscoin generate-images
```

Use `--images-dir` for an alternate source directory. The existing ledger is never edited by image generation.

## Local development

Requires Go 1.26 or newer.

```sh
go test -race ./...
go vet ./...
go run ./cmd/chriscoin validate
go run ./cmd/chriscoin build
```

The build writes `dist/`. Serve it with any static server under `/ChrisCoin/`, or build with `--base-path ''` for a local server root or custom domain. Options include `--repository owner/repo`, `--ledger path`, `--output path`, and `--commit SHA`. Output uses UTF-8 and escapes account names and recipient labels.

The live processor requires `GITHUB_TOKEN` and `GITHUB_REPOSITORY`. Do not run it locally concurrently with Actions: all ledger writers must share the workflow's lock. Test the processor using its in-memory GitHub store instead.

## GitHub setup

1. Create a public repository with Issues and Actions enabled; push this project to `main`.
2. Under **Settings → Actions → General**, allow GitHub Actions to create and approve pull requests. The worker requests its own minimal write permissions; no PAT or OAuth secrets are needed.
3. Under **Settings → Pages**, choose **GitHub Actions** as the publishing source. Keep the `github-pages` environment limited to `main`.
4. Permit squash merging. Do not require human reviews or external status checks on the bot's ledger PRs; the trusted processor validates them itself. Bot-created PR workflow runs can otherwise require manual approval. Never manually merge unverified ledger PRs.
5. Run **ChrisCoin ledger and Pages** once. It processes pending requests and deploys Pages directly; it does not depend on bot commits triggering another workflow.

For a fork, update the README links. The runtime repository and Pages base path come from GitHub Actions, so no code changes are required.

Before claiming the hosted multi-user flow verified, exercise it with two real GitHub accounts. Automated tests cover competing claims and retry failures, but are not proof that a second account used the hosted project.
