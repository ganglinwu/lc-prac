package drill

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// EncodeDrills renders drills as the same JSON array shape a user drill file
// uses, so what crosses the network is what could sit on disk.
func EncodeDrills(ds []Drill) ([]byte, error) {
	if ds == nil {
		ds = []Drill{}
	}
	b, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// DecodeDrills reads a drill array and validates every entry, so a corrupt or
// hostile upload is refused before it reaches anyone's deck. Empty bytes mean
// no drills rather than an error.
func DecodeDrills(b []byte) ([]Drill, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var ds []Drill
	if err := json.Unmarshal(b, &ds); err != nil {
		return nil, fmt.Errorf("drill: parsing drills: %w", err)
	}
	for i := range ds {
		if err := ds[i].Validate(); err != nil {
			return nil, err
		}
		ds[i].Source = SourceUser
	}
	return ds, nil
}

// MergeDrills unions two sets of user drills by id, newest edit winning. The
// result is sorted by id so both machines end up with byte-identical files.
func MergeDrills(local, remote []Drill) []Drill {
	byID := make(map[string]Drill, len(local)+len(remote))
	for _, d := range append(append([]Drill{}, local...), remote...) {
		if cur, ok := byID[d.ID]; ok {
			byID[d.ID] = laterDrill(cur, d)
			continue
		}
		byID[d.ID] = d
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Drill, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out
}

// laterDrill picks the winner of two versions of one drill. Equal timestamps
// are broken on the encoded form rather than on arrival order, because a merge
// that depended on which side pushed first would never converge.
func laterDrill(a, b Drill) Drill {
	ta, tb := editedAt(a), editedAt(b)
	switch {
	case tb.After(ta):
		return b
	case ta.After(tb):
		return a
	}
	if canonical(b) > canonical(a) {
		return b
	}
	return a
}

// editedAt treats an unstamped drill as the oldest possible, so a drill
// written before stamping existed loses to any edited copy of it.
func editedAt(d Drill) time.Time {
	if d.UpdatedAt == nil {
		return time.Time{}
	}
	return d.UpdatedAt.UTC()
}

// canonical is the drill's JSON, used only to break a timestamp tie.
func canonical(d Drill) string {
	b, err := json.Marshal(d)
	if err != nil {
		return d.ID
	}
	return string(b)
}

// SameDrill reports whether two versions of a drill are the same edit. It
// ignores Source, which the loader fills in from where the file was, and
// compares timestamps by instant rather than by wall clock and location.
func SameDrill(a, b Drill) bool {
	if !editedAt(a).Equal(editedAt(b)) {
		return false
	}
	a.UpdatedAt, b.UpdatedAt = nil, nil
	a.Source, b.Source = "", ""
	return canonical(a) == canonical(b)
}
