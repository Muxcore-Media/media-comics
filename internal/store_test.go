package internal_test

import (
	"testing"

	"github.com/Muxcore-Media/media-comics/internal"
)

func TestStoreSeriesIssueRoundTrip(t *testing.T) {
	s := internal.NewStore()
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
	if len(s.ListIssues(ser.ID)) != 1 {
		t.Fatal("expected 1 issue")
	}
	if err := s.RemoveSeries(ser.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.ListIssues("")) != 0 {
		t.Fatal("expected issues cleared")
	}
}
