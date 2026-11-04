package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type server struct {
	s            *Store
	mu           sync.Mutex
	assets, font string
	envFile      string
	configLocked map[string]bool
	jobMu        sync.Mutex
	rebuildJob   Record
}

func sendJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, e error) { sendJSON(w, 400, Record{"error": traditional(e.Error())}) }
func decode(w http.ResponseWriter, r *http.Request) (Record, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	dec := json.NewDecoder(r.Body)
	var v Record
	if e := dec.Decode(&v); e != nil {
		return nil, fmt.Errorf("请求JSON无效: %w", e)
	}
	if v == nil {
		return nil, fmt.Errorf("请求不能为空")
	}
	if e := dec.Decode(new(any)); e != io.EOF {
		return nil, fmt.Errorf("请求只能包含一个JSON对象")
	}
	return v, nil
}
func (a *server) routes() http.Handler {
	mux := http.NewServeMux()
	a.restoreRoute(mux)
	a.configRoutes(mux)
	mux.HandleFunc("GET /api/rebuild/status", func(w http.ResponseWriter, r *http.Request) {
		a.jobMu.Lock()
		defer a.jobMu.Unlock()
		if a.rebuildJob == nil {
			sendJSON(w, 200, Record{"status": "idle"})
			return
		}
		sendJSON(w, 200, a.rebuildJob)
	})
	mux.HandleFunc("POST /api/rebuild", func(w http.ResponseWriter, r *http.Request) {
		v, e := decode(w, r)
		if e != nil {
			fail(w, e)
			return
		}
		if _, specified := v["use_cloud"]; !specified {
			v["use_cloud"] = a.s.ocr.configured()
		}
		a.jobMu.Lock()
		defer a.jobMu.Unlock()
		if a.rebuildJob != nil && (str(a.rebuildJob, "status") == "running" || str(a.rebuildJob, "status") == "queued") {
			sendJSON(w, 409, Record{"error": "已有账务梳理正在执行"})
			return
		}
		if yes(v, "use_cloud") && !a.s.ocr.configured() {
			fail(w, fmt.Errorf("腾讯云OCR未配置完整"))
			return
		}
		id := uuid.NewString()
		a.rebuildJob = Record{"id": id, "status": "queued", "done": 0, "total": 0, "name": "", "started_at": time.Now().Format(time.RFC3339)}
		go func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			defer func() {
				if recover() != nil {
					a.jobMu.Lock()
					defer a.jobMu.Unlock()
					a.rebuildJob["status"] = "failed"
					a.rebuildJob["error"] = "梳理遇到内部错误，请查看整理记录和备份"
				}
			}()
			a.jobMu.Lock()
			a.rebuildJob["status"] = "running"
			a.jobMu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			result, err := a.s.rebuildLedger(ctx, yes(v, "use_cloud"), func(done, total int, name string) {
				a.jobMu.Lock()
				defer a.jobMu.Unlock()
				a.rebuildJob["done"] = done
				a.rebuildJob["total"] = total
				a.rebuildJob["name"] = name
			})
			a.jobMu.Lock()
			defer a.jobMu.Unlock()
			a.rebuildJob["finished_at"] = time.Now().Format(time.RFC3339)
			if err != nil {
				a.rebuildJob["status"] = "failed"
				a.rebuildJob["error"] = err.Error()
			} else {
				a.rebuildJob["status"] = "completed"
				a.rebuildJob["result"] = result
			}
		}()
		sendJSON(w, 202, Record{"id": id, "status": "queued"})
	})
	mux.HandleFunc("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		v, e := a.s.accounts()
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, v)
	})
	mux.HandleFunc("POST /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		v, e := decode(w, r)
		if e != nil {
			fail(w, e)
			return
		}
		v, e = a.s.saveAccount(v)
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, v)
	})
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		sendJSON(w, 200, Record{"status": "ok", "version": "2.0.0", "backend": "Go", "ocr": a.s.ocr.status()})
	})
	mux.HandleFunc("GET /api/data", func(w http.ResponseWriter, r *http.Request) {
		d, e := a.s.load()
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, d)
	})
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		v, e := a.s.periodSettings(time.Now())
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, v)
	})
	mux.HandleFunc("GET /api/ocr/status", func(w http.ResponseWriter, r *http.Request) { sendJSON(w, 200, a.s.ocr.status()) })
	mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		v, e := decode(w, r)
		if e != nil {
			fail(w, e)
			return
		}
		allowed := Record{}
		for _, k := range []string{"ui_language", "company", "start", "end", "mode", "year", "month", "quarter", "report_start", "report_end", "subsequent_end", "company_aliases", "director_names", "sales_defaults", "auto_match_exact", "google_billing_id", "google_billing_currency"} {
			if s, ok := v[k].(string); ok {
				if k == "ui_language" && s != "en" && s != "zh-Hant" && s != "zh-Hans" {
					fail(w, fmt.Errorf("語言設定無效"))
					return
				}
				maxLength := 500
				if k == "auto_match_exact" && s != "true" && s != "false" {
					fail(w, fmt.Errorf("自動匹配設定無效"))
					return
				}
				if k == "google_billing_currency" && s != "USD" && s != "HKD" && s != "" {
					fail(w, fmt.Errorf("Google結算幣種無效"))
					return
				}
				if k == "sales_defaults" {
					maxLength = 12000
					var preset Record
					if json.Unmarshal([]byte(s), &preset) != nil {
						fail(w, fmt.Errorf("開票預設格式無效"))
						return
					}
					allowedPreset := map[string]bool{"company_address": true, "company_email": true, "business_number": true, "ccy": true, "due_days": true, "payment_details": true, "payment_terms": true, "note": true}
					for key, val := range preset {
						if !allowedPreset[key] {
							fail(w, fmt.Errorf("開票預設不可包含客戶、金額或正式確認資料"))
							return
						}
						if key == "due_days" {
							if val != nil {
								days, ok := val.(float64)
								if !ok || days < 0 || days > 3650 || days != float64(int(days)) {
									fail(w, fmt.Errorf("到期天數須為0至3650的整數"))
									return
								}
							}
						} else if _, ok := val.(string); !ok {
							fail(w, fmt.Errorf("開票預設欄位格式錯誤"))
							return
						}
					}
					if c := str(preset, "ccy"); c != "" && c != "USD" && c != "HKD" {
						fail(w, fmt.Errorf("預設幣種只支援HKD或USD"))
						return
					}
				}
				if len(s) > maxLength {
					fail(w, fmt.Errorf("设置内容过长"))
					return
				}
				allowed[k] = s
			}
		}
		if e = validReportSettings(allowed); e != nil {
			fail(w, e)
			return
		}
		out, e := a.s.settings(allowed)
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, out)
	})
	mux.HandleFunc("POST /api/platforms", func(w http.ResponseWriter, r *http.Request) {
		v, e := decode(w, r)
		if e == nil {
			e = a.s.savePlatform(v)
		}
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, Record{"saved": true})
	})
	mux.HandleFunc("GET /api/audit", func(w http.ResponseWriter, r *http.Request) {
		v, e := a.s.auditHistory()
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, v)
	})
	mux.HandleFunc("POST /api/lock", func(w http.ResponseWriter, r *http.Request) {
		v, e := decode(w, r)
		if e == nil {
			e = a.s.setLock(v)
		}
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, Record{"saved": true})
	})
	mux.HandleFunc("GET /api/settlements", func(w http.ResponseWriter, r *http.Request) {
		date := r.URL.Query().Get("as_of")
		if !validDate(date) {
			fail(w, fmt.Errorf("截止日期无效"))
			return
		}
		d, e := a.s.load()
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, settlementSchedule(d, date))
	})
	mux.HandleFunc("PUT /api/reviews/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := decode(w, r)
		if e != nil {
			fail(w, e)
			return
		}
		d, e := a.s.load()
		if e != nil {
			fail(w, e)
			return
		}
		exists := false
		for _, t := range d.Tx {
			if str(t, "id") == r.PathValue("id") {
				if e = a.s.checkEditable(str(t, "date")); e != nil {
					fail(w, e)
					return
				}
				exists = true
				break
			}
		}
		if !exists {
			fail(w, fmt.Errorf("流水不存在"))
			return
		}
		_, e = a.s.db.Exec("INSERT INTO reviews VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET category=excluded.category,note=excluded.note", r.PathValue("id"), strings.TrimSpace(str(v, "category")), strings.TrimSpace(str(v, "note")))
		if e != nil {
			fail(w, e)
			return
		}
		if e = a.s.refreshCandidates(); e != nil {
			fail(w, fmt.Errorf("分類已保存，但自動匹配未完成：%w", e))
			return
		}
		sendJSON(w, 200, Record{"saved": true})
	})
	mux.HandleFunc("PUT /api/invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := decode(w, r)
		if e == nil {
			v["id"] = r.PathValue("id")
			e = a.s.saveInvoice(v)
		}
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, Record{"saved": true})
	})
	mux.HandleFunc("POST /api/matches", func(w http.ResponseWriter, r *http.Request) {
		v, e := decode(w, r)
		if e == nil {
			e = a.s.saveMatch(v)
		}
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, Record{"saved": true})
	})
	mux.HandleFunc("POST /api/automation/enable", func(w http.ResponseWriter, r *http.Request) {
		if _, e := a.s.settings(Record{"automation": "retain_unmatched"}); e != nil {
			fail(w, e)
			return
		}
		if e := a.s.refreshCandidates(); e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, Record{"saved": true})
	})
	mux.HandleFunc("POST /api/matches/refresh", func(w http.ResponseWriter, r *http.Request) {
		if e := a.s.refreshCandidates(); e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, Record{"saved": true})
	})
	mux.HandleFunc("POST /api/import", a.importFiles)
	mux.HandleFunc("POST /api/documents/organize", func(w http.ResponseWriter, r *http.Request) {
		if _, e := a.s.backup(); e != nil {
			fail(w, e)
			return
		}
		if e := a.s.organiseBankAdviceContents(); e != nil {
			fail(w, e)
			return
		}
		if e := a.s.organiseInvoiceContents(); e != nil {
			fail(w, e)
			return
		}
		result, e := a.s.organizeDocuments()
		if e != nil {
			fail(w, e)
			return
		}
		if e = a.s.refreshCandidates(); e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, result)
	})
	mux.HandleFunc("POST /api/invoice-preview", func(w http.ResponseWriter, r *http.Request) {
		v, e := decode(w, r)
		var result Record
		if e == nil {
			v["status"] = "草稿"
			v["number"] = "PREVIEW"
			result, e = a.s.generate("销售发票", v, a.font, true)
		}
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, result)
	})
	mux.HandleFunc("POST /api/generate", func(w http.ResponseWriter, r *http.Request) {
		v, e := decode(w, r)
		var result Record
		if e == nil {
			result, e = a.s.generate(str(v, "kind"), v, a.font)
		}
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, result)
	})
	mux.HandleFunc("GET /api/documents/{sha}", func(w http.ResponseWriter, r *http.Request) {
		path, e := a.s.documentPath(r.PathValue("sha"))
		if e != nil {
			http.Error(w, "凭证不存在或路径无效", 404)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": filepath.Base(path)}))
		http.ServeFile(w, r, path)
	})
	mux.HandleFunc("POST /api/export", func(w http.ResponseWriter, r *http.Request) {
		v, e := decode(w, r)
		var path string
		if e == nil {
			path, e = a.s.export(str(v, "start"), str(v, "end"), yes(v, "allow_incomplete"), str(v, "company"), v)
		}
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, Record{"url": "/api/downloads/export/" + url.PathEscape(filepath.Base(path)), "name": filepath.Base(path)})
	})
	mux.HandleFunc("POST /api/director-documents", func(w http.ResponseWriter, r *http.Request) {
		v, e := decode(w, r)
		if e != nil {
			fail(w, e)
			return
		}
		result, e := a.s.generateDirectorDocument(v, a.font)
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, result)
	})
	mux.HandleFunc("GET /api/director-drafts/{year}/{name}", func(w http.ResponseWriter, r *http.Request) {
		year, name := r.PathValue("year"), r.PathValue("name")
		if !rx(`^\d{4}$`).MatchString(year) || filepath.Base(name) != name || !strings.HasSuffix(name, ".pdf") || strings.HasPrefix(name, ".") {
			http.NotFound(w, r)
			return
		}
		path, e := withinRoot(filepath.Join(a.s.root, "董事往來草稿"), filepath.Join(year, name))
		if e != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
		http.ServeFile(w, r, path)
	})
	mux.HandleFunc("POST /api/backup", func(w http.ResponseWriter, r *http.Request) {
		path, e := a.s.backup()
		if e != nil {
			fail(w, e)
			return
		}
		sendJSON(w, 200, Record{"url": "/api/downloads/backup/" + url.PathEscape(filepath.Base(path)), "name": filepath.Base(path)})
	})
	mux.HandleFunc("GET /api/downloads/{kind}/{name}", func(w http.ResponseWriter, r *http.Request) {
		folder := "导出"
		switch r.PathValue("kind") {
		case "export":
		case "backup":
			folder = "备份"
		default:
			http.NotFound(w, r)
			return
		}
		name := r.PathValue("name")
		if filepath.Base(name) != name || !strings.HasSuffix(name, ".zip") || strings.HasPrefix(name, ".") {
			http.NotFound(w, r)
			return
		}
		path, e := withinRoot(filepath.Join(a.s.root, folder), name)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		http.ServeFile(w, r, path)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		rel := strings.TrimPrefix(r.URL.Path, "/")
		if rel == "" {
			rel = "index.html"
		}
		path, e := withinRoot(a.assets, filepath.FromSlash(rel))
		if e != nil {
			if filepath.Ext(rel) != "" {
				http.NotFound(w, r)
				return
			}
			path = filepath.Join(a.assets, "index.html")
		}
		if _, e = os.Stat(path); e != nil {
			http.Error(w, "前端尚未构建：请运行 scripts/build.ps1", 503)
			return
		}
		// Never serve dotenv/config credentials, even if web-dir contains them.
		for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
			if strings.HasPrefix(part, ".") {
				http.NotFound(w, r)
				return
			}
		}
		if a.envFile != "" {
			config, err := filepath.EvalSymlinks(a.envFile)
			if err == nil {
				config, _ = filepath.Abs(config)
				served, _ := filepath.Abs(path)
				if strings.EqualFold(config, served) {
					http.NotFound(w, r)
					return
				}
			}
		}
		http.ServeFile(w, r, path)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("request panic: %v", v)
				sendJSON(w, 500, Record{"error": "处理失败，请检查服务器日志"})
			}
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		host, _, e := net.SplitHostPort(r.Host)
		if e != nil {
			host = r.Host
		}
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			http.Error(w, "仅允许本机访问", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, e := url.Parse(origin)
			if e != nil || u.Scheme != "http" || !strings.EqualFold(u.Host, r.Host) {
				http.Error(w, "来源不允许", 403)
				return
			}
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "不允许跨站请求", 403)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if r.URL.Path != "/api/rebuild" && r.URL.Path != "/api/rebuild/status" {
				a.mu.Lock()
				defer a.mu.Unlock()
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (a *server) importFiles(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	if e := r.ParseMultipartForm(8 << 20); e != nil {
		fail(w, fmt.Errorf("上传失败，单批次最多100MB: %w", e))
		return
	}
	defer r.MultipartForm.RemoveAll()
	kind := r.FormValue("kind")
	if kind != "银行" && !invoiceKind(kind) && kind != "平台结算" && kind != "银行通知" {
		fail(w, fmt.Errorf("导入类型无效"))
		return
	}
	if invoiceKind(kind) {
		d, e := a.s.load()
		if e != nil {
			fail(w, e)
			return
		}
		if len(d.Checks) == 0 {
			fail(w, fmt.Errorf("请先导入银行月结单，再导入发票进行匹配；未付款或未收款发票仍会保留"))
			return
		}
	}
	fs := r.MultipartForm.File["files"]
	if len(fs) == 0 || len(fs) > 50 {
		fail(w, fmt.Errorf("每次请选择1至50份PDF"))
		return
	}
	added, skipped := 0, 0
	warnings := []Record{}
	errs := []Record{}
	results := []Record{}
	for _, h := range fs {
		func() {
			src, e := h.Open()
			if e != nil {
				errs = append(errs, Record{"name": h.Filename, "error": e.Error()})
				return
			}
			defer src.Close()
			tmp, e := os.CreateTemp(a.s.root, "upload-*.pdf")
			if e != nil {
				errs = append(errs, Record{"name": h.Filename, "error": e.Error()})
				return
			}
			defer os.Remove(tmp.Name())
			_, e = io.Copy(tmp, src)
			ce := tmp.Close()
			if e == nil {
				e = ce
			}
			ok := false
			if e == nil {
				ok, e = a.s.importPDF(r.Context(), tmp.Name(), filepath.Base(h.Filename), kind)
			}
			if e != nil {
				errs = append(errs, Record{"name": h.Filename, "error": e.Error()})
			} else if ok {
				added++
				b, err := os.ReadFile(tmp.Name())
				if err == nil {
					var raw string
					if err = a.s.db.QueryRow("SELECT meta FROM documents WHERE sha=?", hashBytes(b)).Scan(&raw); err == nil {
						var meta Record
						if json.Unmarshal([]byte(raw), &meta) == nil && str(meta, "ocr_fallback_reason") != "" {
							warnings = append(warnings, Record{"name": h.Filename, "warning": str(meta, "ocr_fallback_reason") + "；已使用本地解析"})
						}
					}
				}
			} else {
				skipped++
			}
			if e == nil {
				if b, err := os.ReadFile(tmp.Name()); err == nil {
					sha := hashBytes(b)
					var rel string
					err = a.s.db.QueryRow("SELECT path FROM documents WHERE sha=?", sha).Scan(&rel)
					if err != nil && !ok {
						if keep, findErr := a.s.duplicateOriginal(tmp.Name(), kind); findErr == nil && keep != "" {
							sha = keep
							err = a.s.db.QueryRow("SELECT path FROM documents WHERE sha=?", sha).Scan(&rel)
						}
					}
					if err == nil {
						status := "已导入并自动命名"
						if !ok {
							status = "内容重复，已自动清理本次导入副本"
						}
						results = append(results, Record{"archive_name": filepath.Base(rel), "sha": sha, "status": traditional(status)})
					}
				}
			}
		}()
	}
	if _, e := a.s.organizeDocuments(); e != nil {
		errs = append(errs, Record{"name": "重复检测与文件整理", "error": e.Error()})
	}
	if e := a.s.refreshCandidates(); e != nil {
		errs = append(errs, Record{"name": "匹配候选", "error": e.Error()})
	}
	sendJSON(w, 200, Record{"added": added, "skipped": skipped, "errors": errs, "warnings": warnings, "files": results})
}
