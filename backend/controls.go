package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type jsonExecutor interface {
	Exec(string, ...any) (sql.Result, error)
}

func insertJSON(db jsonExecutor, table, id string, m Record) error {
	if table != "allocations" {
		return fmt.Errorf("无效表")
	}
	b, e := json.Marshal(m)
	if e != nil {
		return e
	}
	_, e = db.Exec("INSERT INTO "+table+" VALUES(?,?) ON CONFLICT(id) DO UPDATE SET meta=excluded.meta", id, string(b))
	return e
}
func (s *Store) setupControls() error {
	return setupControlsWith(s.db)
}

func setupControlsWith(db jsonExecutor) error {
	_, e := db.Exec(`CREATE TABLE IF NOT EXISTS platform_reports(id TEXT PRIMARY KEY,meta TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS audit_log(seq INTEGER PRIMARY KEY AUTOINCREMENT,at TEXT NOT NULL,entity TEXT NOT NULL,record_id TEXT NOT NULL,action TEXT NOT NULL,before_json TEXT,after_json TEXT);
 CREATE TRIGGER IF NOT EXISTS audit_no_update BEFORE UPDATE ON audit_log BEGIN SELECT RAISE(ABORT,'复核历史不可修改'); END;
 CREATE TRIGGER IF NOT EXISTS audit_no_delete BEFORE DELETE ON audit_log BEGIN SELECT RAISE(ABORT,'复核历史不可删除'); END;`)
	if e != nil {
		return e
	}
	for _, table := range []string{"documents", "allocations", "reviews", "settings", "accounts", "platform_reports"} {
		id := "id"
		old, newv := "OLD.meta", "NEW.meta"
		switch table {
		case "documents":
			id = "sha"
			old = "json_object('kind',OLD.kind,'name',OLD.name,'path',OLD.path,'meta',json(OLD.meta))"
			newv = "json_object('kind',NEW.kind,'name',NEW.name,'path',NEW.path,'meta',json(NEW.meta))"
		case "reviews":
			old = "json_object('category',OLD.category,'note',OLD.note)"
			newv = "json_object('category',NEW.category,'note',NEW.note)"
		case "settings":
			id = "key"
			old = "json_object('value',OLD.value)"
			newv = "json_object('value',NEW.value)"
		}
		for _, action := range []string{"INSERT", "UPDATE", "DELETE"} {
			before, after, record := "NULL", newv, "NEW."+id
			if action == "UPDATE" {
				before = old
			}
			if action == "DELETE" {
				before = old
				after = "NULL"
				record = "OLD." + id
			}
			query := fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS audit_%s_%s AFTER %s ON %s BEGIN INSERT INTO audit_log(at,entity,record_id,action,before_json,after_json) VALUES(strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ','now'),'%s',%s,'%s',%s,%s); END;`, table, strings.ToLower(action), action, table, table, record, action, before, after)
			if _, e = db.Exec(query); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Store) auditHistory(all ...bool) ([]Record, error) {
	query := "SELECT seq,at,entity,record_id,action,coalesce(before_json,''),coalesce(after_json,'') FROM audit_log WHERE entity<>'issues' ORDER BY seq DESC"
	if len(all) == 0 || !all[0] {
		query += " LIMIT 2000"
	}
	rows, e := s.db.Query(query)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var seq int
		var at, entity, id, action, before, after string
		if e = rows.Scan(&seq, &at, &entity, &id, &action, &before, &after); e != nil {
			return nil, e
		}
		out = append(out, Record{"id": seq, "at": at, "entity": entity, "record_id": id, "action": action, "before": before, "after": after})
	}
	return out, rows.Err()
}
func (s *Store) checkEditable(dates ...string) error {
	cfg, e := s.settings(nil)
	if e != nil {
		return e
	}
	lock := str(cfg, "locked_until")
	if lock == "" {
		return nil
	}
	for _, date := range dates {
		if validDate(date) && date <= lock {
			return fmt.Errorf("%s已结账锁定（截至%s），需在设置中填写原因解锁后修改", date, lock)
		}
	}
	return nil
}
func (s *Store) setLock(v Record) error {
	date := str(v, "locked_until")
	if date != "" && !validDate(date) {
		return fmt.Errorf("锁定日期无效")
	}
	if strings.TrimSpace(str(v, "reason")) == "" || strings.TrimSpace(str(v, "reviewer")) == "" {
		return fmt.Errorf("锁定或解锁需填写复核人及原因")
	}
	_, e := s.settings(Record{"locked_until": date, "lock_reason": str(v, "reason"), "lock_reviewer": str(v, "reviewer")})
	return e
}

// One-time upgrade preserves a complete rollback snapshot and historical facts.
func (s *Store) upgradeReviewPolicy() error {
	cfg, e := s.settings(nil)
	if e != nil {
		return e
	}
	if str(cfg, "review_policy_version") == "2" {
		return nil
	}
	d, e := s.load()
	if e != nil {
		return e
	}
	backup := ""
	if len(d.Docs) > 0 {
		backup, e = s.backup()
		if e != nil {
			return e
		}
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	unverified := map[string]bool{}
	for _, i := range d.Invoices {
		if (str(i, "method") == "Go PDF内容核验" || str(i, "method") == "PDF内容核验") && str(i, "verification_source") == "" {
			i["verified"] = false
			i["extraction_complete"] = true
			i["method"] = "历史机器识别完整，待人工核验"
			unverified[str(i, "id")] = true
			b, _ := json.Marshal(i)
			if _, e = tx.Exec("UPDATE documents SET meta=? WHERE sha=?", string(b), str(i, "document_sha")); e != nil {
				return e
			}
		}
	}
	for _, m := range d.Matches {
		if str(m, "status") == "已确认" && (str(m, "reviewer") == "系统自动关联" || unverified[str(m, "invoice")]) {
			m["previous_confirmation"] = clone(m)
			m["status"] = "待确认"
			m["note"] = str(m, "note") + "；核验规则升级，保留关联依据并等待人工复核"
			if e = insertJSON(tx, "allocations", str(m, "id"), m); e != nil {
				return e
			}
		}
	}
	for k, v := range map[string]string{"automation": "retain_unmatched", "review_policy_version": "2", "review_policy_backup": backup} {
		if _, e = tx.Exec("INSERT INTO settings VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, v); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// Reference material is not an application task list. Retain a rollback backup,
// retire the accidental module, and exclude its historical rows from exports.
func (s *Store) retireReferenceIssues() error {
	var n int
	if e := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='issues'").Scan(&n); e != nil {
		return e
	}
	if n == 0 {
		return nil
	}
	if _, e := s.backup(); e != nil {
		return e
	}
	_, e := s.db.Exec("DROP TABLE issues")
	return e
}
