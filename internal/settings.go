package internal

import (
	"fmt"
	"os"
	"strings"

	"github.com/Muxcore-Media/core/pkg/contracts"
)

func (m *Module) Settings() []contracts.SettingDef {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	return []contracts.SettingDef{
		{
			Key: "library_dir", Label: "Library directory", Type: contracts.SettingTypeString,
			Value: m.libraryDir, Description: "Root path for comics/manga library (scanned offline)", Group: "Library",
		},
		{
			Key: "data_dir", Label: "Data directory", Type: contracts.SettingTypeString,
			Value: m.dataDir, Description: "Directory for SQLite library database (comics.db)", Group: "Library",
		},
	}
}

func (m *Module) UpdateSetting(key, value string) error {
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	switch key {
	case "library_dir":
		value = strings.TrimSpace(value)
		if value == "" {
			return fmt.Errorf("library_dir cannot be empty")
		}
		info, err := os.Stat(value)
		if err != nil {
			if mkErr := os.MkdirAll(value, 0o700); mkErr != nil {
				return fmt.Errorf("library_dir: %w", err)
			}
		} else if !info.IsDir() {
			return fmt.Errorf("library_dir is not a directory: %s", value)
		}
		m.libraryDir = value
	case "data_dir":
		return fmt.Errorf("data_dir is set at startup (COMICS_DATA_DIR); restart to change")
	default:
		return fmt.Errorf("unknown setting %q", key)
	}
	return nil
}
