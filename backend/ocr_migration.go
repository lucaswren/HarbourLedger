package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (a *server) migrateLegacyOCR(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return fmt.Errorf("舊設定須為小於 1MB 的普通檔案")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	legacy, err := parseEnv(strings.NewReader(string(raw)))
	if err != nil || len(legacy) == 0 {
		return err
	}
	cfg, err := a.s.settings(nil)
	if err != nil {
		return err
	}
	updates := Record{}
	for key, value := range legacy {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if a.configLocked[key] {
			return fmt.Errorf("舊檔案包含由環境變數管理的 OCR 設定，已保留原檔")
		}
		current, exists := a.ocrSecrets[key]
		if key == ocrRegionKey {
			current = str(cfg, "ocr_region")
			_, exists = cfg["ocr_region"]
		}
		if exists && current != value {
			return fmt.Errorf("舊檔案與現有 OCR 設定有衝突，已保留原檔")
		}
		if !exists {
			updates[key] = value
		}
	}
	if err := a.saveConfig(Record{"values": updates}); err != nil {
		return err
	}

	// Remove only the migrated OCR entries. Preserve settings owned by other tools.
	lines := strings.Split(strings.TrimPrefix(string(raw), "\ufeff"), "\n")
	kept := make([]string, 0, len(lines))
	hasOtherSetting := false
	for _, line := range lines {
		text := strings.TrimSpace(line)
		key, _, assignment := strings.Cut(strings.TrimPrefix(text, "export "), "=")
		if assignment && ocrEnvKeys[strings.TrimSpace(key)] {
			continue
		}
		if text != "" && !strings.HasPrefix(text, "#") {
			hasOtherSetting = true
		}
		kept = append(kept, line)
	}
	if !hasOtherSetting {
		return os.Remove(path)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.WriteString(strings.Join(kept, "\n"))
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), path)
}
