package internal_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/Muxcore-Media/media-comics/internal"
	comicsv1 "github.com/Muxcore-Media/media-comics/proto/gen/muxcore/comics/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func dialComics(t *testing.T, addr string) comicsv1.ComicManagementServiceClient {
	t.Helper()
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", addr)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return comicsv1.NewComicManagementServiceClient(conn)
}

func startComicsModule(t *testing.T) (*internal.Module, string, comicsv1.ComicManagementServiceClient) {
	t.Helper()
	data := t.TempDir()
	lib := copyFixtureLibrary(t, data)
	m := internal.NewModule(internal.Config{
		DataDir: data, LibraryDir: lib,
		GRPCAddr: "127.0.0.1:0", HTTPAddr: "127.0.0.1:0",
	})
	if err := m.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(t.Context()) })
	return m, lib, dialComics(t, m.GRPCListenAddr())
}

func TestGRPCSeriesIssueLifecycle(t *testing.T) {
	_, _, client := startComicsModule(t)
	ctx := t.Context()

	addSer, err := client.AddSeries(ctx, &comicsv1.AddSeriesRequest{
		Title: "Test Series", Publisher: "Test Pub", Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	serID := addSer.GetSeries().GetId()

	gotSer, err := client.GetSeries(ctx, &comicsv1.GetSeriesRequest{Id: serID})
	if err != nil {
		t.Fatal(err)
	}
	if gotSer.GetSeries().GetTitle() != "Test Series" {
		t.Fatalf("%+v", gotSer.GetSeries())
	}

	listSer, err := client.ListSeries(ctx, &comicsv1.ListSeriesRequest{Query: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(listSer.GetSeries()) < 1 {
		t.Fatal("expected series in list")
	}

	mon := false
	updSer, err := client.UpdateSeries(ctx, &comicsv1.UpdateSeriesRequest{
		Id: serID, Monitored: &mon,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updSer.GetSeries().GetMonitored() {
		t.Fatal("expected unmonitored series")
	}

	addIss, err := client.AddIssue(ctx, &comicsv1.AddIssueRequest{
		SeriesId: serID, Number: "1", Title: "First", Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	issID := addIss.GetIssue().GetId()

	listIss, err := client.ListIssues(ctx, &comicsv1.ListIssuesRequest{SeriesId: serID})
	if err != nil {
		t.Fatal(err)
	}
	if len(listIss.GetIssues()) != 1 {
		t.Fatalf("issues=%d", len(listIss.GetIssues()))
	}

	gotIss, err := client.GetIssue(ctx, &comicsv1.GetIssueRequest{Id: issID})
	if err != nil {
		t.Fatal(err)
	}
	if gotIss.GetIssue().GetNumber() != "1" {
		t.Fatalf("%+v", gotIss.GetIssue())
	}

	issMon := false
	updIss, err := client.UpdateIssue(ctx, &comicsv1.UpdateIssueRequest{
		Id: issID, Monitored: &issMon,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updIss.GetIssue().GetMonitored() {
		t.Fatal("expected unmonitored issue")
	}

	if _, err := client.RemoveIssue(ctx, &comicsv1.RemoveIssueRequest{Id: issID}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RemoveSeries(ctx, &comicsv1.RemoveSeriesRequest{Id: serID}); err != nil {
		t.Fatal(err)
	}
}

func TestGRPCScanListMissingImport(t *testing.T) {
	_, lib, client := startComicsModule(t)
	ctx := t.Context()

	scan, err := client.ScanLibrary(ctx, &comicsv1.ScanLibraryRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if scan.GetFilesFound() < 2 {
		t.Fatalf("scan=%+v", scan)
	}

	missingBefore, err := client.ListMissing(ctx, &comicsv1.ListMissingRequest{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}

	addSer, err := client.AddSeries(ctx, &comicsv1.AddSeriesRequest{Title: "Wanted", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	addIss, err := client.AddIssue(ctx, &comicsv1.AddIssueRequest{
		SeriesId: addSer.GetSeries().GetId(), Number: "99", Title: "Missing", Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	missingAfter, err := client.ListMissing(ctx, &comicsv1.ListMissingRequest{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if missingAfter.GetTotal() != missingBefore.GetTotal()+1 {
		t.Fatalf("before=%d after=%d", missingBefore.GetTotal(), missingAfter.GetTotal())
	}

	newFile := filepath.Join(lib, "Fixture Series", "099 - Imported.cbz")
	if err := os.WriteFile(newFile, []byte("imported"), 0o644); err != nil {
		t.Fatal(err)
	}
	imp, err := client.ImportIssue(ctx, &comicsv1.ImportIssueRequest{
		IssueId: addIss.GetIssue().GetId(), Path: newFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !imp.GetIssue().GetHasFile() {
		t.Fatalf("%+v", imp.GetIssue())
	}

	missingFinal, err := client.ListMissing(ctx, &comicsv1.ListMissingRequest{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if missingFinal.GetTotal() != missingBefore.GetTotal() {
		t.Fatalf("before=%d final=%d", missingBefore.GetTotal(), missingFinal.GetTotal())
	}

	rescan, err := client.ScanLibrary(ctx, &comicsv1.ScanLibraryRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if rescan.GetFilesImported() != 0 {
		t.Fatalf("rescan=%+v", rescan)
	}
}
