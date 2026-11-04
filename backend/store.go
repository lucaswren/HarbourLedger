package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Store struct {
	db   *sql.DB
	root string
	ocr  *TencentOCR
}

func openStore(root string) (*Store, error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(filepath.Join(root, "憑證"), 0755); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", filepath.Join(root, "账务.sqlite3"))
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`PRAGMA busy_timeout=30000; CREATE TABLE IF NOT EXISTS documents(sha TEXT PRIMARY KEY,kind TEXT,name TEXT,path TEXT,meta TEXT); CREATE TABLE IF NOT EXISTS reviews(id TEXT PRIMARY KEY,category TEXT,note TEXT); CREATE TABLE IF NOT EXISTS allocations(id TEXT PRIMARY KEY,meta TEXT); CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value TEXT); CREATE TABLE IF NOT EXISTS accounts(id TEXT PRIMARY KEY,meta TEXT);`)
	if e != nil {
		db.Close()
		return nil, e
	}
	s := &Store{db: db, root: root}
	if _, e = db.Exec(`CREATE TABLE IF NOT EXISTS stripe_records(scope TEXT NOT NULL,id TEXT NOT NULL,kind TEXT NOT NULL,meta TEXT NOT NULL,PRIMARY KEY(scope,id)); CREATE TABLE IF NOT EXISTS stripe_links(scope TEXT NOT NULL,id TEXT NOT NULL,document_id TEXT NOT NULL,PRIMARY KEY(scope,id));`); e != nil {
		db.Close()
		return nil, e
	}
	if e = s.setupControls(); e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) load() (Data, error) {
	d := emptyData()
	cfg, err := s.settings(nil)
	if err != nil {
		return d, err
	}
	rows, e := s.db.Query("SELECT sha,kind,name,path,meta FROM documents ORDER BY kind,name,sha")
	if e != nil {
		return d, e
	}
	for rows.Next() {
		var sha, kind, name, path, meta string
		if e = rows.Scan(&sha, &kind, &name, &path, &meta); e != nil {
			rows.Close()
			return d, e
		}
		var m Record
		if e = json.Unmarshal([]byte(meta), &m); e != nil {
			rows.Close()
			return d, fmt.Errorf("凭证 %s 元数据损坏: %w", name, e)
		}
		d.Docs = append(d.Docs, Record{"sha": sha, "kind": kind, "name": name, "archive_name": filepath.Base(path), "path": path, "pdf_type": str(m, "pdf_type"), "pdf_type_label": str(m, "pdf_type_label"), "url": "/api/documents/" + sha})
		clearResolvedExtractionWarning(m)
		switch kind {
		case "银行":
			for _, t := range arr(m, "tx") {
				classifyKnownParty(t, cfg)
				t["document_sha"] = sha
				d.Tx = append(d.Tx, t)
			}
			for _, c := range arr(m, "checks") {
				c["document_sha"] = sha
				d.Checks = append(d.Checks, c)
			}
		case "待分类发票":
			m["document_sha"] = sha
			m["kind"] = kind
			m["hash"] = sha
			d.Unclassified = append(d.Unclassified, m)
		case "银行通知":
			m["document_sha"] = sha
			d.Advices = append(d.Advices, m)
		case "平台结算":
			// Original attachment, not an issued invoice.
		case "销售发票":
			m["document_sha"] = sha
			m["kind"] = kind
			m["hash"] = sha
			if str(m, "customer") == "" {
				m["customer"] = str(m, "party")
			}
			if str(m, "origin") == "generated" || m["items"] != nil {
				m["origin"] = "generated"
				d.Issued = append(d.Issued, m)
			}
			d.Sales = append(d.Sales, m)
		case "发票":
			m["document_sha"] = sha
			d.Invoices = append(d.Invoices, m)
		default:
			m["document_sha"] = sha
			d.Issued = append(d.Issued, m)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return d, e
	}
	rows, e = s.db.Query("SELECT id,category,note FROM reviews")
	if e != nil {
		return d, e
	}
	reviews := map[string]Record{}
	for rows.Next() {
		var id, c, n string
		if e = rows.Scan(&id, &c, &n); e != nil {
			rows.Close()
			return d, e
		}
		reviews[id] = Record{"category": c, "note": n}
	}
	rows.Close()
	rows, e = s.db.Query("SELECT meta FROM allocations ORDER BY id")
	if e != nil {
		return d, e
	}
	for rows.Next() {
		var meta string
		var m Record
		if e = rows.Scan(&meta); e == nil {
			e = json.Unmarshal([]byte(meta), &m)
		}
		if e != nil {
			rows.Close()
			return d, e
		}
		if str(m, "note") == "需补付款/换汇通知" {
			m["note"] = "銀行扣賬與票面金額分開記錄"
		}
		if str(m, "basis") == "同账号及邻近日期，跨币金额未核验" {
			m["basis"] = "同賬號及鄰近日期找到發票"
		}
		d.Matches = append(d.Matches, m)
	}
	rows.Close()
	sort.Slice(d.Tx, func(i, j int) bool {
		return str(d.Tx[i], "date")+str(d.Tx[i], "account")+str(d.Tx[i], "id") < str(d.Tx[j], "date")+str(d.Tx[j], "account")+str(d.Tx[j], "id")
	})
	classifyOwnTransferReceipts(d.Tx, cfg)
	for _, t := range d.Tx {
		normalizeUnlistedOriginalAmount(t)
		r := reviews[str(t, "id")]
		t["manual_category"] = str(r, "category")
		t["manual_note"] = str(r, "note")
		serviceEvidence(t)
		t["internal_voucher"] = ""
		for _, i := range d.Issued {
			if str(i, "tx_id") == str(t, "id") {
				t["internal_voucher"] = str(i, "status") + " " + str(i, "number")
			}
		}
	}
	applyCreditAdvices(&d)
	var platformErr error
	d.Platforms, platformErr = s.platformList()
	if platformErr != nil {
		return d, platformErr
	}
	markDuplicates(d.Invoices)
	markSalesDuplicates(d.Sales)
	sort.Slice(d.Invoices, func(i, j int) bool {
		return str(d.Invoices[i], "date")+str(d.Invoices[i], "id") < str(d.Invoices[j], "date")+str(d.Invoices[j], "id")
	})
	sort.Slice(d.Checks, func(i, j int) bool {
		return str(d.Checks[i], "period")+str(d.Checks[i], "bank_number")+str(d.Checks[i], "account") < str(d.Checks[j], "period")+str(d.Checks[j], "bank_number")+str(d.Checks[j], "account")
	})
	prev := map[string]Record{}
	for _, c := range d.Checks {
		key := str(c, "bank_number") + str(c, "account") + str(c, "ccy")
		c["continuity"] = nil
		if p := prev[key]; p != nil {
			ms, _ := months(str(p, "period"), str(c, "period"))
			if len(ms) == 2 && c["opening"] != nil && p["closing"] != nil {
				delta := cents(c, "opening") - cents(p, "closing")
				c["continuity"] = float64(delta) / 100
				if delta != 0 {
					c["ok"] = false
				}
			}
		}
		prev[key] = c
	}
	verdicts(&d)
	return d, nil
}
func markDuplicates(invoices []Record) {
	sort.Slice(invoices, func(i, j int) bool { return str(invoices[i], "id") < str(invoices[j], "id") })
	seen := map[string]string{}
	for _, i := range invoices {
		i["duplicate"] = ""
		key := strings.ToLower(strings.Join(strings.Fields(str(i, "vendor")), "")) + "|" + strings.ToLower(str(i, "number")) + "|" + str(i, "ccy")
		if str(i, "vendor") != "" && str(i, "number") != "" && str(i, "ccy") != "" {
			i["duplicate"] = seen[key]
			if seen[key] == "" {
				seen[key] = str(i, "id")
			}
		}
	}
}
func (s *Store) settings(values Record) (Record, error) {
	if values != nil {
		tx, e := s.db.Begin()
		if e != nil {
			return nil, e
		}
		defer tx.Rollback()
		for k, v := range values {
			if _, e = tx.Exec("INSERT INTO settings VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, fmt.Sprint(v)); e != nil {
				return nil, e
			}
		}
		if e = tx.Commit(); e != nil {
			return nil, e
		}
	}
	r, e := s.db.Query("SELECT key,value FROM settings")
	if e != nil {
		return nil, e
	}
	defer r.Close()
	out := Record{}
	for r.Next() {
		var k, v string
		if e = r.Scan(&k, &v); e != nil {
			return nil, e
		}
		out[k] = v
	}
	return out, r.Err()
}
func (s *Store) saveMatch(m Record) error {
	d, e := s.load()
	if e != nil {
		return e
	}
	if strings.TrimSpace(str(m, "reason")) == "" {
		m["reason"] = str(m, "note")
	}
	for _, old := range d.Matches {
		if str(old, "id") == str(m, "id") {
			for _, t := range d.Tx {
				if str(t, "id") == str(old, "tx") {
					if e = s.checkEditable(str(t, "date")); e != nil {
						return e
					}
				}
			}
			for _, i := range append(append([]Record{}, d.Invoices...), d.Sales...) {
				if str(i, "id") == str(old, "invoice") {
					if e = s.checkEditable(str(i, "date")); e != nil {
						return e
					}
				}
			}
		}
		if str(old, "id") == str(m, "id") && str(old, "status") == "已确认" && str(m, "status") != "已确认" && strings.TrimSpace(str(m, "reason")) == "" {
			return fmt.Errorf("撤回已确认关联请填写原因")
		}
	}
	for _, t := range d.Tx {
		if str(t, "id") == str(m, "tx") {
			if e = s.checkEditable(str(t, "date")); e != nil {
				return e
			}
		}
	}
	for _, i := range append(append([]Record{}, d.Invoices...), d.Sales...) {
		if str(i, "id") == str(m, "invoice") {
			if e = s.checkEditable(str(i, "date")); e != nil {
				return e
			}
		}
	}
	if str(m, "id") == "" {
		m["id"] = uuid.NewString()
	}
	if st := str(m, "status"); st != "待确认" && st != "已确认" && st != "不匹配" {
		return fmt.Errorf("确认状态无效")
	}
	v, e := money(m["amount"])
	if e != nil || v <= 0 {
		return fmt.Errorf("分配金额必须为正数")
	}
	m["amount"] = float64(v) / 100
	next := []Record{}
	for _, x := range d.Matches {
		if str(x, "id") != str(m, "id") {
			next = append(next, x)
		}
	}
	d.Matches = append(next, m)
	verdicts(&d)
	if str(m, "validity") == "关联编号失效" {
		return fmt.Errorf("关联编号失效")
	}
	for _, x := range d.Matches {
		if str(x, "status") == "已确认" && str(x, "validity") != "有效确认" {
			return fmt.Errorf("不能保存: %s", str(x, "validity"))
		}
	}
	b, e := json.Marshal(m)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO allocations VALUES(?,?) ON CONFLICT(id) DO UPDATE SET meta=excluded.meta", str(m, "id"), string(b))
	return e
}
func (s *Store) saveInvoice(input Record) error {
	d, e := s.load()
	if e != nil {
		return e
	}
	var i Record
	kind := "发票"
	for _, x := range append(append(append([]Record{}, d.Invoices...), d.Sales...), d.Unclassified...) {
		if str(x, "id") == str(input, "id") {
			i = x
			break
		}
	}
	if i == nil {
		return fmt.Errorf("发票不存在")
	}
	if isSale(i) {
		kind = "销售发票"
	}
	if str(i, "kind") == "待分类发票" || str(input, "direction") != "" {
		kind = str(input, "direction")
		if kind != "发票" && kind != "销售发票" {
			return fmt.Errorf("請先確認此發票為採購或銷售")
		}
		if kind != str(i, "kind") {
			for _, a := range d.Matches {
				if str(a, "invoice") == str(i, "id") && str(a, "status") == "已确认" {
					return fmt.Errorf("變更發票方向前，請先撤回已確認關聯")
				}
			}
		}
		i["kind"] = kind
	}
	if e = s.checkEditable(str(i, "date"), str(input, "date")); e != nil {
		return e
	}
	for _, k := range []string{"date", "vendor", "number", "ccy", "amount", "accounts", "verified", "note", "customer", "due_date", "reviewer", "service_period", "document_type"} {
		if v, ok := input[k]; ok {
			i[k] = v
		}
	}
	if typ := str(i, "document_type"); typ != "" && typ != "invoice" && typ != "payment_receipt" {
		return fmt.Errorf("憑證類型無效")
	}
	if isSale(i) {
		i["vendor"] = i["customer"]
	}
	if due := str(i, "due_date"); due != "" && !validDate(due) {
		return fmt.Errorf("到期日期无效")
	}
	v, e := money(i["amount"])
	if e != nil || v <= 0 || !validDate(str(i, "date")) || strings.TrimSpace(str(i, "vendor")) == "" || strings.TrimSpace(str(i, "number")) == "" || str(i, "ccy") != "HKD" && str(i, "ccy") != "USD" {
		return fmt.Errorf("请填写有效日期、供应商、编号、HKD/USD币种和正数金额")
	}
	i["amount"] = float64(v) / 100
	i["direction_review_required"] = false
	i["detected_direction"] = kind
	i["extraction_complete"] = true // All required direction/date/party/currency/amount fields were validated above.
	clearResolvedExtractionWarning(i)
	i["method"] = "人工暂录，待核验"
	if yes(i, "verified") {
		if strings.TrimSpace(str(i, "reviewer")) == "" {
			return fmt.Errorf("人工核验请填写复核人")
		}
		i["verification_source"] = "human"
		i["method"] = "人工核验"
	}
	markDuplicates(d.Invoices)
	markSalesDuplicates(d.Sales)
	verdicts(&d)
	for _, x := range d.Matches {
		if str(x, "status") == "已确认" && str(x, "validity") != "有效确认" {
			return fmt.Errorf("修改将使已确认关联失效，请先撤回关联")
		}
	}
	sourcePath, e := s.documentPath(str(i, "hash"))
	if e != nil {
		return e
	}
	content, e := os.ReadFile(sourcePath)
	if e != nil {
		return e
	}
	if hashBytes(content) != str(i, "hash") {
		return fmt.Errorf("原始PDF校验失败")
	}
	if e = s.saveCanonical(str(i, "hash"), kind, str(i, "source"), i, content); e != nil {
		return e
	}
	return s.refreshCandidates()
}

// Remove only the obsolete parser-generated warning, never a user-written note.
func clearResolvedExtractionWarning(i Record) {
	if yes(i, "extraction_complete") && str(i, "note") == "识别字段不完整，请打开原件核验" {
		i["note"] = ""
	}
}
