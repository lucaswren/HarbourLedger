package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	pdf "github.com/ledongthuc/pdf"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// TencentOCR uses only Tencent's HTTPS endpoint. Keys stay in server memory;
// they are never exposed in settings, responses, logs or SQLite records.
type TencentOCR struct {
	secretID, secretKey, token, region string
	client                             *http.Client
	endpoint                           string
}

func newTencentOCR(values map[string]string) *TencentOCR {
	return &TencentOCR{secretID: values["TENCENTCLOUD_SECRET_ID"], secretKey: values["TENCENTCLOUD_SECRET_KEY"], token: values["TENCENTCLOUD_TOKEN"], region: values[ocrRegionKey], client: &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}, endpoint: "https://ocr.tencentcloudapi.com/"}
}
func (c *TencentOCR) configured() bool { return c != nil && c.secretID != "" && c.secretKey != "" }
func (c *TencentOCR) status() Record {
	configured := c.configured()
	mode := "本地PDF解析"
	invoiceMode := "local"
	if configured {
		mode = "银行月结单仅本地解析；文字型发票本地解析，仅扫描型/含扫描页发票使用腾讯云OCR"
		invoiceMode = "local_text_cloud_scanned"
	}
	partial := c != nil && (c.secretID != "" || c.secretKey != "") && !configured
	region := ""
	if c != nil {
		region = c.region
	}
	return Record{"configured": configured, "mode": mode, "region": region, "partial_configuration": partial, "provider": "tencent", "bank_mode": "local_only", "invoice_mode": invoiceMode, "local_scanned_pdf": "manual_review", "max_cloud_pdf_pages": 30}
}
func hmacSHA(key []byte, s string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(s))
	return h.Sum(nil)
}
func tcAuthorization(id, key, host, action string, payload []byte, now time.Time) string {
	date := now.UTC().Format("2006-01-02")
	scope := date + "/ocr/tc3_request"
	canonicalHeaders := "content-type:application/json; charset=utf-8\nhost:" + host + "\nx-tc-action:" + strings.ToLower(action) + "\n"
	signed := "content-type;host;x-tc-action"
	canonical := "POST\n/\n\n" + canonicalHeaders + "\n" + signed + "\n" + hashBytes(payload)
	toSign := "TC3-HMAC-SHA256\n" + strconv.FormatInt(now.Unix(), 10) + "\n" + scope + "\n" + hashBytes([]byte(canonical))
	dateKey := hmacSHA([]byte("TC3"+key), date)
	serviceKey := hmacSHA(dateKey, "ocr")
	signingKey := hmacSHA(serviceKey, "tc3_request")
	signature := hex.EncodeToString(hmacSHA(signingKey, toSign))
	return "TC3-HMAC-SHA256 Credential=" + id + "/" + scope + ", SignedHeaders=" + signed + ", Signature=" + signature
}
func (c *TencentOCR) call(ctx context.Context, action string, input Record) (Record, error) {
	payload, e := json.Marshal(input)
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if e != nil {
		return nil, fmt.Errorf("腾讯云请求初始化失败")
	}
	now := time.Now()
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Version", "2018-11-19")
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(now.Unix(), 10))
	if c.region != "" {
		req.Header.Set("X-TC-Region", c.region)
	}
	if c.token != "" {
		req.Header.Set("X-TC-Token", c.token)
	}
	req.Header.Set("Authorization", tcAuthorization(c.secretID, c.secretKey, req.URL.Host, action, payload, now))
	resp, e := c.client.Do(req)
	if e != nil {
		return nil, fmt.Errorf("腾讯云连接失败或超时")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("腾讯云HTTP状态 %d", resp.StatusCode)
	}
	var envelope struct{ Response Record }
	dec := json.NewDecoder(io.LimitReader(resp.Body, 16<<20))
	if e = dec.Decode(&envelope); e != nil || envelope.Response == nil {
		return nil, fmt.Errorf("腾讯云返回无效JSON")
	}
	if v, ok := envelope.Response["Error"].(map[string]any); ok {
		code, _ := v["Code"].(string)
		if !rx(`^[A-Za-z0-9._]+$`).MatchString(code) {
			code = "ServiceError"
		}
		return nil, fmt.Errorf("腾讯云OCR失败: %s（RequestId: %s）", code, str(envelope.Response, "RequestId"))
	}
	return envelope.Response, nil
}
func (c *TencentOCR) recognize(ctx context.Context, path, name, sha, kind string) (Record, error) {
	if kind != "发票" && kind != "销售发票" {
		return nil, fmt.Errorf("银行月结单仅使用本地解析，不调用腾讯云OCR")
	}
	kindPDF, typeErr := detectPDFType(path)
	if typeErr != nil {
		return nil, typeErr
	}
	if !needsCloudPDF(kindPDF) {
		return nil, fmt.Errorf("文字型或无法确认的PDF不调用腾讯云OCR")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	if base64.StdEncoding.EncodedLen(len(b)) > 10*1024*1024 {
		return nil, fmt.Errorf("PDF的Base64超过腾讯云10MB限制")
	}
	f, r, e := pdf.Open(path)
	if e != nil {
		return nil, fmt.Errorf("无法读取PDF页数: %w", e)
	}
	pages := r.NumPage()
	f.Close()
	if pages < 1 || pages > 30 {
		return nil, fmt.Errorf("云端识别限制1至30页，当前%d页", pages)
	}
	encoded := base64.StdEncoding.EncodeToString(b)
	response, e := c.call(ctx, "RecognizeGeneralInvoice", Record{"ImageBase64": encoded, "EnablePdf": true, "EnableMultiplePage": true, "EnableOther": true})
	if e != nil {
		return nil, e
	}
	return cloudInvoice(response, name, sha, pages)
}
func cloudInvoice(response Record, name, sha string, pages int) (Record, error) {
	items := arr(response, "MixedInvoiceItems")
	if len(items) != 1 {
		return nil, fmt.Errorf("识别到%d张票据，请每份PDF仅保留一张发票并人工核验", len(items))
	}
	item := items[0]
	if code := str(item, "Code"); code != "" && code != "OK" {
		return nil, fmt.Errorf("腾讯云票据识别未成功")
	}
	infos, ok := item["SingleInvoiceInfos"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("腾讯云未返回发票字段")
	}
	i := Record{"id": "INV-" + sha[:12], "hash": sha, "source": name, "pages": pages, "date": "", "vendor": "", "number": "", "ccy": "", "amount": nil, "accounts": []string{}, "verified": false, "method": "腾讯云票据OCR，待人工核验", "amount_basis": "腾讯云结构化字段", "note": "云端识别结果，请核对原件后确认", "recognition_provider": "tencent", "ocr_request_id": str(response, "RequestId")}
	fields := map[string]string{}
	var flatten func(any)
	flatten = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if name, ok := x["Name"].(string); ok {
				if value, ok := x["Value"].(string); ok {
					fields[strings.ToLower(strings.ReplaceAll(name, " ", ""))] = value
				}
			}
			for k, v := range x {
				if s, ok := v.(string); ok {
					fields[strings.ToLower(k)] = s
				} else {
					flatten(v)
				}
			}
		case []any:
			for _, v := range x {
				flatten(v)
			}
		}
	}
	for k, v := range infos {
		if v != nil { // Reject bank slips and receipts masquerading as supplier invoices.
			if k == "BankSlip" || k == "ShoppingReceipt" {
				return nil, fmt.Errorf("识别到银行回单或小票，请人工录入核验")
			}
			flatten(v)
		}
	}
	first := func(keys ...string) string {
		for _, k := range keys {
			if s := fields[k]; strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		return ""
	}
	i["customer"] = first("buyername", "buyer", "customername", "购买方名称", "購買方名稱", "客户名称", "客戶名稱")
	i["date"] = parseDate(first("invoicedate", "date", "开票日期", "发票日期", "發票日期"))
	i["vendor"] = first("seller", "vendorname", "suppliername", "销售方名称", "銷售方名稱", "供应商", "供應商", "卖方名称", "卖方", "開票方名稱", "开票方名称", "sellername")
	i["number"] = first("invoicenumber", "invoiceno", "number", "发票号码", "发票编号", "發票號碼", "發票編號", "票据号码")
	ccy := strings.ToUpper(first("currency", "currencycode", "币种", "幣種", "货币", "貨幣"))
	if strings.Contains(ccy, "USD") || ccy == "US$" || ccy == "美元" {
		i["ccy"] = "USD"
	} else if strings.Contains(ccy, "HKD") || ccy == "HK$" || ccy == "港币" || ccy == "港幣" {
		i["ccy"] = "HKD"
	}
	amount := first("invoicetotal", "totalamount", "amountdue", "total", "价税合计(小写)", "价税合计", "總金額", "总金额", "合计金额", "金额", "发票金额")
	clean := rx(`(?i)USD|HKD|US\$|HK\$|[$,\s]`).ReplaceAllString(amount, "")
	if v, e := money(clean); e == nil && v > 0 {
		i["amount"] = float64(v) / 100
	}
	accounts := []string{}
	for _, value := range fields {
		accounts = append(accounts, rx(`\b\d{3}-\d{6}-\d{3}\b`).FindAllString(value, -1)...)
	}
	i["accounts"] = accounts
	if str(i, "vendor") == "" && str(i, "number") == "" && i["amount"] == nil {
		return nil, fmt.Errorf("腾讯云未提取有效发票字段")
	}
	return i, nil
}
