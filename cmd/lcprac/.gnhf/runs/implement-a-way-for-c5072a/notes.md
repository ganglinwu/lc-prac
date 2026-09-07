# gnhf run: implement-a-way-for-c5072a

Objective: see .gnhf/runs/implement-a-way-for-c5072a/prompt.md

## Iteration Log

### Iteration 1

**Summary:** Added the deterministic two-way merge of progress histories (plus byte-level encode/decode for transport) that cross-machine sync will be built on, with nine tests covering the re-sync and conflict cases.

**Changes:**
- internal/progress/merge.go: Store.Merge folds a remote history into a new store bound to the local path. Counters take the max rather than the sum so a re-synced sitting cannot inflate accuracy; streak/LastSeen/DueAt come as a set from the later attempt; notes survive from whichever side wrote one; saved solutions prefer a passing version, then the later one. Merge is symmetric and does not mutate the receiver.
- internal/progress/merge.go: sessions and attempts are unioned, deduped on a timestamp+content key, sorted oldest-first and re-capped at maxSessions/maxAttempts with the oldest dropped, so DayStreak stays correct after a merge.
- internal/progress/merge.go: Store.Encode and Decode(path, bytes) let history move as bytes rather than only as a file; progress.go's Save/Load were rewired through them so there is still a single serialization format and no duplicated parsing.
- internal/progress/merge_test.go: nine tests covering encode/decode round-trip, empty decode, one-sided records, idempotent re-sync of an identical history, schedule-from-later-attempt plus merge symmetry, note retention, passing-solution preference, log union with streak, and log capping.

**Learnings:**
- The progress store is the whole sync surface: records (per-drill counters, streak, DueAt, note, saved code), a capped session log, and a capped attempt log, all in one JSON file at $LCPRAC_HOME/XDG/~/.local/share/lcprac/progress.json. There is no device id or per-event log, so merging has no common ancestor to diff against; max-on-counters is the safe approximation and the alternative (summing) silently corrupts accuracy on any repeated sync.
- AWS recon: exactly one Lightsail instance exists, 'apple-health-mcp' (Ubuntu, running) in ap-southeast-1 at 54.169.114.237. Note the region: default-region aws calls may miss it.
- guenyanghae.com is on Route 53 nameservers (ns-1873.awsdns-42.co.uk et al) but its apex resolves to CloudFront IPs (108.157.254.x), NOT the Lightsail instance, and `aws lightsail get-domains` returns empty. The Caddy host must be reached via some subdomain not yet identified; 'health.guenyanghae.com' does not resolve. Next iteration should list the Route 53 hosted-zone records and read the Caddyfile on the box before designing the sync endpoint.
- The repo has no existing HTTP/server/network code at all (internal/ is codecheck, drill, progress, runner, session) and no third-party dependencies in go.mod, so the sync transport should stay on net/http from the standard library to match.

### Iteration 2

**Summary:** Built and tested the server half of cross-machine sync: an authenticated HTTP endpoint that merges a machine's pushed progress history into a canonical server copy and returns the merge, plus the lcpracd daemon that hosts it.

**Changes:**
- internal/syncsrv: POST /v1/sync accepts a machine's whole encoded history, folds it into the server's stored copy with Store.Merge, persists atomically and returns the merged history, so one round trip converges both sides. GET /v1/sync reads the stored copy for a fresh machine, /healthz for the proxy.
- internal/syncsrv: bearer-token auth using constant-time comparison, tokens mapped to account names, account names validated to [A-Za-z0-9_-] so they cannot escape the data directory when used as a filename, minimum 16-char tokens enforced at construction, 8 MB request body cap, and a single mutex serialising every read-merge-write.
- cmd/lcpracd: the daemon binary, loopback-only by default on 127.0.0.1:8090 (8080 is already taken by the apple-health server on the box) with read/write timeouts, configured by LCPRAC_SYNC_ADDR / LCPRAC_SYNC_DATA / LCPRAC_SYNC_TOKENS in account:token,account:token form so adding an identity is one env edit and a restart.
- internal/syncsrv/syncsrv_test.go and cmd/lcpracd/main_test.go: 13 tests covering two-machine merge, idempotent re-push (counters do not drift across three identical pushes), per-account isolation, config rejection, 401/400/405 paths, disk persistence and token parsing.
- Verified the compiled lcpracd end to end with curl (healthz, push, get, 401) against a temp data dir, then stopped it.

**Learnings:**
- The Caddy host is applehealth.guenyanghae.com -> 54.169.114.237 (the apple-health-mcp Lightsail box); the apex and www are CloudFront and unrelated to the VPS. SSH as ubuntu@54.169.114.237 works with the existing key and no password, so deployment can be scripted from here.
- /etc/caddy/Caddyfile on the box is one reverse_proxy block for applehealth -> 127.0.0.1:8080, TLS auto-provisioned by ACME, and the Go app behind it is loopback-only. Adding sync means a second vhost block plus a new Route 53 A record; port 8080 is taken, hence lcpracd defaulting to 8090.
- progress.Decode("", body) is the right way to parse an uploaded history: New("") makes Save a no-op, and Merge builds its result on the receiver's path, so the server's own file path is what gets written and the client's store can never accidentally save.
- Merge being symmetric and max-on-counters means the server needs no version vectors or conflict UI at all: repeated pushes of an unchanged history are provably a no-op, which is what makes a single POST-and-return-merged endpoint sufficient.

### Iteration 3

**Summary:** Built and tested the client half of cross-machine sync: an `internal/synccli` package plus a `lcprac sync` command that pushes this machine's history to the server, merges the canonical answer back, and reports what arrived.

**Changes:**
- internal/synccli: Config stored in sync.json beside progress.json at mode 0600 (it holds a token), with LCPRAC_SYNC_URL/LCPRAC_SYNC_TOKEN as overrides for a one-off run. Validate refuses a non-http(s) url, a missing token, and plain http to any non-loopback host, so the token cannot cross the network in the clear; loopback http stays allowed so local testing works.
- internal/synccli: Client.Push/Pull hit <url>/v1/sync with a bearer token and a 30s timeout, and non-200 responses are turned into an error carrying the endpoint's JSON {"error":...} message, falling back to the raw body so a proxy's 502 is still legible.
- internal/synccli: Sync() encodes the local store, pushes it, decodes the server's merge and folds it into the local store with Store.Merge, returning the merged store without saving so the caller can offer a dry run. DiffStores reports new drills, updated drills, new sessions and new attempts; sameRecord compares fields rather than structs because Record.Solution is a pointer and equal solutions from two sides would otherwise read as a change.
- cmd/lcprac/sync.go: the `lcprac sync` subcommand. -set-url / -set-token each update only the field named so rotating a token does not unset the server, -status prints the destination and the token's length but never the token, -n shows what would come back without writing, and a plain run saves and reports the new drill count. Wired into main.go's dispatch and usage text.
- internal/synccli/synccli_test.go: 12 tests run against the real syncsrv rather than a stub, covering a two-machine handoff, idempotent re-sync with no counter drift, a wrong token surfacing the server's message, GET pull, url trailing-slash trimming, config round trip plus 0600 mode plus env override, the missing-config case, delta counting, proxy-error fallback and the Authorization header.
- cmd/lcprac/sync_test.go: 6 tests over the command layer covering config path resolution under LCPRAC_HOME, partial config updates, a pull writing to local history, a dry run writing nothing, the already-in-sync path, the unconfigured error naming -set-url, and -status not leaking the token.
- README.md: a "Practising on more than one machine" section documenting the sync commands, why the merge makes repeated syncs a no-op, the config file and env overrides, the http-refusal rule, and how to run lcpracd behind a TLS proxy; plus sync in the usage list and the three new packages in the layout.

**Learnings:**
- Store.Merge already returns a store bound to the receiver's path, so the client can merge the server's response into its own store and Save() writes to the right local file with no path juggling; progress.Decode("", body) for the response keeps the server's copy unsaveable by accident.
- Diffing two progress stores cannot use struct equality: Record.Solution is a *Solution, so two identical solutions arriving from different sides compare unequal and every synced drill would be reported as changed. Field-by-field comparison (with time.Equal for LastSeen/DueAt) is required for the "already in sync" message to ever be true.
- Refusing plain http to non-loopback hosts in Config.Validate is a real constraint on deployment, not just hygiene: it means the VPS must be reached through the Caddy TLS vhost, never the Lightsail IP or a raw port. Loopback has to stay exempt or the local end-to-end test is impossible.
- Testing the client against the actual syncsrv.Handler() via httptest, instead of a hand-written stub, caught the whole protocol in one place and made the idempotence test meaningful: three round trips through the real merge code confirm counters do not drift, which a stub would have asserted about nothing.
- Both halves of sync now exist and are verified end to end with the compiled binaries. What remains for the objective is deployment (systemd unit for lcpracd on 54.169.114.237, a second Caddy vhost, a Route 53 A record for a sync subdomain) and the separate ask of adding more drills to the bank.

### Iteration 4

**Summary:** Deployed the sync server to the Lightsail VPS behind Caddy TLS at https://lcprac.guenyanghae.com and verified a real two-machine sync over the public internet, with the install captured as a reproducible deploy script.

**Changes:**
- Route 53 A record lcprac.guenyanghae.com -> 54.169.114.237 created in hosted zone Z0538799AASM7SVVJMH1, and a second Caddy vhost on the box reverse-proxying it to 127.0.0.1:8090 with an ACME-provisioned Let's Encrypt certificate.
- lcpracd running as an enabled systemd service on the VPS under a dedicated non-login lcpracd user, loopback-only, with hardening (ProtectSystem=strict, NoNewPrivileges, PrivateTmp, RestrictAddressFamilies, SystemCallFilter=@system-service) and its history in /var/lib/lcpracd at mode 0700; tokens live in /etc/lcpracd/lcpracd.env at 0640 root:lcpracd, outside the world-readable unit file.
- deploy/deploy.sh: one-command install over ssh (cross-compile, upload, create user, install unit, generate a token only if none exists, append the Caddy vhost only if absent, then poll the public /healthz until it answers or fail loudly). Verified idempotent by running it twice. Accompanied by deploy/lcpracd.service and deploy/Caddyfile.lcprac.
- cmd/lcprac/sync.go: the no-delta message changed from 'already in sync' to 'in sync with <url> (N drills); nothing new to bring back', because Sync always pushes first and the old wording implied the call had done nothing when it had in fact uploaded new history. Test assertion updated to match.
- README.md: a 'Deploying the server' subsection covering the deploy script, the DNS-before-ACME ordering, redeploy-does-not-rotate-the-token, how to add a second account, and the live endpoint.
- This machine configured against the live server (~/.local/share/lcprac/sync.json) and its real 3-drill history pushed; the scripted test data used during verification was removed from the server first.

**Learnings:**
- The full client/server round trip works unchanged against real TLS infrastructure: a scripted two-machine handoff (A practises, pushes; fresh B pulls 2 drills and 1 session; both re-sync as no-ops) left A and B with byte-identical progress.json, confirming the max-on-counters merge converges in production and not just in httptest.
- Caddy's ACME run races the deploy script's own verification: the first https request after adding a vhost failed with a TLS internal error while the certificate was still being issued seconds later. Any deploy check on a brand-new vhost needs a retry loop, not a single curl.
- `set -e` does not abort on a failing command that sits before the final `&&` in a list, so the original `curl -fsS ... && echo` let a failed public health check exit 0 and print '==> done'. Deploy verification must be written as an explicit if/exit, or a broken deploy reports success.
- Port 8080 on the box is the apple-health server and 2019 is Caddy's admin API; 8090 was free, so lcpracd's default needed no change. Caddy on that host is a single /etc/caddy/Caddyfile with one vhost block per site, so appending a block plus `systemctl reload caddy` is the whole integration.
- guenyanghae.com's hosted zone is Z0538799AASM7SVVJMH1; the apex and www are CloudFront aliases and only applehealth (now also lcprac) point at the Lightsail box. New subdomain records propagate to the authoritative NS within seconds, so DNS is not a meaningful wait in this deploy.

### Iteration 5

**Summary:** Expanded the drill bank from 60 to 72 drills by adding four missing interview-staple topics (union-find, trie, topological-sort, monotonic deque), each with a machine-graded code drill plus two conceptual drills, all verified by the existing bank test suite.

**Changes:**
- internal/drill/data/code-structures.json: four machine-graded code drills, one per new topic. countComponents via DSU with path compression and union by rank; a Trie with Insert/Search/StartsWith; canFinish via Kahn's indegree queue; maxSlidingWindow via a monotonic deque of indices. Each ships a stub that compiles but fails, a model answer proven to pass its own tests, and hints that do not quote the answer.
- Test files for the new code drills go past happy-path cases: countComponents covers duplicate and reversed edges plus a 2000-node chain, canFinish covers a self-loop and a 20000-node chain that a recursive DFS would stack-overflow on, maxSlidingWindow cross-checks against a brute-force max over pseudorandom input at four window sizes, and the Trie covers the empty word and prefix-but-not-word cases.
- internal/drill/data/core-structures.json: eight recall/choice/complexity drills, two per new topic. Union-find's two optimisations and DSU-vs-BFS selection; trie-vs-hash-set and the O(L) lookup; Kahn's loop from memory and its free cycle test; what the monotonic deque holds and why the inner while loop is still O(n) amortised.
- README.md: deck description updated to 72 drills across 21 topics with the four new topic names, and the code-drill count corrected from 16 to 20.

**Learnings:**
- The bank has structural guard tests that constrain how new topics may be added, not just how drills are shaped: TestBuiltinTopicsAreUsable rejects any topic with fewer than two drills and TestEveryTopicHasCodeDrill rejects any non-complexity topic without a code drill. A new topic is therefore a minimum three-drill unit of work, one of which must be machine-graded, so topics cannot be introduced incrementally one drill at a time.
- TestBuiltinCodeSourcesAreFormatted compares the embedded Go against gofmt byte for byte, and hand-authored struct literals in test tables fail it on field alignment alone. Generating the JSON and then piping every stub/answer/preamble/tests field through `gofmt` before writing is the reliable authoring loop; writing the Go by hand and hoping it matches wastes a test cycle per drill.
- A code drill stub must fail its tests without panicking to give a clean failure: the Trie stub returns &Trie{} from its constructor rather than nil, because a nil return would make every method call a nil-pointer panic and the user's first run would show a stack trace instead of a failed assertion. The guard test only checks that the stub does not pass and has no syntax/undefined errors, so panic-avoidance is an authoring judgement the tests will not catch.
- Grepping the repo root now picks up the gnhf iteration JSONL transcripts under cmd/lcprac/.gnhf/, which echo back entire files that were read earlier in the session and swamp the real matches. Scope greps to internal/ and cmd/lcprac/*.go, or exclude .gnhf explicitly.
- The drill bank is embedded in the binary via //go:embed, so new questions are not carried by the sync server at all: sync moves progress history only. A second machine picks up these four topics by rebuilding or redeploying the client binary, and until it does its progress file will simply have no records for the new drill ids, which the max-on-counters merge handles as absent rather than as zero.

### Iteration 6

**Summary:** Made sync automatic: a drill session now pulls before it picks drills and pushes when it ends, on by default once a server is configured, with -nosync/-auto off escape hatches and soft failure when the server is unreachable.

**Changes:**
- internal/synccli: Config gained an `auto` field stored as *bool so a config file written before this iteration reads as auto-on (setting up a server is itself the opt-in), plus Config.AutoEnabled/Ready, an LCPRAC_SYNC_AUTO env override that errors on a non-boolean rather than silently disabling, and NewAutoClient with an 8s timeout so an unattended sync gives up sooner than a hand-typed one.
- cmd/lcprac/sync.go: autoSync() is the session-time sync; it returns no error at all, so a down server costs a line on stderr and practice continues on local history. autoConfig() stays silent when the machine is unconfigured or auto is off, so an unconfigured machine never even creates progress.json.
- cmd/lcprac/sync.go: `lcprac sync -auto on|off` persists the setting through the same field-preserving path as -set-url/-set-token, accepting the words the help text uses (on/off/yes/no/true/false) rather than only Go booleans; -status and the save confirmation now report the auto state.
- cmd/lcprac/main.go: cmdDrill pulls before openStore (so scheduling sees other machines' history) and pushes quietly after the session summary; a new -nosync flag and the existing -nosave both suppress it. Usage text updated for -nosync and -auto.
- cmd/lcprac/sync_test.go: 9 new tests over pull-before, push-after, quiet mode, the unconfigured no-op, -auto off never touching the network, an unreachable server leaving local history intact, the -auto round trip, its error message, and -status reporting the state. internal/synccli: 2 tests for auto defaulting on for a legacy config and the env override.
- README.md: the multi-machine section now leads with sessions syncing on their own and documents -nosync, -auto off, LCPRAC_SYNC_AUTO and the -nosave exemption.

**Learnings:**
- A bool config field added after release must be *bool, not bool: every existing sync.json on a machine already set up would decode `auto` as false and silently keep syncing manual. nil-means-on made the feature retroactive with no migration.
- The pull must run before openStore() in cmdDrill, not after: the store is read once into memory at session start and drives spaced-repetition priority, so a pull afterwards would merge history the session had already scheduled against.
- Post-session sync can safely reload from disk rather than reusing the in-memory store, because liveProgress.record saves after every graded drill and logSession saves the sitting; by the time cmdDrill returns, disk is already authoritative.
- strconv.ParseBool does not accept "on"/"off", which is exactly the vocabulary a CLI flag documented as `-auto on|off` invites. A small parseOnOff wrapper was needed, caught only because the test used the word from the help text.
- Verifying against the live server without disturbing the real history is easy here: copy sync.json into a temp LCPRAC_HOME and run the binary there. The merge being max-on-counters means the throwaway machine's empty push is provably harmless, so an end-to-end check against production costs nothing.
