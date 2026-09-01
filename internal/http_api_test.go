package internal_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Muxcore-Media/media-comics/internal"
	comicsv1 "github.com/Muxcore-Media/media-comics/proto/gen/muxcore/comics/v1"
)

func TestHTTPListSeriesFixtures(t *testing.T) {
	data := t.TempDir()
	lib := copyFixtureLibrary(t, data)

	m := internal.NewModule(internal.Config{
		DataDir:    data,
		LibraryDir: lib,
		GRPCAddr:   "127.0.0.1:0",
		HTTPAddr:   "127.0.0.1:0",
	})
	if err := m.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(t.Context()) })

	hr, err := http.Get("http://" + m.HTTPListenAddr() + "/api/series")
	if err != nil {
		t.Fatal(err)
	}
	defer hr.Body.Close()
	if hr.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(hr.Body)
		t.Fatalf("status %d: %s", hr.StatusCode, b)
	}
	body, err := io.ReadAll(hr.Body)
	if err != nil {
		t.Fatal(err)
	}
	var series []map[string]any
	if err := json.Unmarshal(body, &series); err != nil {
		t.Fatal(err)
	}
	if len(series) < 1 {
		t.Fatal("expected fixture series via HTTP API")
	}

	missingResp, err := http.Get("http://" + m.HTTPListenAddr() + "/api/missing")
	if err != nil {
		t.Fatal(err)
	}
	defer missingResp.Body.Close()
	if missingResp.StatusCode != http.StatusOK {
		t.Fatalf("missing status %d", missingResp.StatusCode)
	}
	var missing struct {
		Total    int `json:"total"`
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
	}
	if err := json.NewDecoder(missingResp.Body).Decode(&missing); err != nil {
		t.Fatal(err)
	}
	if missing.Page != 1 || missing.PageSize != 100 {
		t.Fatalf("unexpected pagination: %+v", missing)
	}
}

func TestHTTPScanEndpoint(t *testing.T) {
	data := t.TempDir()
	lib := copyFixtureLibrary(t, data)
	m := startHTTPModule(t, data, lib)

	resp, err := http.Post("http://"+m.HTTPListenAddr()+"/api/scan", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}
	var scan map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&scan); err != nil {
		t.Fatal(err)
	}
	if scan["files_found"] < 2 {
		t.Fatalf("%+v", scan)
	}
}

func TestHTTPStreamIssue(t *testing.T) {
	data := t.TempDir()
	lib := copyFixtureLibrary(t, data)
	m := startHTTPModule(t, data, lib)
	base := "http://" + m.HTTPListenAddr()

	issuesResp, err := http.Get(base + "/api/issues")
	if err != nil {
		t.Fatal(err)
	}
	defer issuesResp.Body.Close()
	var issues []struct {
		ID      string `json:"id"`
		HasFile bool   `json:"has_file"`
	}
	if err := json.NewDecoder(issuesResp.Body).Decode(&issues); err != nil {
		t.Fatal(err)
	}
	var onDiskID string
	for _, iss := range issues {
		if iss.HasFile {
			onDiskID = iss.ID
			break
		}
	}
	if onDiskID == "" {
		t.Fatal("no on-disk issue from startup scan")
	}

	streamResp, err := http.Get(base + "/api/issues/" + onDiskID + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status %d", streamResp.StatusCode)
	}

	client := dialComics(t, m.GRPCListenAddr())
	outside := filepath.Join(data, "outside.cbz")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	ser, err := client.AddSeries(t.Context(), &comicsv1.AddSeriesRequest{Title: "Escape", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	iss, err := client.AddIssue(t.Context(), &comicsv1.AddIssueRequest{
		SeriesId: ser.GetSeries().GetId(), Number: "1", Title: "Outside", Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	outPath := outside
	if _, err := client.UpdateIssue(t.Context(), &comicsv1.UpdateIssueRequest{
		Id: iss.GetIssue().GetId(), Path: &outPath,
	}); err != nil {
		t.Fatal(err)
	}
	escapeResp, err := http.Get(base + "/api/issues/" + iss.GetIssue().GetId() + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer escapeResp.Body.Close()
	if escapeResp.StatusCode != http.StatusNotFound {
		t.Fatalf("escape stream status %d", escapeResp.StatusCode)
	}
}

func TestHTTPImportIssue(t *testing.T) {
	data := t.TempDir()
	lib := copyFixtureLibrary(t, data)
	m := internal.NewModule(internal.Config{
		DataDir: data, LibraryDir: lib, GRPCAddr: "127.0.0.1:0", HTTPAddr: "127.0.0.1:0",
	})
	if err := m.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(t.Context()) })

	client := dialComics(t, m.GRPCListenAddr())
	ctx := t.Context()
	ser, err := client.AddSeries(ctx, &comicsv1.AddSeriesRequest{Title: "HTTP Import", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	iss, err := client.AddIssue(ctx, &comicsv1.AddIssueRequest{
		SeriesId: ser.GetSeries().GetId(), Number: "99", Title: "Missing", Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(lib, "Fixture Series", "099 - Imported.cbz")
	if err := os.WriteFile(newFile, []byte("imported"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"path": newFile})
	resp, err := http.Post(
		"http://"+m.HTTPListenAddr()+"/api/issues/"+iss.GetIssue().GetId()+"/import",
		"application/json", bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}
	var out struct {
		HasFile bool `json:"has_file"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.HasFile {
		t.Fatal("expected imported file")
	}

	outside := filepath.Join(data, "outside.cbz")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	body2, _ := json.Marshal(map[string]string{"path": outside})
	badResp, err := http.Post(
		"http://"+m.HTTPListenAddr()+"/api/issues/"+iss.GetIssue().GetId()+"/import",
		"application/json", bytes.NewReader(body2),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("outside import status %d", badResp.StatusCode)
	}
}

func startHTTPModule(t *testing.T, data, lib string) *internal.Module {
	t.Helper()
	m := internal.NewModule(internal.Config{
		DataDir: data, LibraryDir: lib, GRPCAddr: "127.0.0.1:0", HTTPAddr: "127.0.0.1:0",
	})
	if err := m.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(t.Context()) })
	return m
}

func TestRemoveSeriesDeleteFiles(t *testing.T) {
	data := t.TempDir()
	lib := copyFixtureLibrary(t, data)
	m := internal.NewModule(internal.Config{
		DataDir: data, LibraryDir: lib, GRPCAddr: "127.0.0.1:0", HTTPAddr: "127.0.0.1:0",
	})
	if err := m.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(t.Context()) })

	client := dialComics(t, m.GRPCListenAddr())
	ctx := t.Context()
	list, err := client.ListSeries(ctx, &comicsv1.ListSeriesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.GetSeries()) == 0 {
		t.Fatal("expected scanned series")
	}
	serID := list.GetSeries()[0].GetId()
	issues, err := client.ListIssues(ctx, &comicsv1.ListIssuesRequest{SeriesId: serID})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues.GetIssues()) == 0 {
		t.Fatal("expected issues")
	}
	filePath := issues.GetIssues()[0].GetPath()
	if filePath == "" {
		t.Fatal("expected file path")
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatal(err)
	}

	if _, err := client.RemoveSeries(ctx, &comicsv1.RemoveSeriesRequest{Id: serID, DeleteFiles: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatal("expected file deleted")
	}

	outside := filepath.Join(data, "outside.cbz")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ser2, err := client.AddSeries(ctx, &comicsv1.AddSeriesRequest{Title: "Outside", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	outPath := outside
	iss2, err := client.AddIssue(ctx, &comicsv1.AddIssueRequest{SeriesId: ser2.GetSeries().GetId(), Number: "1", Title: "x", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.UpdateIssue(ctx, &comicsv1.UpdateIssueRequest{Id: iss2.GetIssue().GetId(), Path: &outPath}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RemoveSeries(ctx, &comicsv1.RemoveSeriesRequest{Id: ser2.GetSeries().GetId(), DeleteFiles: true}); err == nil {
		t.Fatal("expected refusal deleting outside library root")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("outside file must remain")
	}
}
