package main

import (
	"fmt"
	"maps"
	"net/http"
	"os"
	"strings"
)

const ocrRegionKey = "TENCENTCLOUD_REGION"

type ocrCredentialStore interface {
	read() (map[string]string, error)
	write(map[string]string) error
}

func (a *server) reloadOCR() error {
	cfg, err := a.s.settings(nil)
	if err != nil {
		return err
	}
	values := map[string]string{ocrRegionKey: str(cfg, "ocr_region")}
	for key := range ocrEnvKeys {
		if key != ocrRegionKey {
			values[key] = a.ocrSecrets[key]
		}
		if a.configLocked[key] {
			values[key] = strings.TrimSpace(os.Getenv(key))
		}
	}
	a.s.ocr = newTencentOCR(values)
	return nil
}

func (a *server) configStatus() Record {
	values, present, locked := Record{}, Record{}, Record{}
	for k := range ocrEnvKeys {
		present[k] = a.ocrSecrets[k] != ""
		if a.configLocked[k] {
			present[k] = strings.TrimSpace(os.Getenv(k)) != ""
		}
		locked[k] = a.configLocked[k]
	}
	if a.s.ocr != nil {
		values[ocrRegionKey] = a.s.ocr.region
		present[ocrRegionKey] = a.s.ocr.region != ""
	}
	return Record{"values": values, "configured": present, "locked": locked, "editable": a.credentials != nil}
}

func (a *server) saveConfig(input Record) error {
	if a.credentials == nil {
		return fmt.Errorf("此環境無法保存 Windows OCR 憑據")
	}
	updates, _ := input["values"].(map[string]any)
	if record, ok := input["values"].(Record); ok {
		updates = record
	}
	clean := map[string]string{}
	for k, v := range updates {
		if !ocrEnvKeys[k] {
			return fmt.Errorf("不支持的配置项")
		}
		value, ok := v.(string)
		if !ok || len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00\"'") {
			return fmt.Errorf("配置格式无效：%s", k)
		}
		value = strings.TrimSpace(value)
		// An empty password field means keep; clearing is an explicit action.
		if value == "" && k != ocrRegionKey {
			continue
		}
		clean[k] = value
	}
	for _, k := range texts(input, "clear") {
		if !ocrEnvKeys[k] || k == ocrRegionKey {
			return fmt.Errorf("清除配置项无效")
		}
		clean[k] = ""
	}
	for k := range clean {
		if a.configLocked[k] {
			return fmt.Errorf("%s 由系统环境变量管理，请在系统中修改", k)
		}
	}
	previous, err := a.credentials.read()
	if err != nil {
		return err
	}
	next := make(map[string]string)
	maps.Copy(next, previous)
	for k, value := range clean {
		if k == ocrRegionKey {
			continue
		}
		if value == "" {
			delete(next, k)
		} else {
			next[k] = value
		}
	}
	changed := !maps.Equal(previous, next)
	if changed {
		if err = a.credentials.write(next); err != nil {
			return err
		}
	}
	if region, ok := clean[ocrRegionKey]; ok {
		if _, err = a.s.settings(Record{"ocr_region": region}); err != nil {
			if changed {
				if rollback := a.credentials.write(previous); rollback != nil {
					return fmt.Errorf("區域保存失敗，且無法恢復原有 Windows OCR 憑據：%w", rollback)
				}
			}
			return fmt.Errorf("無法保存 OCR 服務區域：%w", err)
		}
	}
	a.ocrSecrets = next
	return a.reloadOCR()
}

func (a *server) configRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) { sendJSON(w, 200, a.configStatus()) })
	mux.HandleFunc("PUT /api/config", func(w http.ResponseWriter, r *http.Request) {
		v, e := decode(w, r)
		if e == nil {
			e = a.saveConfig(v)
		}
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, a.configStatus())
	})
}
