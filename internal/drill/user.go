package drill

import (
	"fmt"
	"os"
	"path/filepath"
)

// SourceBuiltin and SourceUser say where a drill came from, so the CLI can
// mark your own drills and so a broken file names itself in the error.
const (
	SourceBuiltin = "builtin"
	SourceUser    = "user"
)

// UserDir is where your own drill files live: $LCPRAC_HOME/drills, else
// $XDG_DATA_HOME/lcprac/drills, else ~/.local/share/lcprac/drills.
func UserDir() (string, error) {
	if h := os.Getenv("LCPRAC_HOME"); h != "" {
		return filepath.Join(h, "drills"), nil
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "lcprac", "drills"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "lcprac", "drills"), nil
}

// LoadUserDir reads every *.json file in dir as the drills to practise, with
// tombstones left out. A missing directory is not an error: most people never
// write their own.
func LoadUserDir(dir string) ([]Drill, error) {
	all, err := LoadUserDirAll(dir)
	if err != nil {
		return nil, err
	}
	return LiveDrills(all), nil
}

// LoadUserDirAll is LoadUserDir including the tombstones, which sync needs:
// dropping them here is what would resurrect a deleted drill on the next pull.
func LoadUserDirAll(dir string) ([]Drill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var all []Drill
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		ds, err := readDrillFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		for i := range ds {
			ds[i].Source = SourceUser
		}
		all = append(all, ds...)
	}
	return all, nil
}

// Merge lays extra drills over base. A drill whose ID already exists replaces
// the original in place, so you can rewrite a builtin drill you disagree with
// without losing its position.
func Merge(base *Set, extra []Drill) (*Set, error) {
	drills := base.All()
	byID := make(map[string]int, len(drills))
	for i, d := range drills {
		byID[d.ID] = i
	}
	for _, d := range extra {
		if i, ok := byID[d.ID]; ok {
			drills[i] = d
			continue
		}
		byID[d.ID] = len(drills)
		drills = append(drills, d)
	}
	return NewSet(drills)
}

// Combined is the deck the CLI practises from: the builtin drills with your
// own laid over them. It also reports how many of your drills were loaded.
func Combined() (*Set, int, error) {
	set, err := Builtin()
	if err != nil {
		return nil, 0, err
	}
	dir, err := UserDir()
	if err != nil {
		return set, 0, nil
	}
	mine, err := LoadUserDir(dir)
	if err != nil {
		return nil, 0, fmt.Errorf("%w\n  fix the file, or run with -builtin to skip your drills", err)
	}
	if len(mine) == 0 {
		return set, 0, nil
	}
	merged, err := Merge(set, mine)
	if err != nil {
		return nil, 0, fmt.Errorf("in %s: %w\n  fix the file, or run with -builtin to skip your drills", dir, err)
	}
	return merged, len(mine), nil
}
