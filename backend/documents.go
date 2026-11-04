package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/phpdave11/gofpdf"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func lineItems(text string) ([]Record, int64, error) {
	items := []Record{}
	var total int64
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		p := strings.Split(line, "|")
		if len(p) != 3 {
			return nil, 0, fmt.Errorf("明细每行按“项目 | 数量 | 单价”填写")
		}
		label := strings.TrimSpace(p[0])
		if len([]rune(label)) > 500 || !rx(`^\d{1,12}(\.\d{1,4})?$`).MatchString(strings.TrimSpace(p[1])) || !rx(`^\d{1,12}(\.\d{1,4})?$`).MatchString(strings.TrimSpace(p[2])) {
			return nil, 0, fmt.Errorf("項目最多500字；數量及單價最多4位小數")
		}
		q, ok := new(big.Rat).SetString(strings.TrimSpace(p[1]))
		price, okp := new(big.Rat).SetString(strings.TrimSpace(p[2]))
		if label == "" || !ok || !okp || q.Sign() <= 0 || price.Sign() < 0 {
			return nil, 0, fmt.Errorf("项目、数量或单价不正确")
		}
		product := new(big.Rat).Mul(q, price)
		v, e := money(product.FloatString(12))
		if e != nil {
			return nil, 0, e
		}
		if total > 9000000000000000-v {
			return nil, 0, fmt.Errorf("發票合計超出支援範圍")
		}
		total += v
		pf, _ := price.Float64()
		if math.IsInf(pf, 0) || math.IsNaN(pf) || pf > 999999999999 {
			return nil, 0, fmt.Errorf("单价超出支持范围")
		}
		items = append(items, Record{"description": label, "quantity": strings.TrimSpace(p[1]), "unit_price": pf, "unit_price_text": formatUnitPrice(price), "amount": float64(v) / 100})
	}
	if len(items) > 100 || len(items) == 0 || total <= 0 {
		return nil, 0, fmt.Errorf("需要1至100项明细，合计必须大于零")
	}
	return items, total, nil
}
func renderDocument(path string, r Record, fontPath string) error {
	p := gofpdf.New("P", "mm", "A4", "")
	p.SetMargins(16, 16, 16)
	p.SetAutoPageBreak(true, 20)
	p.AddUTF8Font("Noto", "", fontPath)
	p.SetTitle(traditional(str(r, "kind"))+" "+str(r, "number"), true)
	p.SetAuthor(str(r, "company"), true)
	p.SetFooterFunc(func() {
		p.SetY(-14)
		p.SetFont("Noto", "", 8)
		p.SetTextColor(100, 110, 120)
		p.CellFormat(178, 6, str(r, "number")+fmt.Sprintf("    第 %d 頁", p.PageNo()), "", 0, "R", false, 0, "")
	})
	p.SetHeaderFunc(func() {
		if str(r, "status") == "草稿" {
			p.SetFont("Noto", "", 36)
			p.SetTextColor(230, 233, 236)
			p.TransformBegin()
			p.TransformRotate(35, 105, 150)
			p.Text(50, 150, "草稿 / DRAFT")
			p.TransformEnd()
		}
	})
	p.AddPage()
	write := func(text string, size float64) {
		if strings.TrimSpace(text) == "" {
			return
		}
		p.SetFont("Noto", "", size)
		p.SetTextColor(25, 59, 90)
		p.MultiCell(178, 6, text, "", "L", false)
		p.Ln(2)
	}
	write(str(r, "company"), 17)
	write(str(r, "company_address"), 9)
	write(str(r, "company_email"), 9)
	if str(r, "business_number") != "" {
		write("商業登記號碼 / BR No.: "+str(r, "business_number"), 9)
	}
	title := "銷售發票 / INVOICE"
	if str(r, "kind") == "内部支出凭证" {
		title = "內部支出憑證 / PAYMENT VOUCHER"
	}
	write(title, 16)
	write("編號 / No.: "+str(r, "number")+"    日期 / Date: "+str(r, "date"), 10)
	write("狀態: "+traditional(str(r, "status"))+"    幣種 / Currency: "+str(r, "ccy"), 9)
	if str(r, "kind") == "销售发票" {
		write("客戶 / Bill to: "+str(r, "party"), 11)
	} else {
		write("實際收款方 / Payee: "+str(r, "party"), 11)
	}
	if str(r, "kind") == "销售发票" {
		write(str(r, "address"), 9)
		if str(r, "service_period") != "" {
			write("服務期間 / Service period: "+str(r, "service_period"), 9)
		}
		if str(r, "po_number") != "" {
			write("客戶訂單 / PO: "+str(r, "po_number"), 9)
		}
		widths := []float64{92, 22, 30, 34}
		header := func() {
			p.SetFont("Noto", "", 9)
			p.SetTextColor(255, 255, 255)
			p.SetFillColor(25, 59, 90)
			for n, label := range []string{"項目 / Description", "數量 / Qty", "單價 / Rate", "金額 / Amount"} {
				p.CellFormat(widths[n], 9, label, "", 0, "L", true, 0, "")
			}
			p.Ln(9)
		}
		header()
		for _, item := range arr(r, "items") {
			p.SetFont("Noto", "", 9)
			lines := p.SplitText(str(item, "description"), 88)
			h := float64(len(lines))*6 + 4
			if h < 10 {
				h = 10
			}
			if p.GetY()+h > 273 {
				p.AddPage()
				header()
			}
			x, y := p.GetXY()
			p.SetTextColor(35, 50, 65)
			p.SetXY(x+2, y+2)
			p.MultiCell(88, 6, str(item, "description"), "", "L", false)
			p.SetXY(x+92, y)
			for n, value := range []string{str(item, "quantity"), str(item, "unit_price_text"), fmt.Sprintf("%.2f", num(item, "amount"))} {
				p.CellFormat(widths[n+1], h, value, "", 0, "R", false, 0, "")
			}
			p.SetDrawColor(220, 226, 230)
			p.Line(x, y+h, x+178, y+h)
			p.SetXY(x, y+h)
		}
		p.Ln(5)
		p.SetFont("Noto", "", 15)
		p.SetTextColor(25, 59, 90)
		p.CellFormat(178, 10, fmt.Sprintf("合計 / TOTAL  %s %.2f", str(r, "ccy"), num(r, "amount")), "", 1, "R", false, 0, "")
		p.Ln(3)
		if str(r, "due_date") != "" {
			write("到期日期 / Due date: "+str(r, "due_date"), 10)
		}
		if str(r, "payment_terms") != "" {
			write("付款條款 / Terms: "+str(r, "payment_terms"), 9)
		}
		if str(r, "payment_details") != "" {
			write("收款資料 / Payment instructions", 10)
			write(str(r, "payment_details"), 9)
		}
	} else {
		tx, _ := r["transaction"].(Record)
		if tx == nil {
			if m, ok := r["transaction"].(map[string]any); ok {
				tx = Record(m)
			}
		}
		write("真實用途: "+str(r, "purpose"), 11)
		write(fmt.Sprintf("實際銀行扣款: %s %.2f", str(r, "ccy"), num(r, "amount")), 16)
		write("流水編號: "+str(tx, "id"), 9)
		write("銀行賬號: "+str(tx, "bank_number")+" / "+str(tx, "account"), 9)
		write("銀行摘要: "+str(tx, "details"), 9)
		write(fmt.Sprintf("原幣參考: %s %.2f", str(tx, "orig_ccy"), num(tx, "orig_amount")), 9)
		write("經辦 / 覆核人: "+str(r, "prepared_by"), 9)
		write("本憑證僅記錄真實付款，不能代替供應商發票或正式收據；外部票據仍須補齊。", 9)
	}
	if str(r, "note") != "" {
		write("備註 / Notes: "+str(r, "note"), 9)
	}
	if e := p.Error(); e != nil {
		return e
	}
	return p.OutputFileAndClose(path)
}

func (s *Store) generate(kind string, v Record, fontPath string, preview ...bool) (Record, error) {
	if kind != "销售发票" && kind != "内部支出凭证" {
		return nil, fmt.Errorf("凭证类型无效")
	}
	d, e := s.load()
	if e != nil {
		return nil, e
	}
	date := str(v, "date")
	var t Record
	if kind == "内部支出凭证" {
		for _, x := range d.Tx {
			if str(x, "id") == str(v, "tx_id") && cents(x, "wd") > 0 {
				t = x
				break
			}
		}
		if t == nil {
			return nil, fmt.Errorf("请选择实际银行付款流水")
		}
		date = str(t, "date")
	}
	if e = s.checkEditable(date); e != nil {
		return nil, e
	}
	company := strings.TrimSpace(str(v, "company"))
	status := str(v, "status")
	if company == "" || !validDate(date) || strings.TrimSpace(str(v, "party")) == "" {
		return nil, fmt.Errorf("公司、客户/收款方和有效日期为必填项")
	}
	if status != "草稿" && status != "正式" {
		return nil, fmt.Errorf("状态应为草稿或正式")
	}
	if status == "正式" && kind == "销售发票" && strings.TrimSpace(str(v, "reviewer")) == "" {
		return nil, fmt.Errorf("正式销售发票请填写复核人")
	}
	if status == "正式" && !yes(v, "confirmed") {
		return nil, fmt.Errorf("正式文件需要确认真实交易资料已核对")
	}
	number := strings.TrimSpace(str(v, "number"))
	if number == "" {
		prefix := "SI-"
		if status == "草稿" && kind == "销售发票" {
			prefix = "DRAFT-"
		}
		if kind == "内部支出凭证" {
			prefix = "EV-"
		}
		prefix += strings.ReplaceAll(date[:7], "-", "") + "-"
		max := 0
		for _, x := range d.Issued {
			if str(x, "kind") == kind && strings.HasPrefix(str(x, "number"), prefix) {
				n, _ := strconv.Atoi(strings.TrimPrefix(str(x, "number"), prefix))
				if n > max {
					max = n
				}
			}
		}
		number = prefix + fmt.Sprintf("%03d", max+1)
	}
	for _, x := range d.Issued {
		if str(x, "kind") == kind && str(x, "number") == number && str(x, "company") == company {
			return nil, fmt.Errorf("该编号已使用")
		}
	}
	r := Record{"id": "DOC-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12], "kind": kind, "company": company, "date": date, "number": number, "status": status, "note": str(v, "note"), "tx_id": "", "party": strings.TrimSpace(str(v, "party")), "created_at": time.Now().Format(time.RFC3339)}
	if kind == "销售发票" {
		ccy := str(v, "ccy")
		if ccy != "HKD" && ccy != "USD" {
			return nil, fmt.Errorf("仅支持HKD和USD")
		}
		items, total, e := lineItems(str(v, "items"))
		if e != nil {
			return nil, e
		}
		due := str(v, "due_date")
		if due != "" && (!validDate(due) || due < date) {
			return nil, fmt.Errorf("到期日期无效")
		}
		r["origin"] = "generated"
		r["reviewer"] = str(v, "reviewer")
		r["customer"] = r["party"]
		r["verified"] = status == "正式"
		r["verification_source"] = "human_generation"
		r["items"] = items
		r["amount"] = float64(total) / 100
		r["ccy"] = ccy
		r["address"] = str(v, "address")
		r["due_date"] = due
		r["payment_details"] = str(v, "payment_details")
		for _, k := range []string{"company_address", "company_email", "business_number", "service_period", "po_number", "payment_terms"} {
			r[k] = str(v, k)
		}
	} else {
		if strings.TrimSpace(str(v, "purpose")) == "" || strings.TrimSpace(str(v, "prepared_by")) == "" {
			return nil, fmt.Errorf("请填写真实用途及经办/复核人")
		}
		r["tx_id"] = str(t, "id")
		r["transaction"] = t
		r["ccy"] = t["ccy"]
		r["amount"] = t["wd"]
		r["purpose"] = str(v, "purpose")
		r["prepared_by"] = str(v, "prepared_by")
	}
	tmp, e := os.CreateTemp(s.root, "generated-*.pdf")
	if e != nil {
		return nil, e
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if e = renderDocument(tmp.Name(), r, fontPath); e != nil {
		return nil, e
	}
	b, e := os.ReadFile(tmp.Name())
	if e != nil {
		return nil, e
	}
	if len(preview) > 0 && preview[0] {
		return Record{"pdf_base64": base64.StdEncoding.EncodeToString(b)}, nil
	}
	sha := hashBytes(b)
	r["hash"] = sha
	rel, e := s.archivePath(kind, r, sha, nil)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(filepath.Dir(filepath.Join(s.root, rel)), 0755); e != nil {
		return nil, e
	}
	if e = os.WriteFile(filepath.Join(s.root, rel), b, 0644); e != nil {
		return nil, e
	}
	canonicalMetadata(r, filepath.Base(rel))
	meta, _ := json.Marshal(r)
	if _, e = s.db.Exec("INSERT INTO documents VALUES(?,?,?,?,?)", sha, kind, filepath.Base(rel), rel, string(meta)); e != nil {
		return nil, e
	}
	r["document_sha"] = sha
	r["url"] = "/api/documents/" + sha
	return r, nil
}

func formatUnitPrice(p *big.Rat) string {
	s := p.FloatString(4)
	for strings.HasSuffix(s, "0") && len(strings.SplitN(s, ".", 2)[1]) > 2 {
		s = strings.TrimSuffix(s, "0")
	}
	return s
}
