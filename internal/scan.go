package internal

import (
	"context"
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
	PathsCleared  int
}

// ScanLibraryRoot walks root for comic archives and upserts series/issues.
// Layout expected: Series/issue.ext (or Series/subdir/issue.ext — series is the
// first path segment under root). Metadata is derived only from path/filename —
// no network lookups. Issues whose files disappeared since the last scan have
// their path cleared so they surface as missing.
func (s *Store) ScanLibraryRoot(ctx context.Context, root string) (*ScanResult, error) {
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
	foundPaths := make(map[string]struct{})
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
		absPath, absErr := filepath.Abs(path)
		if absErr != nil {
			return absErr
		}
		foundPaths[absPath] = struct{}{}
		res.FilesFound++
		seriesTitle, number, issueTitle := inferComicFromPath(root, absPath)
		imported, importErr := s.importComicFile(ctx, seriesTitle, number, issueTitle, absPath)
		if importErr != nil {
			return importErr
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
	cleared, err := s.reconcileMissingPaths(ctx, foundPaths)
	if err != nil {
		return nil, err
	}
	res.PathsCleared = cleared
	return res, nil
}

func (s *Store) reconcileMissingPaths(ctx context.Context, foundPaths map[string]struct{}) (int, error) {
	issues, err := s.ListIssues(ctx, "")
	if err != nil {
		return 0, err
	}
	cleared := 0
	for _, iss := range issues {
		if iss.Path == "" {
			continue
		}
		if _, ok := foundPaths[iss.Path]; ok {
			continue
		}
		if issueHasFile(iss.Path) {
			continue
		}
		if err := s.clearIssuePath(ctx, iss.ID); err != nil {
			return cleared, err
		}
		cleared++
	}
	return cleared, nil
}

func (s *Store) clearIssuePath(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE issues SET path = '' WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("clear issue path: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("issue %q not found", id)
	}
	return nil
}

func (s *Store) importComicFile(ctx context.Context, seriesTitle, number, issueTitle, absPath string) (imported bool, err error) {
	existing, err := s.findIssueByPath(ctx, absPath)
	if err != nil {
		return false, err
	}

	ser, err := s.findSeriesByTitle(ctx, seriesTitle)
	if err != nil {
		return false, err
	}
	if ser == nil {
		ser, err = s.AddSeries(ctx, Series{
			Title:     seriesTitle,
			Monitored: true,
			Path:      filepath.Dir(absPath),
		})
		if err != nil {
			return false, err
		}
	}

	_, err = s.upsertIssue(ctx, Issue{
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
