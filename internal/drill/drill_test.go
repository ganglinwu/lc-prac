package drill

import "testing"

func valid() Drill {
	return Drill{
		ID: "x", Title: "T", Kind: KindRecall, Topic: "hashmap",
		Difficulty: Easy, EstMinutes: 3, Prompt: "p", Answer: "a", Explanation: "e",
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Drill)
		wantErr bool
	}{
		{"ok", func(*Drill) {}, false},
		{"no id", func(d *Drill) { d.ID = " " }, true},
		{"bad kind", func(d *Drill) { d.Kind = "quiz" }, true},
		{"bad difficulty", func(d *Drill) { d.Difficulty = "brutal" }, true},
		{"hard difficulty", func(d *Drill) { d.Difficulty = Hard }, false},
		{"minutes too big", func(d *Drill) { d.EstMinutes = 30 }, true},
		{"minutes zero", func(d *Drill) { d.EstMinutes = 0 }, true},
		{"no explanation", func(d *Drill) { d.Explanation = "" }, true},
		{"choices on recall", func(d *Drill) { d.Choices = []string{"a", "b"} }, true},
		{"choice ok", func(d *Drill) {
			d.Kind = KindChoice
			d.Choices = []string{"a", "b"}
			d.Answer = "a"
		}, false},
		{"choice answer not listed", func(d *Drill) {
			d.Kind = KindChoice
			d.Choices = []string{"a", "b"}
			d.Answer = "c"
		}, true},
		{"choice too few options", func(d *Drill) {
			d.Kind = KindChoice
			d.Choices = []string{"a"}
			d.Answer = "a"
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := valid()
			tt.mutate(&d)
			err := d.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewSetRejectsDuplicateIDs(t *testing.T) {
	a, b := valid(), valid()
	if _, err := NewSet([]Drill{a, b}); err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestSetFilterAndTopics(t *testing.T) {
	a := valid()
	b := valid()
	b.ID = "y"
	b.Topic = "dp"
	b.Kind = KindComplexity
	b.Difficulty = Medium

	set, err := NewSet([]Drill{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if got := set.Topics(); len(got) != 2 || got[0] != "dp" || got[1] != "hashmap" {
		t.Fatalf("Topics() = %v", got)
	}
	if got := set.Filter("HASHMAP", "", ""); len(got) != 1 || got[0].ID != "x" {
		t.Fatalf("topic filter should be case-insensitive, got %v", got)
	}
	if got := set.Filter("", KindComplexity, Medium); len(got) != 1 || got[0].ID != "y" {
		t.Fatalf("combined filter = %v", got)
	}
	if got := set.Filter("dp", KindRecall, ""); len(got) != 0 {
		t.Fatalf("filters should AND, got %v", got)
	}
}

func TestSelfGraded(t *testing.T) {
	for kind, want := range map[Kind]bool{
		KindRecall: true, KindSnippet: true, KindChoice: false, KindComplexity: false,
	} {
		d := valid()
		d.Kind = kind
		if got := d.SelfGraded(); got != want {
			t.Errorf("%s SelfGraded() = %v, want %v", kind, got, want)
		}
	}
}

// The embedded content is the product, so a malformed drill should fail the build's tests.
func TestBuiltinIsValid(t *testing.T) {
	set, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	if set.Len() < 20 {
		t.Fatalf("only %d builtin drills, expected a usable deck", set.Len())
	}
	if len(set.Topics()) < 5 {
		t.Fatalf("only %d topics: sessions would not be varied", len(set.Topics()))
	}
}

func TestHintValidation(t *testing.T) {
	base := func() Drill {
		return Drill{
			ID: "h", Title: "T", Kind: KindRecall, Topic: "dp", Difficulty: Easy,
			EstMinutes: 2, Prompt: "p", Answer: "a", Explanation: "e",
		}
	}
	ok := base()
	ok.Hints = []string{"one", "two", "three"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("three hints should be fine: %v", err)
	}
	tooMany := base()
	tooMany.Hints = []string{"1", "2", "3", "4"}
	if err := tooMany.Validate(); err == nil {
		t.Error("four hints should be rejected")
	}
	empty := base()
	empty.Hints = []string{"fine", "  "}
	if err := empty.Validate(); err == nil {
		t.Error("a blank hint should be rejected")
	}
}
