package internal

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// comic archive / document extensions recognized during library scans
// (local files only; no metadata APIs).
var comicExts = map[string]struct{}{
	".cbz":  {},
	".cbr":  {},
	".cb7":  {},
	".cbt":  {},
	".pdf":  {},
	".epub": {},
}

// ScanResult summarizes a library root scan.
type ScanResult struct {
	FilesFound    int
	FilesImported int
	FilesSkipped  int
}

// ScanLibraryRoot walks root for comic archives and upserts series/issues.
// Layout expected: Series/issue.ext (or Series/subdir/issue.ext — series is the
// first path segment under root). Metadata is derived only from path/filename —
// no network lookups.
func (s *Store) ScanLibraryRoot(root string) (*ScanResult, error) {
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, fmt.Errorf("library root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("library root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("library root is not a directory: %s", root)
	}

	res := &ScanResult{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if _, ok := comicExts[ext]; !ok {
			return nil
		}
		res.FilesFound++
		abs, err := filepath.Abs(path)
		if err != nil {
			res.FilesSkipped++
			return nil
		}
		seriesTitle, number, issueTitle := inferComicFromPath(root, abs)
		imported, err := s.importComicFile(seriesTitle, number, issueTitle, abs)
		if err != nil {
			return err
		}
		if imported {
			res.FilesImported++
		} else {
			res.FilesSkipped++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Store) importComicFile(seriesTitle, number, issueTitle, absPath string) (imported bool, err error) {
	existing, err := s.findIssueByPath(absPath)
	if err != nil {
		return false, err
	}

	ser, err := s.findSeriesByTitle(seriesTitle)
	if err != nil {
		return false, err
	}
	if ser == nil {
		ser, err = s.AddSeries(Series{
			Title:     seriesTitle,
			Monitored: true,
			Path:      filepath.Dir(absPath),
		})
		if err != nil {
			return false, err
		}
	}

	_, err = s.upsertIssue(Issue{
		SeriesID:  ser.ID,
		Title:     issueTitle,
		Number:    number,
		Monitored: true,
		Path:      absPath,
	})
	if err != nil {
		return false, err
	}
	return existing == nil, nil
}

// inferComicFromPath derives series/number/title from Series/.../file.ext under root.
func inferComicFromPath(root, absPath string) (series, number, title string) {
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		rel = filepath.Base(absPath)
	}
	parts := strings.Split(rel, string(filepath.Separator))
	base := parts[len(parts)-1]
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	number, title = splitIssueStem(stem)
	if len(parts) == 1 {
		return "Unknown Series", number, title
	}
	return parts[0], number, title
}

// splitIssueStem parses "001 - Romance Dawn" or "01. Intro" into number + title.
func splitIssueStem(name string) (number, title string) {
	name = strings.TrimSpace(name)
	i := 0
	for i < len(name) && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	if i == 0 {
		return "", name
	}
	number = name[:i]
	rest := strings.TrimSpace(name[i:])
	if strings.HasPrefix(rest, "-") || strings.HasPrefix(rest, ".") || strings.HasPrefix(rest, "_") {
		rest = strings.TrimSpace(rest[1:])
	}
	if rest == "" {
		return number, number
	}
	return number, rest
}
