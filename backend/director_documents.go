package main

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/phpdave11/gofpdf"
)

func directorName(t Record) string {
	name := str(t, "party")
	for _, suffix := range []string{"（董事本人）", "(董事本人)"} {
		name = strings.TrimSuffix(name, suffix)
	}
	return strings.TrimSpace(name)
}

// Prepare solely from stored bank records; client-supplied amounts are ignored.
func directorDocumentData(d Data, input Record, company string) (Record, error) {
	kind := str(input, "kind")
	if kind != "borrow" && kind != "repay" {
		return nil, fmt.Errorf("請選擇董事借款或公司還款")
	}
	ids := texts(input, "tx_ids")
	if len(ids) == 0 || len(ids) > 100 {
		return nil, fmt.Errorf("請選擇1至100筆董事往來")
	}
	selected := map[string]bool{}
	for _, id := range ids {
		if selected[id] {
			return nil, fmt.Errorf("不可重複選取流水")
		}
		selected[id] = true
	}
	rows := []Record{}
	totals := map[string]int64{}
	name := ""
	for _, t := range d.Tx {
		if !selected[str(t, "id")] {
			continue
		}
		category := str(t, "manual_category")
		if category == "" {
			category = str(t, "category")
		}
		if !strings.Contains(category, "董事") || !validDate(str(t, "date")) || str(t, "ccy") == "" {
			return nil, fmt.Errorf("僅支援已分類的董事往來流水")
		}
		current := directorName(t)
		if current == "" || current == "待确认" || current == "待確認" {
			return nil, fmt.Errorf("流水缺少董事姓名")
		}
		if name != "" && companyIdentity(name) != companyIdentity(current) {
			return nil, fmt.Errorf("每份證明只可包含同一位董事")
		}
		name = current
		amount := cents(t, "dep")
		if kind == "repay" {
			amount = cents(t, "wd")
		}
		if amount <= 0 || (kind == "borrow" && cents(t, "wd") > 0) || (kind == "repay" && cents(t, "dep") > 0) {
			return nil, fmt.Errorf("借款證明須選董事轉入；還款證明須選公司付出")
		}
		rows = append(rows, Record{"id": str(t, "id"), "date": str(t, "date"), "ccy": str(t, "ccy"), "amount": float64(amount) / 100, "bank_number": str(t, "bank_number"), "document_sha": str(t, "document_sha")})
		totals[str(t, "ccy")] += amount
	}
	if len(rows) != len(selected) {
		return nil, fmt.Errorf("所選流水不存在，請刷新後重試")
	}
	sort.Slice(rows, func(i, j int) bool {
		return str(rows[i], "date")+str(rows[i], "id") < str(rows[j], "date")+str(rows[j], "id")
	})
	terms, reference := strings.TrimSpace(str(input, "terms")), strings.TrimSpace(str(input, "loan_reference"))
	if len([]rune(terms)) > 1500 || len([]rune(reference)) > 300 {
		return nil, fmt.Errorf("補充條款或借款參考過長")
	}
	title := "董事借款確認書"
	if kind == "repay" {
		title = "董事借款還款確認書"
	}
	currencies := []string{}
	for c := range totals {
		currencies = append(currencies, c)
	}
	sort.Strings(currencies)
	summary := []string{}
	for _, c := range currencies {
		summary = append(summary, fmt.Sprintf("%s %.2f", c, float64(totals[c])/100))
	}
	return Record{"kind": kind, "title": title, "company": company, "director": name, "rows": rows, "summary": strings.Join(summary, "； "), "terms": terms, "loan_reference": reference, "start": str(rows[0], "date"), "end": str(rows[len(rows)-1], "date"), "generated_on": time.Now().In(time.FixedZone("HKT", 8*3600)).Format("2006-01-02")}, nil
}

func renderDirectorDocument(r Record, fontPath string) ([]byte, error) {
	p := gofpdf.New("P", "mm", "A4", "")
	p.SetMargins(18, 18, 18)
	p.SetAutoPageBreak(true, 20)
	p.AddUTF8Font("Noto", "", fontPath)
	p.SetTitle(str(r, "title")+"（未簽署草稿）", true)
	p.SetAuthor(str(r, "company"), true)
	p.SetFooterFunc(func() {
		p.SetY(-14)
		p.SetFont("Noto", "", 8)
		p.SetTextColor(110, 120, 130)
		p.CellFormat(174, 6, fmt.Sprintf("未簽署草稿 / UNSIGNED DRAFT     %d", p.PageNo()), "", 0, "R", false, 0, "")
	})
	p.AddPage()
	write := func(text string, size float64) {
		p.SetFont("Noto", "", size)
		p.SetTextColor(30, 55, 70)
		p.MultiCell(174, 6, text, "", "L", false)
		p.Ln(3)
	}
	write(str(r, "company"), 15)
	write(str(r, "title"), 19)
	write("未簽署草稿 / UNSIGNED DRAFT", 11)
	write("公司："+str(r, "company")+"\n董事："+str(r, "director")+"\n編製日期："+str(r, "generated_on"), 10)
	statement := "擬確認以下董事轉入公司款項為董事向公司提供的借款。款項性質及條款須由雙方核實並簽署確認。"
	if str(r, "kind") == "repay" {
		statement = "擬確認以下公司付予董事的款項為償還董事借款。須對照原借款記錄並由雙方核實、簽署；本書不聲稱全部借款已清還。"
	}
	write(statement, 10)
	write("銀行轉賬記錄", 12)
	for n, t := range arr(r, "rows") {
		write(fmt.Sprintf("%d. %s    %s %.2f\n銀行賬號：%s\n流水參考：%s", n+1, str(t, "date"), str(t, "ccy"), num(t, "amount"), str(t, "bank_number"), str(t, "id")), 10)
	}
	write("分幣種合計："+str(r, "summary"), 12)
	reference := str(r, "loan_reference")
	if reference == "" {
		reference = "未填寫"
	}
	write("對應借款／協議參考："+reference, 10)
	terms := str(r, "terms")
	if terms == "" {
		terms = "未提供；本草稿不預設利息、到期日或其他借款條件。"
	}
	write("補充條款／說明：\n"+terms, 10)
	if p.GetY() > 215 {
		p.AddPage()
	}
	p.Ln(8)
	write("董事簽署：____________________    日期：________________\n姓名："+str(r, "director"), 10)
	p.Ln(5)
	write("公司授權簽署：________________    日期：________________\n姓名／職銜：________________________________________", 10)
	write("本文件依所選銀行流水編製，不替代銀行憑證；未簽署前僅作草稿，亦不代表會計分類已獲確認。", 9)
	var b bytes.Buffer
	e := p.Output(&b)
	return b.Bytes(), e
}

func (s *Store) generateDirectorDocument(input Record, font string) (Record, error) {
	d, e := s.load()
	if e != nil {
		return nil, e
	}
	cfg, e := s.settings(nil)
	if e != nil {
		return nil, e
	}
	company := str(cfg, "company")
	if company == "" {
		return nil, fmt.Errorf("請先設定公司名稱")
	}
	r, e := directorDocumentData(d, input, company)
	if e != nil {
		return nil, e
	}
	b, e := renderDirectorDocument(r, font)
	if e != nil {
		return nil, e
	}
	year := str(r, "start")[:4]
	folder := filepath.Join(s.root, "董事往來草稿", year)
	if e = os.MkdirAll(folder, 0755); e != nil {
		return nil, e
	}
	stem := str(r, "start") + "至" + str(r, "end") + "_" + str(r, "title") + "_" + safeName(str(r, "director")) + "_草稿"
	name := ""
	for n := 1; n < 10000; n++ {
		name = stem + ".pdf"
		if n > 1 {
			name = fmt.Sprintf("%s（%d）.pdf", stem, n)
		}
		path := filepath.Join(folder, name)
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b) {
			break
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		_, err = f.Write(b)
		closeErr := f.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		break
	}
	return Record{"name": name, "url": "/api/director-drafts/" + year + "/" + url.PathEscape(name), "status": "未簽署草稿", "count": len(arr(r, "rows")), "summary": str(r, "summary")}, nil
}
