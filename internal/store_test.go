package internal_test

import (
	"path/filepath"
	"testing"

	"github.com/Muxcore-Media/media-comics/internal"
)

func openTempStore(t *testing.T) (*internal.Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "comics.db")
	s, err := internal.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func TestStoreSeriesIssueRoundTrip(t *testing.T) {
	s, _ := openTempStore(t)
	ser, err := s.AddSeries(internal.Series{Title: "One Piece", Publisher: "Shueisha", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	iss, err := s.AddIssue(internal.Issue{SeriesID: ser.ID, Number: "1", Title: "Romance Dawn", Year: 1997, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if iss.Number != "1" {
		t.Fatalf("%+v", iss)
	}
	listed, err := s.ListIssues(ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatal("expected 1 issue")
	}
	if err := s.RemoveSeries(ser.ID); err != nil {
		t.Fatal(err)
	}
	issues, err := s.ListIssues("")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatal("expected issues cleared")
	}
}

func TestStorePersistsAcrossOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "comics.db")
	s1, err := internal.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s1.AddSeries(internal.Series{Title: "Monster", Publisher: "Shogakukan", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s1.AddIssue(internal.Issue{SeriesID: ser.ID, Number: "1", Title: "Herr Dr. Tenma"}); err != nil {
		t.Fatal(err)
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := internal.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	series, err := s2.ListSeries("monster")
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("series=%d", len(series))
	}
	issues, err := s2.ListIssues(series[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Title != "Herr Dr. Tenma" {
		t.Fatalf("%+v", issues)
	}
}
