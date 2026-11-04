package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Rebuild always starts from every registered original, across all periods.
// Reviewed invoice fields and confirmed allocations remain authoritative.
func (s *Store) rebuildLedger(ctx context.Context, useCloud bool, progress func(int, int, string)) (Record, error) {
	if useCloud && !s.ocr.configured() {
		return nil, fmt.Errorf("腾讯云OCR未配置完整")
	}
	old, e := s.load()
	if e != nil {
		return nil, e
	}
	for _, t := range old.Tx {
		if e = s.checkEditable(str(t, "date")); e != nil {
			return nil, e
		}
	}
	for _, i := range append(append([]Record{}, old.Invoices...), old.Sales...) {
		if e = s.checkEditable(str(i, "date")); e != nil {
			return nil, e
		}
	}
	report := Record{"total": len(old.Docs), "updated": 0, "preserved": 0, "issues": 0, "files": []Record{}, "backup_url": ""}
	if len(old.Docs) == 0 {
		return report, nil
	}
	progress(0, len(old.Docs), "校验全部原件与创建备份")
	// Validate the entire evidence set before paying for OCR or changing metadata.
	for _, doc := range old.Docs {
		path, err := s.documentPath(str(doc, "sha"))
		if err != nil {
			return nil, fmt.Errorf("原件不可读：%s", str(doc, "name"))
		}
		b, err := os.ReadFile(path)
		if err != nil || hashBytes(b) != str(doc, "sha") {
			return nil, fmt.Errorf("原件校验失败：%s", str(doc, "name"))
		}
	}
	backup, e := s.backup()
	if e != nil {
		return nil, e
	}
	report["backup_url"] = "/api/downloads/backup/" + filepath.Base(backup)
	candidate := emptyData()
	candidate.Platforms = old.Platforms
	for _, m := range old.Matches {
		candidate.Matches = append(candidate.Matches, clone(m))
	}
	updates := map[string]Record{}
	txIDs := map[string]bool{}
	for n, doc := range old.Docs {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		sha, kind := str(doc, "sha"), str(doc, "kind")
		progress(n, len(old.Docs), str(doc, "archive_name"))
		var raw string
		if e = s.db.QueryRow("SELECT meta FROM documents WHERE sha=?", sha).Scan(&raw); e != nil {
			return nil, e
		}
		var current Record
		if e = json.Unmarshal([]byte(raw), &current); e != nil {
			return nil, e
		}
		next := clone(current)
		status, note := "已重新解析", ""
		path, _ := s.documentPath(sha)
		if kind == "银行" || invoiceKind(kind) {
			var parsed Record
			var parseErr, errorCloud error
			// Verified/manual fields are still read locally, without another paid call.
			protected := invoiceKind(kind) && (yes(current, "verified") || strings.HasPrefix(str(current, "method"), "人工"))
			pdfType, typeErr := detectPDFType(path)
			if typeErr != nil {
				return nil, typeErr
			}
			annotatePDFType(next, pdfType)
			if invoiceKind(kind) && needsCloudPDF(pdfType) && useCloud && !protected {
				parsed, errorCloud = s.ocr.recognize(ctx, path, str(doc, "name"), sha, kind)
			}
			if parsed == nil {
				if kind == "银行" {
					parsed, parseErr = parseBank(path, str(doc, "name"), sha)
				} else {
					parsed, parseErr = parseInvoice(path, str(doc, "name"), sha)
				}
			}
			if kind == "销售发票" {
				parsed, parseErr = s.parseSalesInvoice(ctx, path, str(doc, "name"), sha, pdfType, parsed)
				if parsed != nil {
					parsed["kind"] = "销售发票"
				}
			}
			if errorCloud != nil {
				note = "腾讯云失败，已回退本地：" + errorCloud.Error()
			}
			if parseErr != nil {
				status = "保留原记录，需人工检查"
				note = parseErr.Error()
				report["issues"] = num(report, "issues") + 1
			} else if kind == "银行" {
				priorIDs := map[string]bool{}
				for _, t := range arr(current, "tx") {
					priorIDs[str(t, "id")] = true
				}
				stable := len(arr(parsed, "tx")) == len(priorIDs)
				for _, t := range arr(parsed, "tx") {
					if !priorIDs[str(t, "id")] {
						stable = false
					}
				}
				if !stable {
					status = "保留原记录，流水编号变化需复核"
					report["issues"] = num(report, "issues") + 1
				} else {
					for _, k := range []string{"tx", "checks", "statement_date"} {
						next[k] = parsed[k]
					}
					next["recognition_provider"] = "local"
					next["recognition_method"] = "Go本地PDF解析及银行对账"
					delete(next, "ocr_fallback_reason")
					delete(next, "ocr_request_ids")
					status = "已本地解析"
				}
			} else if protected {
				status = "已校验原件，保留人工/已核验字段"
			} else {
				// Fill gaps; a rerun must not erase existing values with empty OCR results.
				changed := false
				for _, k := range []string{"date", "vendor", "number", "ccy", "amount", "customer", "due_date"} {
					if (next[k] == nil || next[k] == "") && parsed[k] != nil && parsed[k] != "" {
						next[k] = parsed[k]
						changed = true
					}
				}
				if len(texts(next, "accounts")) == 0 && len(texts(parsed, "accounts")) > 0 {
					next["accounts"] = parsed["accounts"]
					changed = true
				}
				if changed {
					next["verified"] = false
					next["method"] = "重新梳理补全，待人工核验"
				}
				if !validDate(str(next, "date")) || str(next, "vendor") == "" || str(next, "number") == "" || cents(next, "amount") <= 0 || (str(next, "ccy") != "HKD" && str(next, "ccy") != "USD") {
					status = "已梳理，字段不完整待人工核验"
					report["issues"] = num(report, "issues") + 1
				}
			}
		} else {
			status = "自制凭证原件已校验"
		}
		if kind == "发票" && str(next, "pdf_type") == "text" {
			next["recognition_provider"] = "local"
		}
		// Volatile processing metadata stays outside manually confirmed business data.
		next["last_rebuilt_at"] = time.Now().Format(time.RFC3339)
		next["last_rebuild_status"] = status
		updates[sha] = next
		switch kind {
		case "银行":
			for _, t := range arr(next, "tx") {
				candidate.Tx = append(candidate.Tx, t)
				txIDs[str(t, "id")] = true
			}
			candidate.Checks = append(candidate.Checks, arr(next, "checks")...)
		case "待分类发票":
			candidate.Unclassified = append(candidate.Unclassified, next)
		case "销售发票":
			candidate.Sales = append(candidate.Sales, next)
		case "发票":
			candidate.Invoices = append(candidate.Invoices, next)
		default:
			candidate.Issued = append(candidate.Issued, next)
		}
		if status == "已重新解析" || status == "已本地解析" {
			report["updated"] = num(report, "updated") + 1
		} else {
			report["preserved"] = num(report, "preserved") + 1
		}
		report["files"] = append(report["files"].([]Record), Record{"sha": sha, "name": str(doc, "archive_name"), "kind": kind, "status": status, "note": note})
	}
	markDuplicates(candidate.Invoices)
	markSalesDuplicates(candidate.Sales)
	verdicts(&candidate)
	for _, m := range candidate.Matches {
		if str(m, "status") == "已确认" && str(m, "validity") != "有效确认" {
			return nil, fmt.Errorf("梳理将使已确认关联失效，未保存：%s", str(m, "validity"))
		}
	}
	for _, v := range candidate.Issued {
		if str(v, "tx_id") != "" && !txIDs[str(v, "tx_id")] {
			return nil, fmt.Errorf("梳理将使自制凭证关联失效，未保存")
		}
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	for sha, m := range updates {
		b, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		if _, e = tx.Exec("UPDATE documents SET meta=? WHERE sha=?", string(b), sha); e != nil {
			return nil, e
		}
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	if e = s.organiseInvoiceContents(); e != nil {
		return nil, e
	}
	// Reload recomputes cross-month balances, duplicate status and evidence gaps.
	if e = s.refreshCandidates(); e != nil {
		return nil, fmt.Errorf("资料已梳理，但候选刷新失败：%w", e)
	}
	organized, e := s.organizeDocuments()
	if e != nil {
		return nil, fmt.Errorf("资料已梳理，但文件整理失败：%w", e)
	}
	report["organization"] = organized
	fresh, e := s.load()
	if e != nil {
		return nil, e
	}
	failed, needs, duplicates := 0, 0, 0
	for _, c := range fresh.Checks {
		if !yes(c, "ok") {
			failed++
		}
	}
	for _, t := range fresh.Tx {
		state := str(t, "state")
		if state != "银行凭证已具备" && state != "发票已关联（复核分类）" {
			needs++
		}
	}
	for _, i := range fresh.Invoices {
		if str(i, "duplicate") != "" {
			duplicates++
		}
	}
	report["transactions"] = len(fresh.Tx)
	report["failed_checks"] = failed
	report["needs"] = needs
	report["duplicate_invoices"] = duplicates
	logs := filepath.Join(s.root, "整理记录")
	if e = os.MkdirAll(logs, 0755); e != nil {
		return nil, e
	}
	b, e := json.MarshalIndent(report, "", "  ")
	if e != nil {
		return nil, e
	}
	if e = os.WriteFile(filepath.Join(logs, "账务梳理_"+time.Now().Format("20060102_150405.000000000")+".json"), b, 0644); e != nil {
		return nil, e
	}
	progress(len(old.Docs), len(old.Docs), "")
	return report, nil
}
