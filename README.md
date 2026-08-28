# lc-prac

A CLI for staying interview-sharp on LeetCode patterns without burning an hour
on a full problem. A session is a handful of 2-4 minute drills that fit a
minute budget you set, defaulting to 12.

The idea: after a break from grinding, what decays first is not your ability to
code, it is instant recall of *which pattern applies and why it is correct*.
These drills target that recall directly.

## Usage

```
go run ./cmd/lcprac                      # a ~12 minute session
go run ./cmd/lcprac drill -m 15          # a 15 minute session
go run ./cmd/lcprac drill -topic dp      # only dynamic programming
go run ./cmd/lcprac drill -seed 42       # reproducible selection
go run ./cmd/lcprac list                 # every drill, no session
go run ./cmd/lcprac topics               # topics and their drill counts
go run ./cmd/lcprac stats                # accuracy so far and what is due
```

Install it as a real binary with `go install ./cmd/lcprac`.

During a session, answer the prompt and press enter, or type `s` to skip.
Ctrl-D quits early and still prints the summary.

## Second pass

Anything you miss is re-asked once at the end of the session, while the
explanation is still fresh. The second attempt is not scored and does not touch
your history: a drill you missed still comes back on the 10 minute rung, so
getting it right immediately after reading the answer cannot fake a streak.
Skips are not misses and are never re-asked. `-noretry` turns the pass off.

## Spaced repetition

Every graded drill is recorded, and the result decides when it comes back:

| streak of correct answers | comes back in |
| --- | --- |
| 0 (you missed it) | 10 minutes |
| 1 | 1 day |
| 2 | 3 days |
| 3 | 1 week |
| 4 | 3 weeks |
| 5+ | 2 months |

Sessions then pick overdue drills first, never-seen drills next, and resting
drills only to fill the budget. Skipped drills are not recorded, so skipping is
free. `lcprac stats` shows per-topic accuracy and how much is due; add `-reset`
to wipe history.

History lives in `$LCPRAC_HOME/progress.json` if that is set, otherwise
`$XDG_DATA_HOME/lcprac/` or `~/.local/share/lcprac/`. Two flags opt out:
`-shuffle` picks at random and ignores your history, `-nosave` runs a session
without recording it, `-noretry` drops the second pass.

## Drill kinds

| kind | graded by | shape |
| --- | --- | --- |
| `recall` | you | "what is the loop invariant here?" |
| `choice` | the CLI | multiple choice |
| `complexity` | the CLI | "what is the big-O?", compared loosely |
| `snippet` | you | write a few lines, compare to a model answer |

Free-form kinds are self-graded on purpose: reading the model answer and
deciding whether you had it is the actual practice.

## Adding drills

Drills live in `internal/drill/data/*.json` and are compiled into the binary
with `go:embed`, so there is nothing to install or configure. Add an object to
any file in that directory; `go test ./...` validates the whole deck, so a
malformed drill fails the build rather than a session.

## Layout

```
cmd/lcprac        flag parsing and subcommands
internal/drill    the Drill type, validation, and the embedded deck
internal/session  picking drills that fit a time budget
internal/runner   the interactive prompt/answer/grade loop
internal/progress recorded history and the spaced-repetition schedule
```
