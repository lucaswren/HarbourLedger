package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"github.com/xuri/excelize/v2"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type column struct{ key, label string }

func exportPeriod(start, end string) string {
	if start == end {
		return start
	}
	return start + "至" + end
}

// Human-readable collision suffixes; existing downloads are never overwritten.
func publishArchive(temp, dir, stem string) (string, error) {
	for version := 1; ; version++ {
		name := stem
		if version > 1 {
			name += fmt.Sprintf("（%d）", version)
		}
		dest := filepath.Join(dir, name+".zip")
		reserved, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if os.IsExist(e) {
			continue
		}
		if e != nil {
			return "", e
		}
		if e = reserved.Close(); e != nil {
			os.Remove(dest)
			return "", e
		}
		if e = os.Rename(temp, dest); e != nil {
			os.Remove(dest)
			return "", e
		}
		return dest, nil
	}
}

func packagePDFName(doc Record, path string, used map[string]bool) string {
	name := filepath.Base(path)
	if strings.HasPrefix(name, "待核验_") {
		name = "待核验" + str(doc, "kind") + ".pdf"
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	for n := 1; ; n++ {
		candidate := stem + ".pdf"
		if n > 1 {
			candidate = fmt.Sprintf("%s（%d）.pdf", stem, n)
		}
		key := strings.ToLower(str(doc, "kind") + "/" + candidate)
		if !used[key] {
			used[key] = true
			return candidate
		}
	}
}

func monetaryColumn(key string) bool {
	return key == "gross" || key == "fees" || key == "refunds" || key == "adjustments" || key == "net" || strings.Contains(key, "amount") || key == "dep" || key == "wd" || key == "actual_dep" || key == "actual_wd" || key == "opening" || key == "closing" || key == "balance" || key == "delta" || key == "continuity"
}

func writeWorkbook(path string, d, full Data, missing []string, start, end, company string, files map[string]string, options ...Record) error {
	opts := Record{}
	if len(options) > 0 {
		opts = options[0]
	}
	f := excelize.NewFile()
	defer f.Close()
	headStyle, e := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "FFFFFF"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"193B5A"}, Pattern: 1}, Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}})
	if e != nil {
		return e
	}
	moneyStyle, e := f.NewStyle(&excelize.Style{NumFmt: 4})
	if e != nil {
		return e
	}
	textStyle, e := f.NewStyle(&excelize.Style{Alignment: &excelize.Alignment{Vertical: "top", WrapText: true}})
	if e != nil {
		return e
	}
	add := func(name string, cols []column, records []Record) error {
		name = traditional(name)
		var writeErr error
		check := func(err error) {
			if writeErr == nil {
				writeErr = err
			}
		}
		if _, err := f.NewSheet(name); err != nil {
			return err
		}
		for c, col := range cols {
			cell, _ := excelize.CoordinatesToCellName(c+1, 1)
			if err := f.SetCellStr(name, cell, traditional(col.label)); err != nil {
				return err
			}
			letter, _ := excelize.ColumnNumberToName(c + 1)
			width := 22.0
			switch col.key {
			case "date", "period":
				width = 14
			case "ccy", "orig_ccy", "bank_ccy", "page":
				width = 12
			case "details", "documents", "note", "manual_note", "evidence", "text", "source":
				width = 48
			case "vendor", "party", "company":
				width = 36
			case "pdf":
				width = 16
			default:
				if monetaryColumn(col.key) {
					width = 18
				}
			}
			check(f.SetColWidth(name, letter, letter, width))
		}
		last, _ := excelize.CoordinatesToCellName(len(cols), 1)
		check(f.SetCellStyle(name, "A1", last, headStyle))
		check(f.SetRowHeight(name, 1, 32))
		for r, rec := range records {
			for c, col := range cols {
				cell, _ := excelize.CoordinatesToCellName(c+1, r+2)
				v := rec[col.key]
				check(f.SetCellStyle(name, cell, cell, textStyle))
				if col.key == "pdf" || col.key == "bank_pdf" {
					sha := str(rec, "document_sha")
					if col.key == "bank_pdf" {
						sha = str(rec, "bank_document_sha")
					}
					if rel := files[sha]; rel != "" {
						check(f.SetCellStr(name, cell, "打开PDF"))
						check(f.SetCellHyperLink(name, cell, rel, "External"))
					}
					continue
				}
				switch x := v.(type) {
				case string:
					switch col.key {
					case "vendor", "customer", "seller", "buyer", "party", "company", "number", "source", "note", "manual_note", "details", "evidence":
					default:
						x = traditional(x)
					}
					check(f.SetCellStr(name, cell, x))
				case []string:
					check(f.SetCellStr(name, cell, strings.Join(x, ", ")))
				case []any:
					values := []string{}
					for _, v := range x {
						values = append(values, fmt.Sprint(v))
					}
					check(f.SetCellStr(name, cell, strings.Join(values, ", ")))
				case nil:
				case float64:
					check(f.SetCellValue(name, cell, x))
					if monetaryColumn(col.key) {
						check(f.SetCellStyle(name, cell, cell, moneyStyle))
					}
				default:
					check(f.SetCellValue(name, cell, x))
				}
			}
		}
		check(f.SetPanes(name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}))
		if len(records) > 0 {
			last, _ = excelize.CoordinatesToCellName(len(cols), len(records)+1)
			check(f.AutoFilter(name, "A1:"+last, nil))
		}
		check(f.SetSheetDimension(name, "A1:"+last))
		return writeErr
	}
	txRows := []Record{}
	for _, t := range d.Tx {
		r := clone(t)
		if str(t, "manual_category") != "" {
			r["category"] = str(t, "manual_category")
		}
		txRows = append(txRows, r)
	}
	needs := []Record{}
	matchingReview, purposeReview := []Record{}, []Record{}
	for _, t := range d.Tx {
		if str(t, "state") != "银行凭证已具备" && str(t, "state") != "发票已关联（复核分类）" {
			switch str(t, "review_type") {
			case "已找到票據":
				matchingReview = append(matchingReview, t)
			case "董事往來（交會計師分類）", "轉賬／換匯核對", "用途說明", "已有收款通知，待補佣金結算資料":
				purposeReview = append(purposeReview, t)
			default:
				needs = append(needs, t)
			}
		}
	}
	specs := []struct {
		name string
		cols []column
		rows []Record
	}{
		{"待补凭证", []column{{"id", "流水编号"}, {"date", "日期"}, {"ccy", "币种"}, {"dep", "收款"}, {"wd", "付款"}, {"party", "交易对象"}, {"gap_type", "缺票类别"}, {"need", "所需凭证"}, {"documents", "需补说明"}, {"state", "当前状态"}, {"reference_note", "日期差异"}, {"pdf", "月结单PDF"}}, needs},
		{"銀行收款通知", []column{{"date", "日期"}, {"number", "通知編號"}, {"payer", "付款方"}, {"ccy", "幣種"}, {"gross", "原付款"}, {"fees", "銀行費用"}, {"net", "淨入帳"}, {"local_fee_ccy", "本地費用參考幣種"}, {"local_fee", "本地費用參考（不另加）"}, {"pdf", "原件"}}, d.Advices},
		{"已找到票據", []column{{"id", "流水編號"}, {"date", "日期"}, {"ccy", "銀行幣種"}, {"dep", "收款"}, {"wd", "付款"}, {"review_type", "整理結果"}, {"review_reason", "待核對資料"}, {"pdf", "月結單PDF"}}, matchingReview},
		{"往來與用途說明", []column{{"id", "流水編號"}, {"date", "日期"}, {"party", "交易對象"}, {"ccy", "幣種"}, {"dep", "收款"}, {"wd", "付款"}, {"review_type", "整理結果"}, {"review_reason", "說明"}, {"pdf", "月結單PDF"}}, purposeReview},
		{"流水明细", []column{{"id", "流水编号"}, {"date", "日期"}, {"period", "月份"}, {"bank_number", "银行账号"}, {"account", "账户"}, {"ccy", "币种"}, {"dep", "收款"}, {"wd", "付款"}, {"balance", "银行余额"}, {"orig_ccy", "原币"}, {"orig_amount", "原币金额"}, {"fx", "银行汇率"}, {"details", "银行摘要"}, {"category", "建议分类"}, {"manual_note", "复核说明"}, {"state", "凭证状态"}, {"source", "源文件"}, {"page", "页码"}, {"pdf", "月结单PDF"}}, txRows},
		{"银行对账", []column{{"period", "月份"}, {"bank_number", "账号"}, {"account", "账户"}, {"ccy", "币种"}, {"opening", "期初余额"}, {"actual_dep", "提取收款"}, {"dep", "银行收款"}, {"actual_wd", "提取付款"}, {"wd", "银行付款"}, {"closing", "期末余额"}, {"delta", "余额差额"}, {"actual_nd", "提取收款笔数"}, {"nd", "银行收款笔数"}, {"actual_nw", "提取付款笔数"}, {"nw", "银行付款笔数"}, {"continuity", "相邻月差额"}, {"ok", "对账通过"}, {"pdf", "月结单PDF"}}, d.Checks},
		{"待分類發票", []column{{"date", "日期"}, {"number", "發票編號"}, {"seller", "開票方"}, {"buyer", "買方"}, {"ccy", "幣種"}, {"amount", "金額"}, {"direction_basis", "待核對原因"}, {"pdf", "原件"}}, d.Unclassified},
		{"销售发票", []column{{"date", "开票日期"}, {"customer", "客户"}, {"number", "发票编号"}, {"ccy", "币种"}, {"amount", "金额"}, {"service_period", "服务期间"}, {"due_date", "到期日"}, {"duplicate", "重复凭证"}, {"pdf", "原始发票"}}, d.Sales},
		{"平台结算", []column{{"payout_id", "结算编号"}, {"tx", "银行流水"}, {"ccy", "币种"}, {"gross", "销售总额"}, {"fees", "手续费"}, {"refunds", "退款"}, {"adjustments", "调整"}, {"net", "净到账"}, {"status", "核验状态"}, {"reviewer", "复核人"}, {"evidence_sha", "结算报告SHA"}}, d.Platforms},
		{"憑證清單", []column{{"kind", "類型"}, {"name", "檔名"}, {"path", "資料包路徑"}, {"sha", "SHA256"}, {"pdf", "憑證PDF"}}, arr(opts, "manifest")},
		{"自制凭证", []column{{"date", "日期"}, {"kind", "类型"}, {"number", "编号"}, {"company", "公司"}, {"party", "客户/收款方"}, {"ccy", "币种"}, {"amount", "金额"}, {"status", "状态"}, {"tx_id", "关联流水"}, {"purpose", "用途"}, {"note", "备注"}, {"pdf", "PDF"}}, d.Issued},
	}
	for _, s := range specs {
		if len(s.rows) == 0 && (s.name == "自制凭证" || s.name == "平台结算" || s.name == "待分類發票" || s.name == "已找到票據" || s.name == "往來與用途說明" || s.name == "銀行收款通知") {
			continue
		}
		if e = add(s.name, s.cols, s.rows); e != nil {
			return e
		}
	}
	txmap, invmap := map[string]Record{}, map[string]Record{}
	for _, t := range full.Tx {
		txmap[str(t, "id")] = t
	}
	for _, i := range append(append([]Record{}, full.Invoices...), full.Sales...) {
		invmap[str(i, "id")] = i
	}
	invs := []Record{}
	for _, i := range d.Invoices {
		r := clone(i)
		before, during, after := int64(0), int64(0), int64(0)
		for _, m := range full.Matches {
			if str(m, "invoice") != str(i, "id") || str(m, "validity") != "有效确认" {
				continue
			}
			period := str(txmap[str(m, "tx")], "period")
			switch {
			case period < start:
				before += cents(m, "amount")
			case period > end:
				after += cents(m, "amount")
			default:
				during += cents(m, "amount")
			}
		}
		r["before_amount"] = float64(before) / 100
		r["during_amount"] = float64(during) / 100
		r["after_amount"] = float64(after) / 100
		if i["amount"] != nil && str(i, "duplicate") == "" {
			r["remaining_amount"] = float64(cents(i, "amount")-before-during) / 100
		}
		invs = append(invs, r)
	}
	if e = add("发票台账", []column{{"id", "凭证编号"}, {"date", "日期"}, {"vendor", "供应商"}, {"number", "发票编号"}, {"ccy", "币种"}, {"amount", "金额"}, {"duplicate", "重复凭证编号"}, {"before_amount", "周期前已确认"}, {"during_amount", "本周期已确认"}, {"after_amount", "周期后已确认"}, {"remaining_amount", "截至周期末未分配"}, {"note", "备注"}, {"pdf", "发票PDF"}}, invs); e != nil {
		return e
	}
	matches := []Record{}
	for _, m := range d.Matches {
		r := clone(m)
		t, i := txmap[str(m, "tx")], invmap[str(m, "invoice")]
		r["document_sha"] = i["document_sha"]
		r["bank_document_sha"] = t["document_sha"]
		r["payment_date"] = t["date"]
		r["invoice_number"] = i["number"]
		r["counterparty"] = i["vendor"]
		if isSale(i) {
			r["counterparty"] = i["customer"]
		}
		r["bank_ccy"] = t["ccy"]
		r["bank_amount"] = float64(invoiceFlow(t, i)) / 100
		r["direction"] = "付款"
		if isSale(i) {
			r["direction"] = "收款"
		}
		r["orig_ccy"] = t["orig_ccy"]
		r["orig_amount"] = t["orig_amount"]
		r["fx"] = t["fx"]
		matches = append(matches, r)
	}
	if e = add("发票关联", []column{{"direction", "方向"}, {"payment_date", "銀行日期"}, {"invoice_number", "發票編號"}, {"counterparty", "交易對象"}, {"ccy", "發票幣種"}, {"amount", "關聯金額"}, {"bank_ccy", "銀行幣種"}, {"bank_amount", "銀行實際金額"}, {"orig_ccy", "銀行原幣"}, {"orig_amount", "銀行原幣金額"}, {"basis", "匹配依據"}, {"validity", "關聯結果"}, {"pdf", "發票PDF"}, {"bank_pdf", "月結單PDF"}}, matches); e != nil {
		return e
	}
	summary := []Record{}
	ms, _ := months(start, end)
	missingSet := map[string]bool{}
	for _, m := range missing {
		missingSet[m] = true
	}
	for _, m := range ms {
		for _, c := range []string{"HKD", "USD"} {
			var dep, wd int64
			for _, t := range d.Tx {
				if str(t, "period") == m && str(t, "ccy") == c {
					dep += cents(t, "dep")
					wd += cents(t, "wd")
				}
			}
			r := Record{"period": m, "ccy": c, "dep": float64(dep) / 100, "wd": float64(wd) / 100, "net_amount": float64(dep-wd) / 100, "coverage": "已导入"}
			if missingSet[m] {
				r["dep"] = nil
				r["wd"] = nil
				r["net_amount"] = nil
				r["coverage"] = "缺少月结单"
			}
			summary = append(summary, r)
		}
	}
	if e = add("周期汇总", []column{{"period", "月份"}, {"ccy", "币种"}, {"dep", "银行收款"}, {"wd", "银行付款"}, {"net_amount", "净流入"}, {"coverage", "月结单覆盖"}}, summary); e != nil {
		return e
	}
	notes := []Record{{"label": "匯出日期範圍", "text": str(opts, "report_start") + " 至 " + str(opts, "report_end")}, {"label": "公司", "text": company}, {"label": "周期", "text": start + " 至 " + end}, {"label": "缺少月份", "text": strings.Join(missing, ", ")}, {"label": "金额口径", "text": "HKD、USD分别列示。银行收付款包含内部转账和换汇，不等于收入或费用。"}, {"label": "未分配额", "text": "发票金额减截至周期末有效付款分配，仅为关联参考，不是应付账款报表。"}, {"label": "扫描凭证", "text": "可配置腾讯云OCR，失败回退本地；无文字层且未识别的扫描PDF须人工核验。"}, {"label": "复核与会计", "text": "导出为快照。应计、预付、资产折旧、汇兑及税务调整由会计师确定。"}}
	if e = add("交接说明", []column{{"label", "项目"}, {"text", "说明"}}, notes); e != nil {
		return e
	}
	if e = f.DeleteSheet("Sheet1"); e != nil {
		return e
	}
	return f.SaveAs(path)
}
func (s *Store) export(start, end string, allow bool, company string, options ...Record) (string, error) {
	opts := Record{}
	if len(options) > 0 {
		opts = clone(options[0])
	}
	delete(opts, "subsequent_end")
	ms, e := months(start, end)
	if e != nil {
		return "", e
	}
	first, _ := time.Parse("2006-01", start)
	last, _ := time.Parse("2006-01", end)
	opts["report_start"] = first.Format("2006-01-02")
	opts["report_end"] = last.AddDate(0, 1, 0).AddDate(0, 0, -1).Format("2006-01-02")
	withinDates := func(date string) bool {
		return validDate(date) && date >= str(opts, "report_start") && date <= str(opts, "report_end")
	}
	full, e := s.load()
	if e != nil {
		return "", e
	}
	d := emptyData()
	coverage := map[string]bool{}
	required, ids, related := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, c := range full.Checks {
		if str(c, "period") >= start && str(c, "period") <= end {
			if !yes(c, "ok") {
				return "", fmt.Errorf("银行对账失败，停止导出: %s", str(c, "source"))
			}
			d.Checks = append(d.Checks, c)
			coverage[str(c, "period")] = true
			required[str(c, "document_sha")] = true
		}
	}
	if len(d.Checks) == 0 {
		return "", fmt.Errorf("所选周期没有月结单")
	}
	missing := []string{}
	for _, m := range ms {
		if !coverage[m] {
			missing = append(missing, m)
		}
	}
	if len(missing) > 0 && !allow {
		return "", fmt.Errorf("缺少月结单: %s；可明确勾选允许缺月导出", strings.Join(missing, ", "))
	}
	for _, t := range full.Tx {
		if withinDates(str(t, "date")) {
			d.Tx = append(d.Tx, t)
			ids[str(t, "id")] = true
		}
	}
	for _, m := range full.Matches {
		if ids[str(m, "tx")] && str(m, "status") != "不匹配" {
			d.Matches = append(d.Matches, m)
			related[str(m, "invoice")] = true
		}
	}
	selected := map[string]bool{}
	for _, i := range full.Invoices {
		date := str(i, "date")
		if related[str(i, "id")] || date == "" || withinDates(date) {
			d.Invoices = append(d.Invoices, i)
			selected[str(i, "id")] = true
			required[str(i, "document_sha")] = true
		}
	}
	for _, i := range full.Invoices {
		if !selected[str(i, "id")] && selected[str(i, "duplicate")] {
			d.Invoices = append(d.Invoices, i)
			required[str(i, "document_sha")] = true
		}
	}
	for _, i := range full.Unclassified {
		if str(i, "date") != "" && !withinDates(str(i, "date")) {
			continue
		}
		required[str(i, "document_sha")] = true
		d.Unclassified = append(d.Unclassified, i)
	}
	for _, i := range full.Sales {
		if str(i, "duplicate") == "" || related[str(i, "id")] {
			date := str(i, "date")
			if related[str(i, "id")] || date == "" || withinDates(date) {
				d.Sales = append(d.Sales, i)
				required[str(i, "document_sha")] = true
			}
		}
	}
	for _, a := range full.Advices {
		date := str(a, "date")
		if withinDates(date) {
			d.Advices = append(d.Advices, a)
			required[str(a, "document_sha")] = true
		}
	}
	for _, i := range full.Issued {
		date := str(i, "date")
		if ids[str(i, "tx_id")] || withinDates(date) {
			d.Issued = append(d.Issued, i)
			required[str(i, "document_sha")] = true
		}
	}
	for _, p := range full.Platforms {
		if ids[str(p, "tx")] {
			d.Platforms = append(d.Platforms, p)
			required[str(p, "evidence_sha")] = true
		}
	}
	platformPDFs := map[string]bool{}
	for _, doc := range full.Docs {
		if str(doc, "kind") == "平台结算" {
			platformPDFs[str(doc, "sha")] = true
		}
	}
	for _, sha := range texts(opts, "platform_pdf_shas") {
		if !platformPDFs[sha] {
			return "", fmt.Errorf("所选平台报告不存在或类型无效")
		}
		required[sha] = true
	}
	outRoot := filepath.Join(s.root, "导出")
	if e = os.MkdirAll(outRoot, 0755); e != nil {
		return "", e
	}
	companyName := strings.TrimSpace(company)
	if companyName == "" {
		companyName = "未设置公司"
	}
	id := safeName(companyName) + "_會計師資料_" + exportPeriod(start, end)
	stage, e := os.MkdirTemp(outRoot, ".pending-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(stage)
	files := map[string]string{}
	manifest := []Record{}
	usedNames := map[string]bool{}
	sectionNames := map[string]map[string]bool{}
	for _, doc := range full.Docs {
		sha := str(doc, "sha")
		if !required[sha] {
			continue
		}
		path, e := s.documentPath(sha)
		if e != nil {
			return "", e
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return "", e
		}
		if hashBytes(b) != sha {
			return "", fmt.Errorf("凭证SHA256校验失败: %s", str(doc, "name"))
		}
		sub := archiveCategory(str(doc, "kind"))
		if sub == "银行" {
			sub = "银行月结单"
		} else if sub == "发票" {
			sub = "采购发票"
		}
		var rawMeta string
		if e = s.db.QueryRow("SELECT meta FROM documents WHERE sha=?", sha).Scan(&rawMeta); e != nil {
			return "", e
		}
		var meta Record
		if e = json.Unmarshal([]byte(rawMeta), &meta); e != nil {
			return "", e
		}
		rel := filepath.Join(s.archivePeriod(str(doc, "kind"), meta), sub, packagePDFName(doc, path, usedNames))
		if str(opts, "report_start") != "" {
			section := filepath.Join("本期資料", str(opts, "report_start")+"至"+str(opts, "report_end"))
			date := str(meta, "date")
			if str(doc, "kind") == "银行" {
				date = str(meta, "statement_date")
			}
			if str(doc, "kind") == "平台结算" {
				section = "平臺報告"
			} else if validDate(date) && !withinDates(date) {
				section = filepath.Join("跨期關聯資料", s.archivePeriod(str(doc, "kind"), meta))
			} else if !validDate(date) {
				section = "待核對日期資料"
			}
			key := filepath.Join(section, sub)
			if sectionNames[key] == nil {
				sectionNames[key] = map[string]bool{}
			}
			rel = filepath.Join(section, sub, packagePDFName(doc, path, sectionNames[key]))
		}
		if e = os.MkdirAll(filepath.Dir(filepath.Join(stage, rel)), 0755); e != nil {
			return "", e
		}
		if e = os.WriteFile(filepath.Join(stage, rel), b, 0644); e != nil {
			return "", e
		}
		files[sha] = filepath.ToSlash(rel)
		manifest = append(manifest, Record{"kind": str(doc, "kind"), "name": str(doc, "name"), "path": filepath.ToSlash(rel), "sha": sha, "document_sha": sha})
	}
	if len(files) != len(required) {
		return "", fmt.Errorf("部分凭证原件缺失，停止导出")
	}
	opts["manifest"] = manifest
	if e = writeWorkbook(filepath.Join(stage, safeName(companyName)+"_賬務明細_"+exportPeriod(start, end)+".xlsx"), d, full, missing, start, end, company, files, opts); e != nil {
		return "", e
	}
	temp, e := os.CreateTemp(outRoot, ".pending-*.zip")
	if e != nil {
		return "", e
	}
	zipTemp := temp.Name()
	if e = temp.Close(); e != nil {
		os.Remove(zipTemp)
		return "", e
	}
	defer os.Remove(zipTemp)
	if e = zipFolder(stage, zipTemp); e != nil {
		os.Remove(zipTemp)
		return "", e
	}
	return publishArchive(zipTemp, outRoot, id)
}
func zipFolder(dir, dest string) error {
	f, e := os.Create(dest)
	if e != nil {
		return e
	}
	z := zip.NewWriter(f)
	e = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		w, err := z.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(w, src)
		return err
	})
	ze := z.Close()
	fe := f.Close()
	if e != nil {
		return e
	}
	if ze != nil {
		return ze
	}
	return fe
}
func (s *Store) documentPath(sha string) (string, error) {
	if !rx(`^[a-f0-9]{64}$`).MatchString(sha) {
		return "", fmt.Errorf("凭证编号无效")
	}
	var rel string
	if e := s.db.QueryRow("SELECT path FROM documents WHERE sha=?", sha).Scan(&rel); e != nil {
		return "", fmt.Errorf("凭证不存在")
	}
	return withinRoot(s.root, rel)
}
func withinRoot(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("不允许绝对路径")
	}
	path := filepath.Join(root, rel)
	r, e := filepath.Rel(root, path)
	if e != nil || r == ".." || strings.HasPrefix(r, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("文件路径越界")
	}
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return "", e
	}
	resolvedRoot, e := filepath.EvalSymlinks(root)
	if e != nil {
		return "", e
	}
	r, e = filepath.Rel(resolvedRoot, resolved)
	if e != nil || r == ".." || strings.HasPrefix(r, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("文件路径越界")
	}
	return resolved, nil
}

// Full backup includes the SQLite snapshot, settings, allocations and original PDFs.
func (s *Store) backup() (string, error) {
	base := filepath.Join(s.root, "备份")
	if e := os.MkdirAll(base, 0755); e != nil {
		return "", e
	}
	stage, e := os.MkdirTemp(base, ".pending-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(stage)
	snapshot := filepath.Join(stage, "账务.sqlite3")
	if _, e = s.db.Exec("VACUUM INTO ?", snapshot); e != nil {
		return "", e
	}
	d, e := s.load()
	if e != nil {
		return "", e
	}
	sort.Slice(d.Docs, func(i, j int) bool { return str(d.Docs[i], "sha") < str(d.Docs[j], "sha") })
	for _, doc := range d.Docs {
		path, e := s.documentPath(str(doc, "sha"))
		if e != nil {
			return "", e
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return "", e
		}
		if hashBytes(b) != str(doc, "sha") {
			return "", fmt.Errorf("凭证校验失败")
		}
		dest := filepath.Join(stage, str(doc, "path"))
		if e = os.MkdirAll(filepath.Dir(dest), 0755); e != nil {
			return "", e
		}
		if e = os.WriteFile(dest, b, 0644); e != nil {
			return "", e
		}
	}
	draftRoot := filepath.Join(s.root, "董事往來草稿")
	if _, statErr := os.Stat(draftRoot); statErr == nil {
		e = filepath.WalkDir(draftRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("備份草稿不允許符號連結")
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(path), ".pdf") {
				return nil
			}
			rel, err := filepath.Rel(s.root, path)
			if err != nil {
				return err
			}
			dest := filepath.Join(stage, rel)
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err = os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return err
			}
			return os.WriteFile(dest, b, 0644)
		})
		if e != nil {
			return "", e
		}
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	id := "账务备份_" + time.Now().Format("2006-01-02")
	temp, e := os.CreateTemp(base, ".pending-*.zip")
	if e != nil {
		return "", e
	}
	tmp := temp.Name()
	if e = temp.Close(); e != nil {
		os.Remove(tmp)
		return "", e
	}
	defer os.Remove(tmp)
	if e = zipFolder(stage, tmp); e != nil {
		os.Remove(tmp)
		return "", e
	}
	return publishArchive(tmp, base, id)
}
