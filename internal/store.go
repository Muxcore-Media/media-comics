package internal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type Series struct {
	ID          string
	Title       string
	Publisher   string
	ComicVineID string
	Path        string
	Monitored   bool
}

type Issue struct {
	ID        string
	SeriesID  string
	Title     string
	Number    string
	Path      string
	Year      int32
	Monitored bool
}

// Store persists the comics library in SQLite.
type Store struct {
	db *sql.DB
}

// OpenStore opens or creates the SQLite database at path (WAL mode).
func OpenStore(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS series (
			id            TEXT PRIMARY KEY,
			title         TEXT NOT NULL,
			publisher     TEXT NOT NULL DEFAULT '',
			comicvine_id  TEXT NOT NULL DEFAULT '',
			monitored     INTEGER NOT NULL DEFAULT 1,
			path          TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS issues (
			id         TEXT PRIMARY KEY,
			series_id  TEXT NOT NULL,
			title      TEXT NOT NULL DEFAULT '',
			number     TEXT NOT NULL DEFAULT '',
			year       INTEGER NOT NULL DEFAULT 0,
			monitored  INTEGER NOT NULL DEFAULT 1,
			path       TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (series_id) REFERENCES series(id) ON DELETE CASCADE
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_issues_path ON issues(path) WHERE path != '';
		CREATE INDEX IF NOT EXISTS idx_series_title ON series(title);
		CREATE INDEX IF NOT EXISTS idx_issues_series ON issues(series_id);
	`)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Ping verifies the SQLite connection.
func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store not open")
	}
	return s.db.QueryRowContext(ctx, `SELECT 1`).Scan(new(int))
}

func (s *Store) AddSeries(ctx context.Context, ser Series) (*Series, error) {
	if strings.TrimSpace(ser.Title) == "" {
		return nil, fmt.Errorf("series title required")
	}
	if ser.ID == "" {
		ser.ID = "cs_" + uuid.NewString()[:8]
	}
	monitored := 0
	if ser.Monitored {
		monitored = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO series (id, title, publisher, comicvine_id, monitored, path)
		VALUES (?, ?, ?, ?, ?, ?)
	`, ser.ID, ser.Title, ser.Publisher, ser.ComicVineID, monitored, ser.Path)
	if err != nil {
		return nil, fmt.Errorf("insert series: %w", err)
	}
	out := ser
	return &out, nil
}

func (s *Store) GetSeries(ctx context.Context, id string) (*Series, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, publisher, comicvine_id, monitored, path FROM series WHERE id = ?
	`, id)
	return scanSeries(row)
}

func (s *Store) ListSeries(ctx context.Context, query string) ([]*Series, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, publisher, comicvine_id, monitored, path FROM series ORDER BY title
	`)
	if err != nil {
		return nil, fmt.Errorf("list series: %w", err)
	}
	defer func() { _ = rows.Close() }()
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]*Series, 0)
	for rows.Next() {
		ser, err := scanSeries(rows)
		if err != nil {
			return nil, err
		}
		if q != "" && !strings.Contains(strings.ToLower(ser.Title), q) {
			continue
		}
		out = append(out, ser)
	}
	return out, rows.Err()
}

func (s *Store) RemoveSeries(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM series WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete series: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("series %q not found", id)
	}
	// Cascades may be off without PRAGMA foreign_keys; clean children explicitly.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM issues WHERE series_id = ?`, id)
	return nil
}

type SeriesUpdate struct {
	Title     *string
	Publisher *string
	Monitored *bool
	Path      *string
}

type IssueUpdate struct {
	Title     *string
	Number    *string
	Year      *int32
	Monitored *bool
	Path      *string
}

func (s *Store) UpdateSeries(ctx context.Context, id string, upd SeriesUpdate) (*Series, error) {
	cur, err := s.GetSeries(ctx, id)
	if err != nil {
		return nil, err
	}
	if upd.Title != nil {
		if strings.TrimSpace(*upd.Title) == "" {
			return nil, fmt.Errorf("series title required")
		}
		cur.Title = *upd.Title
	}
	if upd.Publisher != nil {
		cur.Publisher = *upd.Publisher
	}
	if upd.Monitored != nil {
		cur.Monitored = *upd.Monitored
	}
	if upd.Path != nil {
		cur.Path = *upd.Path
	}
	monitored := 0
	if cur.Monitored {
		monitored = 1
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE series SET title = ?, publisher = ?, monitored = ?, path = ?
		WHERE id = ?
	`, cur.Title, cur.Publisher, monitored, cur.Path, id)
	if err != nil {
		return nil, fmt.Errorf("update series: %w", err)
	}
	return cur, nil
}

func (s *Store) GetIssue(ctx context.Context, id string) (*Issue, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, series_id, title, number, year, monitored, path FROM issues WHERE id = ?
	`, id)
	return scanIssue(row)
}

func (s *Store) UpdateIssue(ctx context.Context, id string, upd IssueUpdate) (*Issue, error) {
	cur, err := s.GetIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	if upd.Title != nil {
		cur.Title = *upd.Title
	}
	if upd.Number != nil {
		cur.Number = *upd.Number
	}
	if upd.Year != nil {
		cur.Year = *upd.Year
	}
	if upd.Monitored != nil {
		cur.Monitored = *upd.Monitored
	}
	if upd.Path != nil {
		cur.Path = *upd.Path
	}
	monitored := 0
	if cur.Monitored {
		monitored = 1
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE issues SET title = ?, number = ?, year = ?, monitored = ?, path = ?
		WHERE id = ?
	`, cur.Title, cur.Number, cur.Year, monitored, cur.Path, id)
	if err != nil {
		return nil, fmt.Errorf("update issue: %w", err)
	}
	return cur, nil
}

func (s *Store) RemoveIssue(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM issues WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete issue: %w", err)
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

func (s *Store) ImportIssuePath(ctx context.Context, issueID, absPath string) (*Issue, error) {
	iss, err := s.GetIssue(ctx, issueID)
	if err != nil {
		return nil, err
	}
	if issueHasFile(iss.Path) {
		return nil, fmt.Errorf("issue %q already has a file", issueID)
	}
	existing, err := s.findIssueByPath(ctx, absPath)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.ID != issueID {
		return nil, fmt.Errorf("path already attached to issue %q", existing.ID)
	}
	path := absPath
	return s.UpdateIssue(ctx, issueID, IssueUpdate{Path: &path})
}

func (s *Store) ListSeriesIssuePaths(ctx context.Context, seriesID string) ([]string, error) {
	issues, err := s.ListIssues(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(issues))
	for _, iss := range issues {
		if iss.Path != "" {
			paths = append(paths, iss.Path)
		}
	}
	return paths, nil
}

func (s *Store) AddIssue(ctx context.Context, iss Issue) (*Issue, error) {
	if strings.TrimSpace(iss.Number) == "" && strings.TrimSpace(iss.Title) == "" {
		return nil, fmt.Errorf("issue number or title required")
	}
	var exists string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM series WHERE id = ?`, iss.SeriesID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("series %q not found", iss.SeriesID)
	}
	if err != nil {
		return nil, fmt.Errorf("lookup series: %w", err)
	}
	if iss.ID == "" {
		iss.ID = "ci_" + uuid.NewString()[:8]
	}
	monitored := 0
	if iss.Monitored {
		monitored = 1
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO issues (id, series_id, title, number, year, monitored, path)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, iss.ID, iss.SeriesID, iss.Title, iss.Number, iss.Year, monitored, iss.Path)
	if err != nil {
		return nil, fmt.Errorf("insert issue: %w", err)
	}
	out := iss
	return &out, nil
}

func (s *Store) ListIssues(ctx context.Context, seriesID string) ([]*Issue, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if seriesID != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, series_id, title, number, year, monitored, path
			FROM issues WHERE series_id = ? ORDER BY number, title
		`, seriesID)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, series_id, title, number, year, monitored, path
			FROM issues ORDER BY title
		`)
	}
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]*Issue, 0)
	for rows.Next() {
		iss, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, iss)
	}
	return out, rows.Err()
}

// MissingIssue is a monitored issue with no file on disk.
type MissingIssue struct {
	IssueID    string `json:"issue_id"`
	SeriesID   string `json:"series_id"`
	Title      string `json:"title"`
	Number     string `json:"number"`
	SeriesName string `json:"series_name"`
	Year       int32  `json:"year"`
}

// ListMissingIssues returns monitored issues with no file on disk.
func (s *Store) ListMissingIssues(ctx context.Context, page, pageSize int) ([]MissingIssue, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.series_id, i.title, i.number, i.year, s.title, i.path
		FROM issues i
		JOIN series s ON s.id = i.series_id
		WHERE i.monitored = 1 AND s.monitored = 1
		ORDER BY s.title, i.number, i.title
	`)
	if err != nil {
		return nil, 0, fmt.Errorf("list missing issues: %w", err)
	}
	defer func() { _ = rows.Close() }()
	all := make([]MissingIssue, 0)
	for rows.Next() {
		var item MissingIssue
		var path string
		if err := rows.Scan(&item.IssueID, &item.SeriesID, &item.Title, &item.Number, &item.Year, &item.SeriesName, &path); err != nil {
			return nil, 0, err
		}
		if issueHasFile(path) {
			continue
		}
		all = append(all, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	total := len(all)
	offset := (page - 1) * pageSize
	if offset >= total {
		return []MissingIssue{}, total, nil
	}
	end := offset + pageSize
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

func (s *Store) findSeriesByTitle(ctx context.Context, title string) (*Series, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, publisher, comicvine_id, monitored, path FROM series
		WHERE lower(title) = lower(?) LIMIT 1
	`, title)
	ser, err := scanSeries(row)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, err
	}
	return ser, nil
}

func (s *Store) findIssueByPath(ctx context.Context, path string) (*Issue, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, series_id, title, number, year, monitored, path FROM issues WHERE path = ?
	`, path)
	iss, err := scanIssue(row)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, err
	}
	return iss, nil
}

func (s *Store) upsertIssue(ctx context.Context, iss Issue) (*Issue, error) {
	if iss.Path != "" {
		existing, err := s.findIssueByPath(ctx, iss.Path)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			monitored := 0
			if iss.Monitored {
				monitored = 1
			}
			_, err := s.db.ExecContext(ctx, `
				UPDATE issues SET series_id = ?, title = ?, number = ?, year = ?, monitored = ?
				WHERE id = ?
			`, iss.SeriesID, iss.Title, iss.Number, iss.Year, monitored, existing.ID)
			if err != nil {
				return nil, fmt.Errorf("update issue: %w", err)
			}
			existing.SeriesID = iss.SeriesID
			existing.Title = iss.Title
			existing.Number = iss.Number
			existing.Year = iss.Year
			existing.Monitored = iss.Monitored
			return existing, nil
		}
	}
	return s.AddIssue(ctx, iss)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSeries(row rowScanner) (*Series, error) {
	var ser Series
	var monitored int
	if err := row.Scan(&ser.ID, &ser.Title, &ser.Publisher, &ser.ComicVineID, &monitored, &ser.Path); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("series not found")
		}
		return nil, err
	}
	ser.Monitored = monitored != 0
	return &ser, nil
}

func scanIssue(row rowScanner) (*Issue, error) {
	var iss Issue
	var monitored int
	if err := row.Scan(&iss.ID, &iss.SeriesID, &iss.Title, &iss.Number, &iss.Year, &monitored, &iss.Path); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("issue not found")
		}
		return nil, err
	}
	iss.Monitored = monitored != 0
	return &iss, nil
}
