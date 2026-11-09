package httpapi

import (
	"encoding/json"
	"html/template"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Presentation fields are read from Catalog's existing public Quote, not stored
// as a second quote or approval. The full owner JSON stays in the MCP read.
type quoteReview struct {
	ID                   string `json:"id"`
	Purpose              string `json:"purpose"`
	Status               string `json:"status"`
	ExpiresAt            string `json:"expiresAt"`
	RuntimeVersionID     string `json:"runtimeVersionId"`
	CapabilityVersionID  string `json:"capabilityVersionId"`
	ComputePlanID        string `json:"computePlanId"`
	StoragePlanID        string `json:"storagePlanId"`
	PeriodStart          string `json:"periodStart"`
	PeriodEnd            string `json:"periodEnd"`
	TotalUSDMicros       string `json:"totalUSDMicros"`
	RefundTerms          string `json:"refundTerms"`
	RetentionTerms       string `json:"retentionTerms"`
	ExpectedInterruption string `json:"expectedInterruption"`
	ModelSelections      []struct {
		Slot    string `json:"slot"`
		ModelID string `json:"modelId"`
	} `json:"modelSelections"`
	LineItems []struct {
		Description     string      `json:"description"`
		Quantity        json.Number `json:"quantity"`
		AmountUSDMicros string      `json:"amountUSDMicros"`
	} `json:"lineItems"`
}

func quoteReviewStatus(quote quoteReview, now time.Time) string {
	expires, err := time.Parse(time.RFC3339Nano, quote.ExpiresAt)
	if err != nil {
		return "unknown"
	}
	if quote.Status == "offered" && !now.Before(expires) {
		return "expired"
	}
	switch quote.Status {
	case "offered", "accepted", "expired":
		return quote.Status
	}
	return "unknown"
}

func quoteUSD(micros string) string {
	n, ok := new(big.Int).SetString(micros, 10)
	if !ok || n.Sign() < 0 {
		return "金额暂不可用"
	}
	whole, fraction := new(big.Int), new(big.Int)
	whole.QuoRem(n, big.NewInt(1_000_000), fraction)
	decimal := fraction.String()
	decimal = strings.Repeat("0", 6-len(decimal)) + decimal
	decimal = strings.TrimRight(decimal, "0")
	if decimal == "" {
		decimal = "00"
	} else if len(decimal) == 1 {
		decimal += "0"
	}
	return "$" + whole.String() + "." + decimal
}

var quoteReviewPage = template.Must(template.New("quote-review").Funcs(template.FuncMap{"usd": quoteUSD}).Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>OPL Cloud 报价审阅</title>
<style>body{font:16px system-ui,sans-serif;color:#1d2837;background:#f5f7fa;margin:0;padding:24px}main{max-width:820px;margin:auto;background:white;padding:32px;border-radius:16px}h1{font-size:26px}h2{font-size:18px;margin-top:28px}dl{display:grid;grid-template-columns:150px 1fr;gap:12px}dt{color:#526174}dd{margin:0;overflow-wrap:anywhere}table{width:100%;border-collapse:collapse}td,th{padding:12px;text-align:left;border-bottom:1px solid #ddd}.total{font-size:30px;font-weight:700}.status{padding:12px;background:#eef3fb;border-radius:8px}a{color:#1658a8}code{overflow-wrap:anywhere}footer{margin-top:28px;font-size:14px}@media(max-width:520px){body{padding:12px}main{padding:20px}dl{grid-template-columns:1fr;gap:6px}dd{margin-bottom:10px}}</style></head>
<body><main><h1>报价审阅</h1><p class="status">{{.StatusLabel}}</p><p class="total">{{usd .Quote.TotalUSDMicros}}</p>
<dl><dt>报价编号</dt><dd>{{.Quote.ID}}</dd><dt>用途</dt><dd>{{.Quote.Purpose}}</dd><dt>应用版本</dt><dd>{{if .Quote.RuntimeVersionID}}{{.Quote.RuntimeVersionID}}{{else}}{{.Quote.CapabilityVersionID}}{{end}}</dd><dt>计算套餐</dt><dd>{{.Quote.ComputePlanID}}</dd><dt>存储套餐</dt><dd>{{.Quote.StoragePlanID}}</dd><dt>模型</dt><dd>{{range .Quote.ModelSelections}}{{.Slot}}：{{.ModelID}}<br>{{else}}应用内置配置{{end}}</dd><dt>计费周期</dt><dd>{{.Quote.PeriodStart}} 至 {{.Quote.PeriodEnd}}</dd><dt>报价有效至</dt><dd>{{.Quote.ExpiresAt}}</dd></dl>
<h2>费用明细</h2><table><thead><tr><th>项目</th><th>数量</th><th>金额（美元）</th></tr></thead><tbody>{{range .Quote.LineItems}}<tr><td>{{.Description}}</td><td>{{.Quantity}}</td><td>{{usd .AmountUSDMicros}}</td></tr>{{end}}</tbody></table>
<h2>退款与数据保留</h2><p>{{.Quote.RefundTerms}}</p><p>{{.Quote.RetentionTerms}}</p>{{if .Quote.ExpectedInterruption}}<p>{{.Quote.ExpectedInterruption}}</p>{{end}}
<p>本页用于核对原报价。需要开通或调整时，请进入 Console 的 Workspace 流程完成确认。</p><a href="/console/workspaces">打开 Console</a>
<footer>读取时间：{{.Read.ObservedAt}}<br>版本：<code>{{.Read.Revision}}</code></footer></main></body></html>`))

func serveQuoteReview(routes http.Handler, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'self'; base-uri 'none'; form-action 'none'")
	expected := r.URL.Query().Get("revision")
	if expected == "" {
		writePublisherError(w, r, 400, "revision_required", "open the review path from a fresh Cloud quote read")
		return
	}
	read, code := readAgentOwner(routes, r, "/api/v2/quotes/"+url.PathEscape(r.PathValue("quoteId")))
	if code != http.StatusOK {
		writePublisherError(w, r, code, "quote_read_unavailable", "the original quote could not be read under the current session")
		return
	}
	if read.Revision != expected {
		writePublisherError(w, r, 409, "stale_revision", "the quote changed; read the original quote again before review")
		return
	}
	var quote quoteReview
	if json.Unmarshal(read.Data, &quote) != nil || quote.ID != r.PathValue("quoteId") {
		writePublisherError(w, r, 502, "invalid_quote_readback", "the original quote could not be verified")
		return
	}
	labels := map[string]string{"offered": "报价仍在有效期内；请核对以下金额与条款。", "accepted": "原报价已接受；请回读原 Workspace 操作查看进展。", "expired": "原报价已过期；需要新报价后再作决定。", "unknown": "报价有效状态暂不可用；请从原来源重新读取。"}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = quoteReviewPage.Execute(w, struct {
		Quote       quoteReview
		Read        agentRead
		StatusLabel string
	}{quote, read, labels[quoteReviewStatus(quote, time.Now())]})
}
