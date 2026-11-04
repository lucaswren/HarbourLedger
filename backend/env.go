package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
)

var ocrEnvKeys = map[string]bool{

	"TENCENTCLOUD_SECRET_ID":  true,
	"TENCENTCLOUD_SECRET_KEY": true,
	"TENCENTCLOUD_TOKEN":      true,
	"TENCENTCLOUD_REGION":     true,
}

// Values are literal: no shell execution, variable expansion or escape handling.
// Errors never include the source line or a credential value.
func parseEnv(r io.Reader) (map[string]string, error) {
	values := map[string]string{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if lineNo == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.IndexFunc(key, func(r rune) bool {
			return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_' || r >= '0' && r <= '9')
		}) >= 0 || key[0] >= '0' && key[0] <= '9' {
			return nil, fmt.Errorf("env配置第%d行格式无效，应为KEY=VALUE", lineNo)
		}
		value = strings.TrimSpace(value)
		if len(value) > 0 && (value[0] == '\'' || value[0] == '"') {
			end := strings.IndexByte(value[1:], value[0])
			if end < 0 {
				return nil, fmt.Errorf("env配置第%d行引号未闭合", lineNo)
			}
			tail := strings.TrimSpace(value[end+2:])
			if tail != "" && !strings.HasPrefix(tail, "#") {
				return nil, fmt.Errorf("env配置第%d行引号后内容无效", lineNo)
			}
			value = value[1 : end+1]
		} else {
			var previous rune
			for pos, c := range value {
				if c == '#' && (pos == 0 || unicode.IsSpace(previous)) {
					value = strings.TrimSpace(value[:pos])
					break
				}
				previous = c
			}
		}
		if strings.ContainsRune(value, 0) {
			return nil, fmt.Errorf("env配置第%d行包含无效字符", lineNo)
		}
		if ocrEnvKeys[key] {
			values[key] = value
		}
	}
	if scanner.Err() != nil {
		return nil, fmt.Errorf("env配置读取失败或单行超过64KB")
	}
	return values, nil
}

func loadEnvFile(path string, optional bool) error {
	if path == "" {
		return nil
	}
	f, e := os.Open(path)
	if os.IsNotExist(e) && optional {
		return nil
	}
	if e != nil {
		return fmt.Errorf("无法打开env配置文件：%w", e)
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return fmt.Errorf("无法读取env配置文件信息")
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return fmt.Errorf("env配置文件必须是小于1MB的普通文件")
	}
	values, e := parseEnv(f)
	if e != nil {
		return e
	}
	// Parse everything before changing the process environment.
	for key, value := range values {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if e = os.Setenv(key, value); e != nil {
			return fmt.Errorf("无法设置OCR环境变量")
		}
	}
	return nil
}
