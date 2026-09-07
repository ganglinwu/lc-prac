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
