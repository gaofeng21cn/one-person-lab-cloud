package ownerstore

import (
	"fmt"
	"io/fs"
)

// filesystemSource adapts an fs.FS (the service's embedded migration directory)
// onto MigrationSource. The two differ only in the directory-entry type, and
// converting here keeps every owner from writing the same six lines.
type filesystemSource struct{ fsys fs.FS }

// SourceFromFS reads migrations from an embedded or on-disk filesystem. A service
// hands its own migration directory in, so the SQL still belongs to the owner that
// defines it.
func SourceFromFS(fsys fs.FS) (MigrationSource, error) {
	if fsys == nil {
		return nil, fmt.Errorf("ownerstore: migration filesystem is required")
	}
	return filesystemSource{fsys: fsys}, nil
}

func (s filesystemSource) ReadDir(name string) ([]DirEntry, error) {
	entries, err := fs.ReadDir(s.fsys, name)
	if err != nil {
		return nil, err
	}
	converted := make([]DirEntry, 0, len(entries))
	for _, entry := range entries {
		converted = append(converted, filesystemEntry{entry})
	}
	return converted, nil
}

func (s filesystemSource) ReadFile(name string) ([]byte, error) {
	return fs.ReadFile(s.fsys, name)
}

type filesystemEntry struct{ fs.DirEntry }

func (e filesystemEntry) Name() string { return e.DirEntry.Name() }

// IsDir reports whether this entry is a directory. fs.DirEntry already provides
// IsDir, but the embedded SQL set is read from the directory itself, so the
// explicit method documents that no other entry kind is expected.
func (e filesystemEntry) IsDir() bool { return e.DirEntry.IsDir() }
