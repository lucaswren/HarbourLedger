package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func archiveYear(kind string, m Record) string {
	date := str(m, "date")
	if kind == "银行" {
		date = str(m, "statement_date")
		if !validDate(date) {
			for _, c := range arr(m, "checks") {
				if p := str(c, "period"); len(p) == 7 {
					if _, e := time.Parse("2006-01", p); e == nil {
						return p[:4]
					}
				}
			}
		}
	}
	if validDate(date) {
		return date[:4]
	}
	return "待核對日期"
}

func archiveCategory(kind string) string {
	switch kind {
	case "银行":
		return "銀行月結單"
	case "发票":
		return "採購憑證"
	case "销售发票":
		return "銷售憑證"
	default:
		return "其他憑證"
	}
}

// Originals are stored by calendar year; export periods remain independent.
func (s *Store) archivePeriod(kind string, m Record) string { return archiveYear(kind, m) }

// Paths use document dates, not upload dates. Same-name originals never overwrite.
func (s *Store) archivePath(kind string, m Record, sha string, reserved map[string]bool) (string, error) {
	dir := ""
	for _, part := range []string{"憑證", s.archivePeriod(kind, m), archiveCategory(kind)} {
		dir = filepath.Join(dir, part)
		if _, e := withinRoot(s.root, dir); e != nil {
			if !os.IsNotExist(e) {
				return "", e
			}
			if e = os.Mkdir(filepath.Join(s.root, dir), 0755); e != nil && !os.IsExist(e) {
				return "", e
			}
		}
		if _, e := withinRoot(s.root, dir); e != nil {
			return "", e
		}
	}
	name := contentName(kind, m, sha)
	if strings.HasPrefix(name, "待核验_") {
		name = "待核验" + safeName(kind) + ".pdf"
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	for n := 1; ; n++ {
		name = stem + ".pdf"
		if n > 1 {
			name = fmt.Sprintf("%s（版本%d）.pdf", stem, n)
		}
		rel := filepath.Join(dir, name)
		key := strings.ToLower(rel)
		if reserved[key] {
			continue
		}
		target := filepath.Join(s.root, rel)
		if info, e := os.Lstat(target); e == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("归档目标含符号链接")
		}
		b, e := os.ReadFile(target)
		if e != nil && !os.IsNotExist(e) {
			return "", e
		}
		if os.IsNotExist(e) || hashBytes(b) == sha {
			if reserved != nil {
				reserved[key] = true
			}
			return rel, nil
		}
	}
}

func defaultPeriod(d Data, now time.Time) (string, string) {
	local := now.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	end := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, local.Location()).AddDate(0, -1, 0).Format("2006-01")
	start := end
	for _, c := range d.Checks {
		p := str(c, "period")
		if parsed, e := time.Parse("2006-01", p); e == nil && parsed.Format("2006-01") == p && p < start {
			start = p
		}
	}
	return start, end
}

func (s *Store) periodSettings(now time.Time) (Record, error) {
	v, e := s.settings(nil)
	if e != nil {
		return nil, e
	}
	if str(v, "mode") == "" || str(v, "mode") == "全部至今" || str(v, "mode") == "全部至上月" {
		d, e := s.load()
		if e != nil {
			return nil, e
		}
		start, end := defaultPeriod(d, now)
		v["start"] = start
		v["end"] = end
		v["mode"] = "全部至上月"
	}
	return v, nil
}

func canonicalMetadata(m Record, name string) {
	m["source"] = name
	delete(m, "original_name")
	delete(m, "original_filename")
	for _, key := range []string{"tx", "checks"} {
		for _, r := range arr(m, key) {
			r["source"] = name
			delete(r, "original_name")
			delete(r, "original_filename")
		}
	}
}
