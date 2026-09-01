package internal_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Muxcore-Media/media-comics/internal"
)

func TestUpdateSettingLibraryDir(t *testing.T) {
	data := t.TempDir()
	m := internal.NewModule(internal.Config{DataDir: data, LibraryDir: data})
	if err := m.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(t.Context()) })

	newDir := filepath.Join(data, "new-lib")
	if err := m.UpdateSetting("library_dir", newDir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(newDir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory created")
	}
	if err := m.UpdateSetting("library_dir", ""); err == nil {
		t.Fatal("expected error for empty library_dir")
	}
}

func TestNewModuleDefaultsLibraryToDataDir(t *testing.T) {
	data := t.TempDir()
	m := internal.NewModule(internal.Config{DataDir: data})
	defs := m.Settings()
	for _, d := range defs {
		if d.Key == "library_dir" && d.Value != data {
			t.Fatalf("library_dir=%q want %q", d.Value, data)
		}
	}
}
