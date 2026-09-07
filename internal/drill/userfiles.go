package drill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// SyncedFile is where drills pulled from another machine land when this
// machine has no file for them yet.
const SyncedFile = "synced.json"

// ApplyMerged rewrites the drill files in dir so they hold merged. A drill
// this machine already has is updated where it lives, so a file you organised
// by hand stays organised; anything new goes into SyncedFile.
func ApplyMerged(dir string, merged []Drill) (added, updated int, err error) {
	files, err := readUserFiles(dir)
	if err != nil {
		return 0, 0, err
	}
	homes := map[string][]place{}
	for _, f := range files {
		for i, d := range f.drills {
			homes[d.ID] = append(homes[d.ID], place{file: f, index: i})
		}
	}
	var newcomers []Drill
	dirty := map[string]bool{}
	for _, m := range merged {
		spots, ok := homes[m.ID]
		if !ok {
			newcomers = append(newcomers, m)
			continue
		}
		changed := false
		for _, p := range spots {
			if SameDrill(p.file.drills[p.index], m) {
				continue
			}
			p.file.drills[p.index] = m
			dirty[p.file.path] = true
			changed = true
		}
		if changed {
			updated++
		}
	}
	for _, f := range files {
		if dirty[f.path] {
			if err := writeDrillFile(f.path, f.drills); err != nil {
				return 0, 0, err
			}
		}
	}
	if len(newcomers) > 0 {
		path := filepath.Join(dir, SyncedFile)
		existing, err := readExisting(path)
		if err != nil {
			return 0, 0, err
		}
		if err := writeDrillFile(path, append(existing, newcomers...)); err != nil {
			return 0, 0, err
		}
		added = len(newcomers)
	}
	return added, updated, nil
}

// place is one copy of a drill: which file holds it and where in that file. A
// drill can have more than one if two files declare the same id.
type place struct {
	file  *userFile
	index int
}

type userFile struct {
	path   string
	drills []Drill
}

// readUserFiles reads every drill file in dir in name order. A missing
// directory is not an error: most machines have no drills of their own.
func readUserFiles(dir string) ([]*userFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	files := make([]*userFile, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		ds, err := readDrillFile(path)
		if err != nil {
			return nil, err
		}
		for i := range ds {
			ds[i].Source = SourceUser
		}
		files = append(files, &userFile{path: path, drills: ds})
	}
	return files, nil
}

func readExisting(path string) ([]Drill, error) {
	ds, err := readDrillFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return ds, err
}

// writeDrillFile replaces a drill file through a temp file, so an interrupted
// sync cannot leave you with half your drills.
func writeDrillFile(path string, ds []Drill) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if ds == nil {
		ds = []Drill{}
	}
	body, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(body, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
