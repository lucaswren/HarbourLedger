package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var publicConfigKeys = map[string]bool{"TENCENTCLOUD_REGION": true}

func (a *server) configStatus() Record {
	values, present, locked := Record{}, Record{}, Record{}
	for k := range ocrEnvKeys {
		present[k] = os.Getenv(k) != ""
		locked[k] = a.configLocked[k]
		if publicConfigKeys[k] {
			values[k] = os.Getenv(k)
		}
	}
	return Record{"values": values, "configured": present, "locked": locked, "editable": a.envFile != ""}
}

func (a *server) saveConfig(input Record) error {
	if a.envFile == "" {
		return fmt.Errorf("本次启动已禁用配置文件，无法保存设置")
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
		if value == "" && !publicConfigKeys[k] {
			continue
		}
		clean[k] = value
	}
	for _, k := range texts(input, "clear") {
		if !ocrEnvKeys[k] || publicConfigKeys[k] {
			return fmt.Errorf("清除配置项无效")
		}
		clean[k] = ""
	}
	for k := range clean {
		if a.configLocked[k] {
			return fmt.Errorf("%s 由系统环境变量管理，请在系统中修改", k)
		}
	}
	path, e := filepath.Abs(a.envFile)
	if e != nil {
		return fmt.Errorf("配置路径无效")
	}
	original, e := os.ReadFile(path)
	if e != nil && !os.IsNotExist(e) {
		return fmt.Errorf("配置文件无法读取")
	}
	if len(original) > 1<<20 {
		return fmt.Errorf("配置文件过大")
	}
	if info, e := os.Lstat(path); e == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("配置文件须为普通文件")
	}
	if _, e := parseEnv(strings.NewReader(string(original))); e != nil {
		return e
	}
	lines := []string{}
	for _, line := range strings.Split(strings.TrimPrefix(string(original), "\ufeff"), "\n") {
		key, _, has := strings.Cut(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export ")), "=")
		if _, replace := clean[strings.TrimSpace(key)]; has && replace {
			continue
		}
		lines = append(lines, strings.TrimSuffix(line, "\r"))
	}
	keys := []string{}
	for k := range clean {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, k+"=\""+clean[k]+"\"")
	}
	tmp, e := os.CreateTemp(filepath.Dir(path), ".settings-*.tmp")
	if e != nil {
		return fmt.Errorf("配置目录不可写，请将客户端放在可写目录")
	}
	name := tmp.Name()
	defer os.Remove(name)
	if e = tmp.Chmod(0600); e == nil {
		_, e = tmp.WriteString(strings.Join(lines, "\n"))
	}
	if closeErr := tmp.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return fmt.Errorf("配置保存失败")
	}
	if e = os.Rename(name, path); e != nil {
		return fmt.Errorf("配置替换失败，请检查文件是否被占用")
	}
	for _, k := range keys {
		if e = os.Setenv(k, clean[k]); e != nil {
			return fmt.Errorf("配置已保存，请重新启动客户端")
		}
	}
	a.s.ocr = ocrFromEnv()
	return nil
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
