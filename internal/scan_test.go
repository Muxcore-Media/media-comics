package internal_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Muxcore-Media/media-comics/internal"
)

// TestScanLibraryRootFixtures walks local stub comic archives under testdata/library.
// Files are empty stubs (no paid metadata APIs).
func TestScanLibraryRootFixtures(t *testing.T) {
	root := filepath.Join("testdata", "library")
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("fixture library missing: %v", err)
	}

	s, _ := openTempStore(t)
	ctx := t.Context()
	res, err := s.ScanLibraryRoot(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesFound < 2 {
		t.Fatalf("expected >=2 comic stubs, found=%d imported=%d skipped=%d",
			res.FilesFound, res.FilesImported, res.FilesSkipped)
	}
	if res.FilesImported < 2 {
		t.Fatalf("expected imports, got %+v", res)
	}

	series, err := s.ListSeries(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(series) < 1 {
		t.Fatal("expected at least one series from fixtures")
	}
	var fixtureSeries *internal.Series
	for _, ser := range series {
		if ser.Title == "Fixture Series" {
			fixtureSeries = ser
			break
		}
	}
	if fixtureSeries == nil {
		t.Fatalf("Fixture Series not found: %+v", series)
	}
	issues, err := s.ListIssues(ctx, fixtureSeries.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) < 2 {
		t.Fatalf("expected cbz+pdf stubs, issues=%d", len(issues))
	}

	// Idempotent rescan should skip already-imported paths.
	res2, err := s.ScanLibraryRoot(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if res2.FilesImported != 0 {
		t.Fatalf("expected no new imports on rescan, got %+v", res2)
	}
	if res2.FilesSkipped < 2 {
		t.Fatalf("expected skips on rescan, got %+v", res2)
	}
}

func TestScanLibraryRootMissing(t *testing.T) {
	s, _ := openTempStore(t)
	_, err := s.ScanLibraryRoot(t.Context(), filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected error for missing root")
	}
}

func TestModuleInitScan(t *testing.T) {
	data := t.TempDir()
	lib := filepath.Join(data, "comics")
	src := filepath.Join("testdata", "library")
	if err := copyTree(src, lib); err != nil {
		t.Fatal(err)
	}

	m := internal.NewModule(internal.Config{
		DataDir:    data,
		LibraryDir: lib,
		GRPCAddr:   "127.0.0.1:0",
		HTTPAddr:   "127.0.0.1:0",
	})
	if err := m.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(t.Context()) })

	res, err := m.ScanLibrary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesImported < 2 {
		t.Fatalf("%+v", res)
	}
}

// TestScanConfiguredLibrary scans COMICS_DATA_DIR / COMICS_LIBRARY_DIR when RUN_LIBRARY_SCAN=1.
func TestScanConfiguredLibrary(t *testing.T) {
	if os.Getenv("RUN_LIBRARY_SCAN") != "1" {
		t.Skip("set RUN_LIBRARY_SCAN=1")
	}
	m := internal.NewModule(internal.Config{
		GRPCAddr: "127.0.0.1:0",
		HTTPAddr: "127.0.0.1:0",
	})
	if err := m.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(t.Context()) })
	res, err := m.ScanLibrary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesFound == 0 {
		t.Fatalf("no files found in library root: %+v", res)
	}
	t.Logf("scan: %+v", res)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
