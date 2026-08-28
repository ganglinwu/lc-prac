package drill

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
)

//go:embed data/*.json
var builtinFS embed.FS

// Builtin returns the drill set compiled into the binary, so the CLI works
// with no config and no network.
func Builtin() (*Set, error) {
	return LoadFS(builtinFS, "data")
}

// LoadFS reads every *.json file in dir as an array of drills. Files are read
// in name order so the resulting set is deterministic.
func LoadFS(fsys fs.FS, dir string) (*Set, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read drill dir %q: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && path.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var all []Drill
	for _, name := range names {
		b, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		var batch []Drill
		if err := json.Unmarshal(b, &batch); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		for i := range batch {
			batch[i].Source = SourceBuiltin
		}
		all = append(all, batch...)
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no drills found in %q", dir)
	}
	return NewSet(all)
}

// readDrillFile reads one JSON array of drills, naming the file in any error
// so an authoring mistake points at itself.
func readDrillFile(file string) ([]Drill, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var ds []Drill
	if err := json.Unmarshal(b, &ds); err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	return ds, nil
}
