package internal_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Muxcore-Media/media-comics/internal"
)

func openTempStore(t *testing.T) (*internal.Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "comics.db")
	s, err := internal.OpenStore(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func TestStoreSeriesIssueRoundTrip(t *testing.T) {
	s, _ := openTempStore(t)
	ctx := t.Context()
	ser, err := s.AddSeries(ctx, internal.Series{Title: "One Piece", Publisher: "Shueisha", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	iss, err := s.AddIssue(ctx, internal.Issue{SeriesID: ser.ID, Number: "1", Title: "Romance Dawn", Year: 1997, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if iss.Number != "1" {
		t.Fatalf("%+v", iss)
	}
	listed, err := s.ListIssues(ctx, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatal("expected 1 issue")
	}
	if err := s.RemoveSeries(ctx, ser.ID); err != nil {
		t.Fatal(err)
	}
	issues, err := s.ListIssues(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatal("expected issues cleared")
	}
}

func TestStoreListMissingIssues(t *testing.T) {
	s, _ := openTempStore(t)
	ctx := t.Context()
	ser, err := s.AddSeries(ctx, internal.Series{Title: "Berserk", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	missing, err := s.AddIssue(ctx, internal.Issue{SeriesID: ser.ID, Number: "1", Title: "The Black Swordsman", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddIssue(ctx, internal.Issue{SeriesID: ser.ID, Number: "2", Title: "On disk", Monitored: true, Path: "/tmp/fake.cbz"}); err != nil {
		t.Fatal(err)
	}
	items, total, err := s.ListMissingIssues(ctx, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("total=%d items=%d", total, len(items))
	}
	found := false
	for _, item := range items {
		if item.IssueID == missing.ID {
			found = true
		}
	}
	if !found || items[0].SeriesName != "Berserk" {
		t.Fatalf("%+v", items)
	}
}

func TestStoreUpdateSeriesIssue(t *testing.T) {
	s, _ := openTempStore(t)
	ctx := t.Context()
	ser, err := s.AddSeries(ctx, internal.Series{Title: "One Piece", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	iss, err := s.AddIssue(ctx, internal.Issue{SeriesID: ser.ID, Number: "1", Title: "Romance Dawn", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	title := "One Piece Updated"
	pub := "Shueisha"
	mon := false
	year := int32(1998)
	path := "/data/one-piece/001.cbz"
	updSer, err := s.UpdateSeries(ctx, ser.ID, internal.SeriesUpdate{
		Title: &title, Publisher: &pub, Monitored: &mon,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updSer.Title != title || updSer.Publisher != pub || updSer.Monitored {
		t.Fatalf("%+v", updSer)
	}
	updIss, err := s.UpdateIssue(ctx, iss.ID, internal.IssueUpdate{
		Title: &title, Number: &iss.Number, Year: &year, Monitored: &mon, Path: &path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updIss.Year != year || updIss.Path != path || updIss.Monitored {
		t.Fatalf("%+v", updIss)
	}
}

func TestStoreRemoveIssue(t *testing.T) {
	s, _ := openTempStore(t)
	ctx := t.Context()
	ser, err := s.AddSeries(ctx, internal.Series{Title: "Delete Me", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	iss, err := s.AddIssue(ctx, internal.Issue{SeriesID: ser.ID, Number: "1", Title: "Gone", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveIssue(ctx, iss.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetIssue(ctx, iss.ID); err == nil {
		t.Fatal("expected issue removed")
	}
}

func TestStoreImportIssuePath(t *testing.T) {
	s, _ := openTempStore(t)
	ctx := t.Context()
	dir := t.TempDir()
	file := filepath.Join(dir, "001.cbz")
	if err := os.WriteFile(file, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	ser, err := s.AddSeries(ctx, internal.Series{Title: "Import", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	iss, err := s.AddIssue(ctx, internal.Issue{SeriesID: ser.ID, Number: "1", Title: "Wanted", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ImportIssuePath(ctx, iss.ID, file)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != file {
		t.Fatalf("%+v", got)
	}
}

func TestStorePersistsAcrossOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "comics.db")
	ctx := t.Context()
	s1, err := internal.OpenStore(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s1.AddSeries(ctx, internal.Series{Title: "Monster", Publisher: "Shogakukan", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s1.AddIssue(ctx, internal.Issue{SeriesID: ser.ID, Number: "1", Title: "Herr Dr. Tenma"}); err != nil {
		t.Fatal(err)
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := internal.OpenStore(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	series, err := s2.ListSeries(ctx, "monster")
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("series=%d", len(series))
	}
	issues, err := s2.ListIssues(ctx, series[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Title != "Herr Dr. Tenma" {
		t.Fatalf("%+v", issues)
	}
}
