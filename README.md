# lc-prac

A CLI for staying interview-sharp on LeetCode patterns without burning an hour
on a full problem. A session is a handful of 2-4 minute drills that fit a
minute budget you set, defaulting to 12.

The idea: after a break from grinding, what decays first is not your ability to
code, it is instant recall of *which pattern applies and why it is correct*.
These drills target that recall directly.

The deck ships 54 drills across 17 topics (hashmap, two-pointers,
sliding-window, binary-search, stack, heap, linked-list, trees, graphs, matrix,
intervals, prefix-sum, dp, greedy, backtracking, bit-manipulation, complexity),
10 of which are machine-graded code drills.

## Usage

```
go run ./cmd/lcprac                      # a ~12 minute session
go run ./cmd/lcprac drill -m 15          # a 15 minute session
go run ./cmd/lcprac drill -topic dp      # only dynamic programming
go run ./cmd/lcprac drill -weak         # aim the session at your weakest topic
go run ./cmd/lcprac drill -leech        # only the drills you keep missing
go run ./cmd/lcprac drill -seed 42       # reproducible selection
go run ./cmd/lcprac drill -tries 1       # code drills: one shot, then the answer
go run ./cmd/lcprac list                 # every drill, no session
go run ./cmd/lcprac topics               # topics and their drill counts
go run ./cmd/lcprac stats                # accuracy, streak, recent sessions, what is due
go run ./cmd/lcprac review               # reread the drills that keep beating you
go run ./cmd/lcprac add                  # write a drill of your own, one prompt at a time
go run ./cmd/lcprac mine                 # your own drills and where they live
```

Install it as a real binary with `go install ./cmd/lcprac`.

During a session, answer the prompt and press enter, or type `s` to skip.
Ctrl-D quits early and still prints the summary.

## The clock is real

`-m` is a wall-clock ceiling, not just an estimate the session is packed
against. Before each drill lcprac checks how long you have actually taken, and
once the budget is spent it stops and tells you what is left for next time. The
check happens between drills, so a drill you are part way through is never cut
off: a session overruns by at most the one in progress. A code drill's fix-it
loop is bounded by the same clock, so once the budget is spent it stops
offering another try instead of stacking three compiles onto the overrun. The second pass gets a
further quarter of the budget, enough for reinforcement without an open-ended
overrun.

A drill that takes more than twice its estimate gets a pace note, and the
summary prints how long each drill actually took, so the estimates in the deck
can be checked against reality. `-nolimit` removes the ceiling.

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

## Streak and what to practise next

Each sitting is also logged (when, how long, how many drills, how you did), so
the history shows the habit and not just the drills. After a session that
recorded anything, `lcprac` prints your streak; `lcprac stats` shows the last
five sessions, the streak, and a `next up` suggestion:

```
recent sessions
  Thu 28 Aug 21:14  12m   9 drills   77%
  Wed 27 Aug 08:02  11m   8 drills   62%
streak: 2 day(s) in a row
```

A streak counts consecutive days with at least one session. Today counts as
soon as you practise, and a day you have not practised yet does not break it
until it ends, so the count is taken from yesterday while today is still empty.
`next up` names the topic with your worst accuracy once it has at least three
attempts on record, and before that the topic with the most drills you have
never tried. The log keeps the most recent 200 sessions and `stats -reset`
clears it along with the drill history.

## Aiming a session

Two flags spend a whole sitting on what your history says is weak, so you do
not have to read `stats` and copy a topic across:

- `-weak` picks the same topic `next up` names and prints why it picked it
  (the accuracy it is going on, or the coverage gap when there is no accuracy
  yet). An explicit `-topic` wins; `-weak` then says it is being ignored.
- `-leech` narrows the deck to the drills that keep beating you, the same set
  `lcprac review` reads out. Use it when you want to be quizzed on them rather
  than to reread them.

Both can be combined, in which case `-weak` picks the weakest topic among the
leeches. Neither is an error on a thin history: with no leeches, or not enough
attempts to judge a topic, lcprac says so and runs a normal session instead of
refusing to start.

Every drill is written to history as soon as it is graded, not at the end of
the sitting, so a session you abandon halfway keeps what you already did.
Ctrl-C stops the sitting cleanly: it logs the partial session (so it still
counts towards the streak), prints what was saved, and exits. Quitting with
Ctrl-D at a prompt ends the session the same way but still shows the summary.

History lives in `$LCPRAC_HOME/progress.json` if that is set, otherwise
`$XDG_DATA_HOME/lcprac/` or `~/.local/share/lcprac/`. Two flags opt out:
`-shuffle` picks at random and ignores your history, `-nosave` runs a session
without recording it, `-noretry` drops the second pass, `-nolimit` removes the
time ceiling.

## Review: the drills that keep beating you

A drill you have missed three times is not bad luck, it is something that has
not stuck. `lcprac review` prints those with their answers open, so you can
read them instead of being quizzed again. A drill leaves the list once you
have solved it cleanly twice in a row, and `stats` says how many are on it.

```
lcprac review                 # up to 5, worst first
lcprac review -n 0            # all of them
lcprac review -topic dp       # only one topic
lcprac review -all            # everything you have ever missed once
lcprac review hashmap-two-sum-code # any drill by id, history or not
```

## Drill kinds

| kind | graded by | shape |
| --- | --- | --- |
| `recall` | you | "what is the loop invariant here?" |
| `choice` | the CLI | multiple choice |
| `complexity` | the CLI | "what is the big-O?", compared loosely |
| `snippet` | you | write a few lines, compare to a model answer |
| `code` | the Go toolchain | write a working function, real tests run against it |

Free-form kinds are self-graded on purpose: reading the model answer and
deciding whether you had it is the actual practice.

## Hints

Staring at a blank prompt for four minutes is the failure mode a short session
cannot afford. Press `h` at any prompt to reveal the next hint (up to three,
ordered from a nudge towards the idea to a nudge towards the mechanics):

```
Your answer (h for a hint, s to skip): h

hint 1/2: A subarray sum is a difference of two prefix sums.
```

A hinted solve still counts as correct for the session score and for accuracy,
but the scheduler treats it as *assisted*: your streak holds where it is rather
than advancing, so the drill comes back at the same interval instead of being
deferred on borrowed help. The summary marks it `~` instead of `+`, and
`lcprac stats` reports how many correct answers needed a hint.

Every code drill ships hints. Drills without them treat `h` as an ordinary
answer, so nothing is swallowed.

## Code drills

A `code` drill is the one kind that is not taken on trust. You get a stub, you
write the function, and `lcprac` builds a throwaway module around it and runs
the drill's test file:

```
e to open $EDITOR, or type your code and end with a line "." (s to skip):
```

Type the function and finish with a line containing only `.` (Ctrl-D works
too), or press `e` to open `$EDITOR` on the stub. On a pass it says so and
moves on; on a failure you see the actual `go test` output and are offered
another try. A run that never terminates is killed after 30 seconds and counts
as a failed try.

### Fixing it yourself

A failed compile does not end the drill. After the test output you get:

```
r to fix it (2 tries left), anything else to give up:
```

`r` re-opens entry seeded with what you just wrote (`e` in the editor opens
your last version, not the stub), so a missing return or an off-by-one costs
you a try rather than the drill. Reading the failing test and repairing your
own code is most of the value, which is why the working version is only shown
once you give up or run out of tries. Three tries by default; `-tries N`
changes it, and `h` still works at the retry prompt. The loop also stops early
if the session clock runs out mid-drill, unless you passed `-nolimit`.

Solving on the second or third try still counts as a solve and advances the
repetition ladder: you got there yourself. The summary shows the try count next
to the drill.

The generated module has no dependencies and is built with `GOPROXY=off`, so
grading is offline and cannot pull anything. Your source lands in its own file,
so an `import` block of your own is fine. If no `go` binary is on `PATH`, code
drills quietly fall back to self-grading rather than failing.

## Your own drills

You do not need to rebuild to add a drill. Any `*.json` file in your drills
directory is loaded on top of the builtin deck:

```
lcprac add           # answer a few prompts, drill lands in mine.json
lcprac mine          # show the directory and what it currently adds
lcprac mine -init    # write an example.json there to copy
```

`add` is the fast path: it asks for kind, title, topic, difficulty, prompt,
answer, explanation, minutes, and optional hints and refs, then shows the drill
the way `review` would before writing it. A choice drill collects its options
and takes the answer by number, so the answer always matches one of them. The
id is slugged from the title (`mine-...`) and suffixed if it is already taken,
though you can type your own; reusing a builtin id is allowed and says so,
since that is how you replace a drill.

Bad input re-asks rather than aborting, Ctrl-D or answering `n` at the
confirmation writes nothing, and the append goes through a temp file so a
failure cannot truncate what you already wrote. `-file other.json` appends
somewhere else in the same directory. Long or multi-line prompts are easier to
paste into the file afterwards: everything `add` writes is ordinary JSON.

The directory is `$LCPRAC_HOME/drills`, else `$XDG_DATA_HOME/lcprac/drills`,
else `~/.local/share/lcprac/drills`. The file format is the same array of
drills as the builtin deck, and your drills take part in selection, scheduling
and stats like any other. `list` marks them with `*`.

A drill that reuses a builtin id replaces it in place, which is how you rewrite
one you disagree with. A malformed file stops the CLI with the filename and the
problem, rather than being skipped silently; `-builtin` on `drill` or `list`
ignores your directory entirely if you need to practise anyway.

## Adding drills to the deck

Drills live in `internal/drill/data/*.json` and are compiled into the binary
with `go:embed`, so there is nothing to install or configure. Add an object to
any file in that directory; `go test ./...` validates the whole deck, so a
malformed drill fails the build rather than a session. The deck guards check
more than the schema: every code drill's model answer must pass its own tests,
every stub must build without already passing, all embedded Go must be
gofmt-clean, and no topic may hold fewer than two drills.

One gotcha when writing a code drill: the preamble is compiled as a separate
file from your solution, so an `import` in the preamble does not cover the
solution. Either the user writes the import themselves, or the preamble exposes
a helper that hides it (which is what the intervals drill does with `sort`).

## Layout

```
cmd/lcprac        flag parsing and subcommands
internal/drill    the Drill type, validation, and the embedded deck
internal/session  picking drills that fit a time budget
internal/runner   the interactive prompt/answer/grade loop
internal/codecheck compiles a code drill's answer and runs its tests
internal/progress recorded history and the spaced-repetition schedule
```
