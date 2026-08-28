package drill_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ganglinwu/lc-prac/internal/codecheck"
	"github.com/ganglinwu/lc-prac/internal/drill"
)

// TestBuiltinCodeDrillsAreSolvable compiles each code drill's model answer
// against its own tests. A drill whose answer fails is a broken drill, and this
// is the only place that catches it before a session does.
func TestBuiltinCodeDrillsAreSolvable(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles every code drill")
	}
	if !codecheck.Available() {
		t.Skip("no go toolchain on PATH")
	}
	set, err := drill.Builtin()
	if err != nil {
		t.Fatalf("Builtin: %v", err)
	}
	code := set.Filter("", drill.KindCode, "")
	if len(code) == 0 {
		t.Fatal("no code drills in the builtin deck")
	}
	for _, d := range code {
		t.Run(d.ID, func(t *testing.T) {
			t.Parallel()
			res, err := codecheck.Run(context.Background(), codecheck.Program{
				Preamble: d.Code.Preamble,
				Source:   d.Answer,
				Tests:    d.Code.Tests,
			})
			if err != nil {
				t.Fatalf("grade: %v", err)
			}
			if !res.Passed {
				t.Fatalf("model answer does not pass its own tests:\n%s", res.Output)
			}
		})
	}
}

// TestBuiltinCodeStubsCompile catches a stub that would not even build, which
// would show the user a compile error they did not cause.
func TestBuiltinCodeStubsCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles every code drill")
	}
	if !codecheck.Available() {
		t.Skip("no go toolchain on PATH")
	}
	set, err := drill.Builtin()
	if err != nil {
		t.Fatalf("Builtin: %v", err)
	}
	for _, d := range set.Filter("", drill.KindCode, "") {
		t.Run(d.ID, func(t *testing.T) {
			t.Parallel()
			res, err := codecheck.Run(context.Background(), codecheck.Program{
				Preamble: d.Code.Preamble,
				Source:   d.Code.Stub,
				Tests:    d.Code.Tests,
			})
			if err != nil {
				t.Fatalf("grade: %v", err)
			}
			if res.Passed {
				t.Fatal("stub passes the tests, so the drill asks for nothing")
			}
			if strings.Contains(res.Output, "syntax error") || strings.Contains(res.Output, "undefined:") {
				t.Fatalf("stub does not build cleanly:\n%s", res.Output)
			}
		})
	}
}
