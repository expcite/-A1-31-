package evidence

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// WriteJSONAtomic publishes a complete public marker in its own directory.
// File Write/Sync/Close and Rename errors are returned. Directory durability
// across power loss is platform-dependent and is not claimed by this helper.
func WriteJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".edgehybrid-public-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	err = publishFile(f, append(b, '\n'))
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		err = errors.Join(err, os.Remove(name))
	}
	return err
}
