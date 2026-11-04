package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	pdf "github.com/ledongthuc/pdf"
	"golang.org/x/text/unicode/norm"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var regexCache sync.Map

func rx(s string) *regexp.Regexp {
	if v, ok := regexCache.Load(s); ok {
		return v.(*regexp.Regexp)
	}
	r := regexp.MustCompile(s)
	v, _ := regexCache.LoadOrStore(s, r)
	return v.(*regexp.Regexp)
}
func capture(pattern, s string) string {
	m := rx(pattern).FindStringSubmatch(s)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
func hashBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func safeName(s string) string {
	s = rx(`[<>:"/\\|?*\x00-\x1f]`).ReplaceAllString(s, "_")
	r := []rune(strings.Trim(s, " ."))
	if len(r) > 65 {
		r = r[:65]
	}
	if len(r) == 0 {
		return "待核验"
	}
	return string(r)
}
func invoiceName(i Record) string {
	if str(i, "date") == "" || str(i, "vendor") == "" || str(i, "number") == "" || str(i, "ccy") == "" || i["amount"] == nil {
		return "待核验_发票_" + str(i, "hash")[:8] + ".pdf"
	}
	return strings.ReplaceAll(str(i, "date"), "-", "") + "-" + safeName(str(i, "vendor")) + "-" + safeName(str(i, "number")) + fmt.Sprintf("-%.2f-%s.pdf", num(i, "amount"), str(i, "ccy"))
}

type word struct {
	x, y, w float64
	s       string
}
type pdfLine struct {
	words []word
	page  int
}

func (l pdfLine) text(min, max float64) string {
	ss := []string{}
	for _, w := range l.words {
		if w.x >= min && w.x < max {
			ss = append(ss, w.s)
		}
	}
	return strings.Join(ss, " ")
}
func readPDF(path string) (lines []pdfLine, pages int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("PDF解析失败: %v", r)
		}
	}()
	f, r, e := pdf.Open(path)
	if e != nil {
		return nil, 0, e
	}
	defer f.Close()
	pages = r.NumPage()
	if pages > 1000 {
		return nil, 0, fmt.Errorf("单份PDF最多1000页")
	}
	for p := 1; p <= pages; p++ {
		chars := r.Page(p).Content().Text
		sort.SliceStable(chars, func(i, j int) bool {
			if chars[i].Y == chars[j].Y {
				return chars[i].X < chars[j].X
			}
			return chars[i].Y > chars[j].Y
		})
		rows := [][]pdf.Text{}
		for _, c := range chars {
			if len(rows) == 0 || math.Abs(rows[len(rows)-1][0].Y-c.Y) > 2 {
				rows = append(rows, []pdf.Text{c})
			} else {
				rows[len(rows)-1] = append(rows[len(rows)-1], c)
			}
		}
		for _, row := range rows {
			sort.Slice(row, func(i, j int) bool { return row[i].X < row[j].X })
			ws := []word{}
			for _, c := range row {
				if strings.TrimSpace(c.S) == "" {
					continue
				}
				if len(ws) > 0 && c.X-(ws[len(ws)-1].x+ws[len(ws)-1].w) < 1.8 {
					last := &ws[len(ws)-1]
					last.s += c.S
					last.w = c.X + c.W - last.x
				} else {
					ws = append(ws, word{c.X, c.Y, c.W, c.S})
				}
			}
			lines = append(lines, pdfLine{ws, p})
		}
	}
	return lines, pages, nil
}

var monthNames = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}

func parseDate(s string) string {
	m := rx(`(20\d{2})[-/.年]\s*(\d{1,2})[-/.月]\s*(\d{1,2})`).FindStringSubmatch(s)
	if len(m) > 0 {
		mo, _ := strconv.Atoi(m[2])
		day, _ := strconv.Atoi(m[3])
		d := fmt.Sprintf("%s-%02d-%02d", m[1], mo, day)
		if validDate(d) {
			return d
		}
	}
	for _, layout := range []string{"2 January 2006", "2 Jan 2006", "02/01/2006", "2/1/2006", "02-01-2006", "2.1.2006"} {
		candidate := rx(`\d{1,2}(?:\s+[A-Za-z]+\s+|[/.-]\d{1,2}[/.-])20\d{2}`).FindString(s)
		if t, e := time.Parse(layout, candidate); e == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}
func parseInvoice(path, name, sha string) (Record, error) {
	lines, pages, e := readPDF(path)
	if e != nil {
		return nil, e
	}
	return parseInvoiceLines(lines, pages, name, sha)
}

func parseInvoiceLines(lines []pdfLine, pages int, name, sha string) (Record, error) {
	ss := []string{}
	for _, l := range lines {
		ss = append(ss, l.text(0, 10000))
	}
	text := norm.NFKC.String(strings.Join(ss, "\n"))
	if strings.Contains(text, "HSBC Business Direct Statement") {
		return nil, fmt.Errorf("这是银行月结单，请使用导入月结单")
	}
	i := Record{"id": "INV-" + sha[:12], "hash": sha, "source": name, "pages": pages, "verified": false, "date": "", "vendor": "", "number": "", "ccy": "", "amount": nil, "accounts": []string{}, "note": "", "method": "Go PDF文字内容识别，待核验", "amount_basis": ""}
	i["number"] = capture(`(?i)(?:Invoice\s*(?:No\.?|Number|#)|發票編號|发票编号)\s*[:：]?\s*([A-Za-z0-9][A-Za-z0-9._/-]*)`, text)
	i["date"] = parseDate(capture(`(?i)(?:Invoice\s*Date|發票日期|发票日期)\s*[:：]?\s*([^\n]+)`, text))
	if no := capture(`(?i)Invoice\s*(?:No\.?|Number|#)\s*[:：]?\s*([A-Za-z0-9][A-Za-z0-9._/-]*)`, text); no != "" {
		i["number"] = no
	}
	if !validDate(str(i, "date")) {
		i["date"] = parseDate(capture(`(?i)Date\s+of\s+Issue\s*[:：]?\s*([^\n]+)`, text))
	}
	if m := rx(`\b((?:IS|IT)\d+)\s+(\d{1,2}\s+[A-Za-z]+\s+20\d{2})`).FindStringSubmatch(text); len(m) > 0 {
		i["number"] = m[1]
		if str(i, "date") == "" {
			i["date"] = parseDate(m[2])
		}
	}
	i["vendor"] = capture(`(?i)Beneficiary['’]?s\s*Name\s*[:：]\s*([^\n]+)`, text)
	if str(i, "vendor") == "" {
		for n, line := range ss {
			if n >= 15 {
				break
			}
			if !rx(`(?i)Bill\s*to|客戶名稱|客户名称`).MatchString(line) && rx(`(?i)有限公司|\bLIMITED\b|\bLTD\.?$`).MatchString(line) {
				i["vendor"] = line
				break
			}
		}
	}
	if rx(`(?i)US\s*\$|\bUSD\b`).MatchString(text) {
		i["ccy"] = "USD"
	} else if rx(`(?i)\bHKD\b|HK\s*\$`).MatchString(text) {
		i["ccy"] = "HKD"
	}
	if rx(`(?i)\bHKD\s+Amount\b|\bAmount\s*\(HKD\)`).MatchString(text) {
		i["ccy"] = "HKD"
		i["currency_basis"] = "票面金額欄明示HKD，優先於頁尾通用USD條款"
	}
	amountPattern := `([0-9][0-9, \t]*\.\s*[0-9]\s*[0-9])`
	for _, pattern := range []string{`(?i)Please\s+remit\s+invoice\s+total[^\n]*?` + amountPattern, `(?i)(?:總金額\s*\(HKD\)|总金额\s*\(HKD\)|總計|总计|合計|合计|Total\s*(?:Amount|Due)|Grand\s*Total)\s*[:：]?\s*(?:US\$|HK\$|USD|HKD|\$)?\s*` + amountPattern} {
		if m := rx(pattern).FindStringSubmatch(text); len(m) > 0 {
			v, _ := money(rx(`[,\s]`).ReplaceAllString(m[1], ""))
			i["amount"] = float64(v) / 100
			i["amount_basis"] = "总金额栏"
			if rx(`(?i)HKD|HK\$`).MatchString(m[0]) {
				i["ccy"] = "HKD"
			}
			if rx(`(?i)USD|US\$`).MatchString(m[0]) {
				i["ccy"] = "USD"
			}
			break
		}
	}
	if receipt := googleReceiptFields(text); receipt != nil {
		for k, v := range receipt {
			i[k] = v
		}
	}
	accounts := []string{}
	for _, a := range rx(`\b\d{3}[-—－]\d{6}[-—－]\d{3}\b`).FindAllString(text, -1) {
		accounts = append(accounts, strings.NewReplacer("—", "-", "－", "-").Replace(a))
	}
	i["accounts"] = accounts
	if len(strings.TrimSpace(text)) < 40 {
		i["method"] = "扫描PDF待人工录入"
		i["note"] = "无可用文字层，请打开原件人工核验；当前Go版本未集成OCR推理"
	} else if validDate(str(i, "date")) && str(i, "vendor") != "" && str(i, "number") != "" && str(i, "ccy") != "" && i["amount"] != nil {
		i["extraction_complete"] = true
		i["method"] = "Go PDF识别完整，待人工核验"
	} else {
		i["note"] = "识别字段不完整，请打开原件核验"
	}
	return i, nil
}
func decorate(t Record) {
	details := str(t, "details")
	t["orig_ccy"] = t["ccy"]
	t["orig_amount"] = num(t, "wd") + num(t, "dep")
	t["fx"] = nil
	t["reference_note"] = ""
	if m := rx(`(?i)/OCMT/\s*(USD|HKD)\s*([\d,]+\.\d{2})(?:\s|/|$)`).FindStringSubmatch(details); len(m) > 0 {
		if v, e := money(strings.ReplaceAll(m[2], ",", "")); e == nil && v > 0 {
			t["orig_ccy"] = strings.ToUpper(m[1])
			t["orig_amount"] = float64(v) / 100
		}
	}
	if s := capture(`/EXCH/([\d.]+)`, details); s != "" {
		t["fx"], _ = strconv.ParseFloat(s, 64)
	}
	own := str(t, "bank_number")
	if len(own) > 10 {
		own = own[:10]
	}
	internal := len(accountKey(own)) >= 9 && strings.Contains(accountKey(details), "TO"+accountKey(own))
	category := "收款待来源确认"
	switch {
	case internal:
		category = "内部转账/换汇待确认"
	case strings.Contains(details, "CREDIT INTEREST"):
		category = "银行利息"
	case strings.Contains(details, "FEE") || strings.Contains(details, "CHG FOR"):
		category = "银行费用"
	case strings.Contains(details, "GOOGLE"):
		category = "广告费待凭证"
	case strings.Contains(details, "CASH REBATE"):
		category = "返现"
	case num(t, "wd") > 0:
		category = "付款待分类"
	}
	t["category"] = category
	t["party"] = "待确认"
	t["need"] = "收款来源及销售凭证"
	t["documents"] = "补付款方和用途；销售收款需销售发票/订单及结算记录"
	switch {
	case internal:
		t["need"] = "转账/换汇说明"
		t["documents"] = "核对两端账户及换汇确认，不计作营业收支"
		t["party"] = "本公司账户"
	case category == "银行利息" || category == "银行费用" || category == "返现":
		t["need"] = "银行记录"
		t["documents"] = "保留月结单及银行通知，通常无需供应商发票"
		t["party"] = "汇丰银行"
	case strings.Contains(details, "STRIPE"):
		t["need"] = "销售及平台结算凭证"
		t["documents"] = "销售发票/订单、Stripe结算及手续费报表；净到账不等于收入"
		t["party"] = "Stripe"
	case num(t, "wd") > 0:
		t["need"] = "付款凭证待判定"
		t["documents"] = "补收款对象、用途及发票/收据"
	}
	if m := rx(`\((\d{2})(JAN|FEB|MAR|APR|MAY|JUN|JUL|AUG|SEP|OCT|NOV|DEC)(\d{2})\)`).FindStringSubmatch(details); len(m) > 0 {
		d := parseDate(m[1] + " " + strings.ToUpper(m[2][:1]) + strings.ToLower(m[2][1:]) + " 20" + m[3])
		if d != "" && d != str(t, "date") {
			t["reference_note"] = "日期栏" + str(t, "date") + "；参考编号日期" + d + "，请核实"
		}
	}
	classifyKnownParty(t)
	normalizeUnlistedOriginalAmount(t)
}

func parseBank(path, name, sha string) (Record, error) {
	lines, _, e := readPDF(path)
	if e != nil {
		return nil, e
	}
	return parseBankLines(lines, name, sha)
}

func parseBankLines(lines []pdfLine, name, sha string) (Record, error) {
	var e error
	header := ""
	for _, l := range lines {
		if l.page == 1 {
			header += l.text(0, 10000) + "\n"
		}
	}
	if !strings.Contains(header, "HSBC Business Direct Statement") {
		return nil, fmt.Errorf("不是支持的汇丰Business Direct文字月结单")
	}
	date := parseDate(header)
	if date == "" {
		return nil, fmt.Errorf("未识别月结单日期")
	}
	period, year := date[:7], date[:4]
	bank := capture(`Number\s*:\s*(\d{3}-\d{6}-\d{3})`, header)
	if bank == "" {
		return nil, fmt.Errorf("未识别银行账号")
	}
	txs, checks := []Record{}, []Record{}
	var cur, last Record
	active := false
	buf := []string{}
	td := ""
	n := 0
	finish := func() error {
		if cur == nil {
			return nil
		}
		if cur["dep"] == nil || cur["wd"] == nil {
			return fmt.Errorf("缺少账户汇总: %s", str(cur, "account"))
		}
		if cur["closing"] == nil {
			cur["closing"] = cur["opening"]
		}
		var dep, wd int64
		nd, nw := 0, 0
		for _, t := range txs {
			if str(t, "account") == str(cur, "account") {
				dep += cents(t, "dep")
				wd += cents(t, "wd")
				if cents(t, "dep") > 0 {
					nd++
				}
				if cents(t, "wd") > 0 {
					nw++
				}
			}
		}
		cur["actual_dep"] = float64(dep) / 100
		cur["actual_wd"] = float64(wd) / 100
		cur["actual_nd"] = nd
		cur["actual_nw"] = nw
		cur["delta"] = nil
		ok := dep == cents(cur, "dep") && wd == cents(cur, "wd") && nd == int(num(cur, "nd")) && nw == int(num(cur, "nw"))
		if cur["opening"] != nil && cur["closing"] != nil {
			delta := cents(cur, "opening") + dep - wd - cents(cur, "closing")
			cur["delta"] = float64(delta) / 100
			ok = ok && delta == 0
		}
		cur["ok"] = ok
		if !ok {
			return fmt.Errorf("月结单金额、笔数或余额核对失败: %s", str(cur, "account"))
		}
		checks = append(checks, cur)
		return nil
	}
	for _, l := range lines {
		line := l.text(0, 10000)
		compact := strings.ReplaceAll(line, " ", "")
		account := capture(`^HSBC Business Direct (HKD Current|HKD Savings|Foreign Currency Savings)$`, line)
		if account != "" {
			if cur == nil || str(cur, "account") != account || cur["dep"] != nil {
				if e = finish(); e != nil {
					return nil, e
				}
				ccy := "HKD"
				if strings.HasPrefix(account, "Foreign") {
					ccy = "USD"
				}
				cur = Record{"period": period, "account": account, "ccy": ccy, "bank_number": bank, "opening": nil, "closing": nil, "dep": nil, "wd": nil, "source": name, "page": l.page}
				n = 0
			}
			active = true
			buf = nil
			last = nil
			continue
		}
		if cur != nil && strings.HasPrefix(compact, "TotalNo.ofDeposits:") {
			if len(buf) > 0 && last != nil {
				last["details"] = str(last, "details") + " " + strings.Join(buf, " ")
				buf = nil
			}
			ns := rx(`\d+`).FindAllString(line, -1)
			if len(ns) >= 2 {
				cur["nd"], _ = strconv.Atoi(ns[len(ns)-2])
				cur["nw"], _ = strconv.Atoi(ns[len(ns)-1])
			}
			active = false
			continue
		}
		if cur != nil && strings.HasPrefix(compact, "TotalDepositAmount:") {
			ns := rx(`[\d,]+\.\d{2}`).FindAllString(line, -1)
			if len(ns) >= 2 {
				a, _ := money(strings.ReplaceAll(ns[len(ns)-2], ",", ""))
				b, _ := money(strings.ReplaceAll(ns[len(ns)-1], ",", ""))
				cur["dep"] = float64(a) / 100
				cur["wd"] = float64(b) / 100
			}
			active = false
			continue
		}
		if !active || cur == nil {
			continue
		}
		if str(cur, "ccy") == "USD" {
			for _, w := range l.words {
				if w.x < 75 && rx(`^[A-Z]{3}$`).MatchString(w.s) && w.s != "USD" && w.s != "CCY" {
					return nil, fmt.Errorf("外币账户检测到不支持的币种: %s", w.s)
				}
			}
		}
		skip := false
		for _, prefix := range []string{"Date ", "CCY ", "ExchangeRate", "The Hongkong", "HSBC Business Direct Statement", "Number", "853-", "SpecialPrivileges", "Others"} {
			if strings.HasPrefix(line, prefix) {
				skip = true
			}
		}
		if skip {
			continue
		}
		if ds := rx(`\d{1,2} (?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)`).FindString(l.text(0, 100)); ds != "" {
			td = parseDate(ds + " " + year)
		}
		details := l.text(100, 320)
		vals := Record{}
		for _, w := range l.words {
			if w.x >= 320 && rx(`^[\d,]+\.\d{2}$`).MatchString(w.s) {
				key := "bal"
				if w.x < 395 {
					key = "dep"
				} else if w.x < 480 {
					key = "wd"
				}
				v, _ := money(strings.ReplaceAll(w.s, ",", ""))
				vals[key] = float64(v) / 100
			}
		}
		if strings.Contains(details, "B/F BALANCE") {
			cur["opening"] = vals["bal"]
			continue
		}
		if vals["dep"] != nil || vals["wd"] != nil {
			if td == "" {
				return nil, fmt.Errorf("未识别交易日期，页%d", l.page)
			}
			if details != "" {
				buf = append(buf, details)
			}
			n++
			id := hashBytes([]byte(fmt.Sprintf("%s|%s|%d", sha, str(cur, "account"), n)))
			t := Record{"id": "TX-" + id[:12], "date": td, "period": period, "account": cur["account"], "bank_number": bank, "ccy": cur["ccy"], "dep": num(vals, "dep"), "wd": num(vals, "wd"), "balance": vals["bal"], "details": strings.Join(buf, " "), "source": name, "page": l.page, "hash": sha}
			txs = append(txs, t)
			last = t
			buf = nil
			if vals["bal"] != nil {
				cur["closing"] = vals["bal"]
			}
		} else if details != "" {
			if strings.HasPrefix(details, "/OCMT/") || strings.HasPrefix(details, "/EXCH/") || strings.HasPrefix(details, "/TMS/") || rx(`(?i)\b(?:LIMITED|LTD\.?)$`).MatchString(details) && last != nil && len(buf) == 0 && (strings.Contains(str(last, "details"), "BIB-") || strings.Contains(str(last, "details"), "FROM ")) {
				if last != nil {
					last["details"] = str(last, "details") + " " + details
				}
			} else {
				buf = append(buf, details)
			}
		}
	}
	if e = finish(); e != nil {
		return nil, e
	}
	if len(checks) == 0 {
		return nil, fmt.Errorf("未识别账户活动")
	}
	for _, t := range txs {
		decorate(t)
	}
	return Record{"tx": txs, "checks": checks, "statement_date": date}, nil
}
func (s *Store) importPDF(ctx context.Context, path, name, kind string) (bool, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return false, e
	}
	if !strings.HasPrefix(string(b), "%PDF-") {
		return false, fmt.Errorf("文件不是有效PDF")
	}
	sha := hashBytes(b)
	var count int
	if e = s.db.QueryRow("SELECT count(*) FROM documents WHERE sha=?", sha).Scan(&count); e != nil {
		return false, e
	}
	if count > 0 {
		var oldKind string
		if e = s.db.QueryRow("SELECT kind FROM documents WHERE sha=?", sha).Scan(&oldKind); e != nil {
			return false, e
		}
		if oldKind != kind && !(invoiceKind(oldKind) && invoiceKind(kind)) {
			return false, fmt.Errorf("该原件已按%s归档，请先核对票据方向，不能按%s重复登记", oldKind, kind)
		}
		stored, err := s.documentPath(sha)
		if err != nil {
			return false, fmt.Errorf("同内容凭证已登记但归档原件不可读：%w", err)
		}
		original, err := os.ReadFile(stored)
		if err != nil || hashBytes(original) != sha {
			return false, fmt.Errorf("同内容凭证已登记但原件校验失败，请先恢复备份")
		}
		return false, nil
	}
	if kind == "银行" || invoiceKind(kind) {
		duplicateKind := kind
		if invoiceKind(kind) {
			duplicateKind = "自动发票"
		}
		if keep, e := s.duplicateOriginal(path, duplicateKind); e != nil {
			return false, e
		} else if keep != "" {
			return false, nil
		}
	}
	var meta Record
	var cloudError error
	pdfType, typeErr := detectPDFType(path)
	if typeErr != nil {
		return false, typeErr
	}
	if invoiceKind(kind) && needsCloudPDF(pdfType) && s.ocr.configured() {
		meta, cloudError = s.ocr.recognize(ctx, path, name, sha, "发票")
	}
	if kind == "银行" {
		if meta == nil {
			meta, e = parseBank(path, name, sha)
		}
		if e == nil {
			meta["recognition_provider"] = "local"
			meta["recognition_method"] = "Go本地PDF解析及银行对账"
			d, err := s.load()
			if err != nil {
				return false, err
			}
			for _, c := range arr(meta, "checks") {
				for _, old := range d.Checks {
					if str(c, "period") == str(old, "period") && str(c, "account") == str(old, "account") && str(c, "bank_number") == str(old, "bank_number") {
						return false, fmt.Errorf("同账户同月份已有月结单，请核对版本")
					}
				}
			}
		}
	} else if kind == "银行通知" {
		meta, e = parseCreditAdvice(path, name, sha)
	} else if kind == "平台结算" {
		meta = Record{"id": "SOURCE-" + sha[:12], "kind": kind, "date": "", "hash": sha, "source": name, "recognition_provider": "local", "note": "平台报告原件，仅归档，结算金额须人工核对"}
	} else if invoiceKind(kind) {
		if meta == nil {
			meta, e = parseInvoice(path, name, sha)
		}
		if e == nil {
			decision, err := s.detectInvoiceDirection(path, meta)
			if err != nil {
				return false, err
			}
			kind = str(decision, "detected_direction")
			applyInvoiceDirection(meta, decision, sha)
		}

	} else {
		return false, fmt.Errorf("导入类型无效")
	}
	if e != nil {
		if cloudError != nil {
			return false, fmt.Errorf("云端失败（%v）；本地解析失败（%w）", cloudError, e)
		}
		return false, e
	}
	if str(meta, "recognition_provider") == "" {
		meta["recognition_provider"] = "local"
	}
	if cloudError != nil {
		meta["ocr_fallback_reason"] = cloudError.Error()
		if kind == "发票" {
			meta["note"] = str(meta, "note") + "；腾讯云识别失败，已回退本地：" + cloudError.Error()
		}
	}
	if e = s.checkEditable(str(meta, "date"), str(meta, "statement_date")); e != nil {
		return false, e
	}
	if kind == "银行" {
		for _, t := range arr(meta, "tx") {
			if e = s.checkEditable(str(t, "date")); e != nil {
				return false, e
			}
		}
	}
	annotatePDFType(meta, pdfType)
	rel, e := s.archivePath(kind, meta, sha, nil)
	if e != nil {
		return false, e
	}
	if e = os.MkdirAll(filepath.Dir(filepath.Join(s.root, rel)), 0755); e != nil {
		return false, e
	}
	if e = os.WriteFile(filepath.Join(s.root, rel), b, 0644); e != nil {
		return false, e
	}
	canonicalMetadata(meta, filepath.Base(rel))
	j, _ := json.Marshal(meta)
	_, e = s.db.Exec("INSERT INTO documents VALUES(?,?,?,?,?)", sha, kind, filepath.Base(rel), rel, string(j))
	return e == nil, e
}
