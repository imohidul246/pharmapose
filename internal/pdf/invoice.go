//go:build !test

package pdf

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/signintech/gopdf"

	"github.com/mohi/pms-marg-inspired/internal/repository"
)

const (
	marginLeft   = 15.0
	marginRight  = 15.0
	pageWidth    = 210.0 // A4 mm
	contentWidth = pageWidth - marginLeft - marginRight
	fontRegular  = "noto"
	fontBold     = "noto-bold"

	accentR = 30
	accentG = 64
	accentB = 75
	goldR   = 201
	goldG   = 162
	goldB   = 52

	// lineHeightMM approximates the on-page line height for a font size in
	// points (1pt = 0.3528mm, ~1.3x typographic line box).
)

func lineHeightMM(pts float64) float64 { return pts * 0.3528 * 1.3 }

type SellerInfo struct {
	Name      string
	Address   string
	GSTIN     string
	PAN       string
	Phone     string
	StateCode string
	StateName string
}

type BuyerInfo struct {
	Name      string
	GSTIN     string
	PAN       string
	Address   string
	Phone     string
	StateCode string
	StateName string
}

type InvoiceData struct {
	Invoice repository.SalesInvoiceDetail
	Seller  SellerInfo
	Buyer   BuyerInfo
}

func GenerateInvoicePDF(w io.Writer, data InvoiceData) error {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{
		PageSize: gopdf.Rect{W: pageWidth, H: 297.0}, // A4, mm
		Unit:     gopdf.UnitMM,
	})

	if err := loadFonts(pdf); err != nil {
		return fmt.Errorf("load fonts: %w", err)
	}

	pdf.AddPage()

	y := drawHeaderBand(pdf, data)
	y = drawParties(pdf, data, y)
	y = drawItemTable(pdf, data, y)
	y = drawSummary(pdf, data, y)
	y = drawAmountInWords(pdf, data, y)
	drawFooter(pdf, data, y)

	_, err := pdf.WriteTo(w)
	return err
}

func loadFonts(pdf *gopdf.GoPdf) error {
	// Resolve font files robustly regardless of the working directory (repo
	// root for the server binary, package dir for tests).
	candidates := []string{
		"internal/pdf/fonts/NotoSans-Regular.ttf",
		"fonts/NotoSans-Regular.ttf",
	}
	reg, err := findFontFile(candidates)
	if err != nil {
		return err
	}
	if err := pdf.AddTTFFont(fontRegular, reg); err != nil {
		return err
	}
	return pdf.AddTTFFont(fontBold, replaceSuffix(reg, "-Regular", "-Bold"))
}

func findFontFile(candidates []string) (string, error) {
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("unable to locate font file (tried %v)", candidates)
}

func replaceSuffix(s, old, new string) string {
	// old is the "-Regular.ttf" segment; produce the "-Bold.ttf" path.
	return strings.Replace(s, old+".ttf", new+".ttf", 1)
}

func setFont(pdf *gopdf.GoPdf, size float64) {
	pdf.SetFont(fontRegular, "", size)
}

func setFontBold(pdf *gopdf.GoPdf, size float64) {
	pdf.SetFont(fontBold, "", size)
}

func measureText(pdf *gopdf.GoPdf, text string) float64 {
	w, _ := pdf.MeasureTextWidth(text)
	return w
}

// setFillSolid fills a rectangle with the current fill color.
func fillRect(pdf *gopdf.GoPdf, x, y, w, h float64) {
	pdf.Rectangle(x, y, x+w, y+h, "F", 0, 0)
}

func strokeRect(pdf *gopdf.GoPdf, x, y, w, h float64) {
	pdf.SetStrokeColor(150, 160, 165)
	pdf.SetLineWidth(0.25)
	pdf.Rectangle(x, y, x+w, y+h, "D", 0, 0)
}

// drawTextTop renders a single line whose top edge is at (top).
func drawTextTop(pdf *gopdf.GoPdf, x, y float64, text string) {
	pdf.SetXY(x, y)
	pdf.Cell(nil, text)
}

// drawRightAligned renders text whose right edge is at (rightX, top).
func drawRightAligned(pdf *gopdf.GoPdf, rightX, top float64, text string) {
	tw := measureText(pdf, text)
	pdf.SetXY(rightX-tw, top)
	pdf.Cell(nil, text)
}

// fitFont shrinks the active font until text fits maxWidth. bold selects
// whether the bold variant is resized.
func fitFont(pdf *gopdf.GoPdf, text string, maxWidth, startSize, minSize float64, bold bool) float64 {
	size := startSize
	for size > minSize && measureText(pdf, text) > maxWidth {
		size -= 0.5
		if bold {
			setFontBold(pdf, size)
		} else {
			setFont(pdf, size)
		}
	}
	return size
}

// wrapLines breaks text into lines that fit maxWidth (using the current font).
// It respects word breaks and caps the result at maxLines.
func wrapLines(pdf *gopdf.GoPdf, text string, maxWidth float64, maxLines int) []string {
	if strings.TrimSpace(text) == "" {
		return []string{""}
	}
	if maxWidth <= 0 || maxLines <= 0 {
		return []string{""}
	}

	var out []string
	appendLine := func(line string) bool {
		out = append(out, line)
		return len(out) >= maxLines
	}

	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			if len(out) < maxLines {
				out = append(out, "")
			}
			continue
		}

		cur := ""
		for _, word := range words {
			// A normal word fits either on the current line or, when the current
			// line is empty, as a standalone token.
			if cur == "" {
				if measureText(pdf, word) <= maxWidth {
					cur = word
					continue
				}

				// The token itself is too long (e.g. a long SKU/medicine name with
				// no spaces). Split it by rune so it cannot overflow the cell.
				chunks := splitLongToken(pdf, word, maxWidth)
				for i, chunk := range chunks {
					if i == len(chunks)-1 {
						cur = chunk
						break
					}
					if appendLine(chunk) {
						break
					}
				}
				if len(out) >= maxLines {
					break
				}
				continue
			}

			try := cur + " " + word
			if measureText(pdf, try) <= maxWidth {
				cur = try
				continue
			}

			if appendLine(cur) {
				break
			}

			if measureText(pdf, word) <= maxWidth {
				cur = word
				continue
			}

			chunks := splitLongToken(pdf, word, maxWidth)
			for i, chunk := range chunks {
				if i == len(chunks)-1 {
					cur = chunk
					break
				}
				if appendLine(chunk) {
					break
				}
			}
			if len(out) >= maxLines {
				break
			}
		}

		if len(out) >= maxLines {
			break
		}
		if cur != "" {
			out = append(out, cur)
		}
	}

	if len(out) == 0 {
		out = []string{""}
	}

	if len(out) > maxLines {
		out = out[:maxLines]
	}

	// Add an ellipsis when the content did not fully fit.
	if len(out) == maxLines && strings.TrimSpace(strings.Join(out, " ")) != strings.TrimSpace(text) {
		last := out[maxLines-1]
		for strings.HasSuffix(last, ".") {
			last = strings.TrimSuffix(last, ".")
		}
		ellipsis := "..."
		for last != "" && measureText(pdf, last+ellipsis) > maxWidth {
			r := []rune(last)
			last = string(r[:len(r)-1])
		}
		out[maxLines-1] = strings.TrimSpace(last) + ellipsis
	}

	return out
}

// splitLongToken breaks a single token into chunks that each fit maxWidth.
// It works with runes so UTF-8 medicine names remain valid strings.
func splitLongToken(pdf *gopdf.GoPdf, token string, maxWidth float64) []string {
	if token == "" || measureText(pdf, token) <= maxWidth {
		return []string{token}
	}

	runes := []rune(token)
	chunks := make([]string, 0, 2)
	current := make([]rune, 0, len(runes))

	for _, r := range runes {
		candidate := append(current, r)
		candidateText := string(candidate)
		if len(current) > 0 && measureText(pdf, candidateText) > maxWidth {
			chunks = append(chunks, string(current))
			current = current[:0]
			current = append(current, r)
			continue
		}
		current = candidate
	}

	if len(current) > 0 {
		chunks = append(chunks, string(current))
	}
	return chunks
}

// drawWrapped renders pre-wrapped lines top-aligned inside a box (used for
// the medicine-name and tax cells so multi-line content stays anchored to the
// top of the row instead of floating when the row grows tall).
func drawWrapped(pdf *gopdf.GoPdf, x, y, w, h float64, lines []string, align string) {
	if len(lines) == 0 {
		return
	}
	lh := lineHeightMM(7)
	top := y + 1.0
	for i, line := range lines {
		tw := measureText(pdf, line)
		tx := x
		switch align {
		case "R":
			tx = x + w - tw
		case "C":
			tx = x + (w-tw)/2
		}
		if tw > w {
			tx = x
		}
		pdf.SetXY(tx, top+float64(i)*lh)
		pdf.Cell(nil, line)
	}
}

// drawCellValue draws one single-line value inside a bordered box, auto
// shrinking the font if the text would overflow the column. Text is drawn
// after the border so it always stays on top.
func drawCellValue(pdf *gopdf.GoPdf, x, y, w, h float64, text, align string, bold bool, size float64) {
	if text == "" {
		return
	}
	if bold {
		setFontBold(pdf, size)
	} else {
		setFont(pdf, size)
	}
	for size > 4.5 && measureText(pdf, text) > w-1.5 {
		size -= 0.5
		if bold {
			setFontBold(pdf, size)
		} else {
			setFont(pdf, size)
		}
	}
	lh := size * 0.45
	top := y + (h-lh)/2
	tw := measureText(pdf, text)
	tx := x + 0.8
	switch align {
	case "R":
		tx = x + w - tw - 0.8
	case "C":
		tx = x + (w-tw)/2
	}
	if tw > w {
		tx = x + 0.8
	}
	pdf.SetXY(tx, top)
	pdf.Cell(nil, text)
}

// drawCellValueTop draws one single-line value top-aligned inside a bordered
// box, auto-shrinking the font if the text would overflow the column. Used for
// row numeric values so they stay anchored to the row top like the name/tax.
func drawCellValueTop(pdf *gopdf.GoPdf, x, y, w, h, topPad float64, text, align string, size float64) {
	if text == "" {
		return
	}
	setFont(pdf, size)
	for size > 4.5 && measureText(pdf, text) > w-1.5 {
		size -= 0.5
		setFont(pdf, size)
	}
	tw := measureText(pdf, text)
	tx := x + 1.2
	switch align {
	case "R":
		tx = x + w - tw - 1.2
	case "C":
		tx = x + (w-tw)/2
	}
	if tw > w {
		tx = x + 1.2
	}
	pdf.SetXY(tx, y+topPad)
	pdf.Cell(nil, text)
}

// ---- Page elements ----

const (
	continuePageY = 18.0 // y where continued/final-page content starts
	pageBottom    = 288.0
)

// supplyLabel converts a SupplyType to a human label.
func (d *InvoiceData) supplyLabel() string {
	if d.Invoice.Invoice.SupplyType == nil {
		return ""
	}
	switch *d.Invoice.Invoice.SupplyType {
	case "INTER_STATE", "INTERSTATE":
		return "INTER-STATE"
	case "INTRA_STATE", "INTRASTATE":
		return "INTRA-STATE"
	default:
		return *d.Invoice.Invoice.SupplyType
	}
}

func (d *InvoiceData) invoiceDate() string {
	return d.Invoice.Invoice.InvoiceDate.String()
}

// drawHeaderBand draws the centered "TAX INVOICE" heading plus the invoice
// metadata (Invoice No / Date / Financial Year / Supply) into a clean light
// band at the very top of the page. The detailed seller identity is NOT
// repeated here — it lives exclusively in the SELLER / FROM box below, so the
// header stays focused on the document details.
func drawHeaderBand(pdf *gopdf.GoPdf, data InvoiceData) float64 {
	bandH := 28.0

	pdf.SetFillColor(240, 244, 246)
	fillRect(pdf, 0, 0, pageWidth, bandH)
	pdf.SetFillColor(accentR, accentG, accentB)
	fillRect(pdf, 0, bandH-1.2, pageWidth, 1.2)

	// Keep the top band exclusively for document identity/metadata. Store
	// information is rendered in the Seller / From box below.
	cx := pageWidth / 2
	pdf.SetTextColor(accentR, accentG, accentB)
	setFontBold(pdf, 17)
	drawCentered(pdf, cx, 3.0, "TAX INVOICE")

	pdf.SetTextColor(40, 40, 40)
	setFont(pdf, 8.5)
	lh := lineHeightMM(8.5)
	meta := []string{
		"Invoice No: " + data.Invoice.Invoice.InvoiceNo,
		"Date: " + data.invoiceDate(),
		"Financial Year: " + data.Invoice.Invoice.FinancialYear,
	}
	if sup := data.supplyLabel(); sup != "" {
		meta = append(meta, "Supply: "+sup)
	}
	for i, line := range meta {
		drawCentered(pdf, cx, 11.0+float64(i)*lh, line)
	}

	return bandH + 5.0
}

// drawCentered renders a single line horizontally centred on cx (top at y).
func drawCentered(pdf *gopdf.GoPdf, cx, y float64, text string) {
	tw := measureText(pdf, text)
	pdf.SetXY(cx-tw/2, y)
	pdf.Cell(nil, text)
}

// ---- From / To boxes ----

type textLine struct {
	text string
	bold bool
	size float64
}

func (d *InvoiceData) sellerLines() []textLine {
	out := make([]textLine, 0, 7)

	// Keep the labels visible even when optional store data has not been
	// configured yet. This prevents an empty SellerInfo from collapsing the
	// seller box to only GSTIN/PAN and makes missing configuration obvious.
	name := strings.TrimSpace(d.Seller.Name)
	if name == "" {
		name = "-"
	}
	out = append(out, textLine{"Store Name: " + name, true, 9.5})

	gstin := strings.TrimSpace(d.Seller.GSTIN)
	if gstin == "" {
		gstin = "-"
	}
	out = append(out, textLine{"GSTIN: " + gstin, false, 8.5})

	pan := strings.TrimSpace(d.Seller.PAN)
	if pan == "" {
		pan = "-"
	}
	out = append(out, textLine{"PAN: " + pan, false, 8.5})

	address := strings.TrimSpace(d.Seller.Address)
	if address == "" {
		address = "-"
	}
	out = append(out, textLine{"Address: " + address, false, 8.5})

	phone := strings.TrimSpace(d.Seller.Phone)
	if phone == "" {
		phone = "-"
	}
	out = append(out, textLine{"Phone: " + phone, false, 8.5})

	state := strings.TrimSpace(d.Seller.StateName)
	if state == "" {
		state = strings.TrimSpace(d.Seller.StateCode)
	}
	if state == "" {
		state = "-"
	}
	out = append(out, textLine{"State: " + state, false, 8.5})

	return out
}

func (d *InvoiceData) buyerLines(invBuyer bool) []textLine {
	out := make([]textLine, 0, 7)

	buyerName := strings.TrimSpace(d.Buyer.Name)
	if buyerName == "" {
		buyerName = strings.TrimSpace(d.Invoice.CustomerName)
	}
	if buyerName == "" && d.Invoice.Invoice.BuyerName != nil {
		buyerName = strings.TrimSpace(*d.Invoice.Invoice.BuyerName)
	}
	if buyerName == "" {
		buyerName = "-"
	}
	out = append(out, textLine{"Buyer Name: " + buyerName, true, 9.5})

	gstin := strings.TrimSpace(d.Buyer.GSTIN)
	if gstin == "" && d.Invoice.Invoice.BuyerGSTIN != nil {
		gstin = strings.TrimSpace(*d.Invoice.Invoice.BuyerGSTIN)
	}
	if gstin == "" && d.Invoice.Invoice.CustomerGSTIN != nil {
		gstin = strings.TrimSpace(*d.Invoice.Invoice.CustomerGSTIN)
	}
	if invBuyer || gstin != "" {
		if gstin == "" {
			gstin = "-"
		}
		out = append(out, textLine{"GSTIN: " + gstin, false, 8.5})
	}

	pan := strings.TrimSpace(d.Buyer.PAN)
	if pan == "" {
		pan = "-"
	}
	out = append(out, textLine{"PAN: " + pan, false, 8.5})

	address := strings.TrimSpace(d.Buyer.Address)
	if address == "" && d.Invoice.Invoice.BuyerAddress != nil {
		address = strings.TrimSpace(*d.Invoice.Invoice.BuyerAddress)
	}
	if address == "" {
		address = "-"
	}
	out = append(out, textLine{"Address: " + address, false, 8.5})

	phone := strings.TrimSpace(d.Buyer.Phone)
	if phone == "" {
		phone = "-"
	}
	out = append(out, textLine{"Phone: " + phone, false, 8.5})

	state := strings.TrimSpace(d.Buyer.StateName)
	if state == "" {
		state = strings.TrimSpace(d.Buyer.StateCode)
	}
	if state == "" && d.Invoice.Invoice.CustomerStateCode != nil {
		state = strings.TrimSpace(*d.Invoice.Invoice.CustomerStateCode)
	}
	if state == "" {
		state = "-"
	}
	out = append(out, textLine{"State: " + state, false, 8.5})

	return out
}

func lineCount(pdf *gopdf.GoPdf, lines []textLine, maxWidth float64) int {
	n := 0
	for _, l := range lines {
		if l.bold {
			setFontBold(pdf, l.size)
		} else {
			setFont(pdf, l.size)
		}
		n += len(wrapLines(pdf, l.text, maxWidth, 8))
	}
	return n
}

func drawPartyBox(pdf *gopdf.GoPdf, x, y, w, h float64, title string, lines []textLine) {
	pdf.SetFillColor(245, 248, 250)
	fillRect(pdf, x, y, w, h)
	pdf.SetStrokeColor(150, 160, 165)
	pdf.SetLineWidth(0.3)
	pdf.Rectangle(x, y, x+w, y+h, "D", 0, 0)

	// section title band
	pdf.SetFillColor(accentR, accentG, accentB)
	fillRect(pdf, x, y, w, 7.0)
	pdf.SetTextColor(255, 255, 255)
	setFontBold(pdf, 8.5)
	drawTextTop(pdf, x+3, y+1.4, title)

	top := y + 9.5
	pad := 3.0
	maxWidth := w - 2*pad
	for _, l := range lines {
		if strings.TrimSpace(l.text) == "" {
			continue
		}
		if l.bold {
			setFontBold(pdf, l.size)
			pdf.SetTextColor(accentR, accentG, accentB)
		} else {
			setFont(pdf, l.size)
			pdf.SetTextColor(40, 40, 40)
		}
		parts := wrapLines(pdf, l.text, maxWidth, 8)
		lh := lineHeightMM(l.size)
		for _, part := range parts {
			drawTextTop(pdf, x+pad, top, part)
			top += lh
		}
	}
}

func drawParties(pdf *gopdf.GoPdf, data InvoiceData, startY float64) float64 {
	gap := 4.0
	halfW := (contentWidth - gap) / 2.0

	invBuyer := data.Invoice.Invoice.SaleType == "B2B"
	seller := data.sellerLines()
	buyer := data.buyerLines(invBuyer)

	setFont(pdf, 8.5)
	sellerN := lineCount(pdf, seller, halfW-6)
	buyerN := lineCount(pdf, buyer, halfW-6)
	rows := maxInt(sellerN, buyerN)
	h := 9.5 + float64(rows)*lineHeightMM(8.5) + 3.0

	drawPartyBox(pdf, marginLeft, startY, halfW, h, "SELLER / FROM", seller)
	drawPartyBox(pdf, marginLeft+halfW+gap, startY, halfW, h, "BUYER / TO", buyer)

	return startY + h + 6.0
}

// ---- Item table ----

type itemColumnKey string

const (
	itemColumnMedicine itemColumnKey = "medicine"
	itemColumnHSN      itemColumnKey = "hsn"
	itemColumnMRP      itemColumnKey = "mrp"
	itemColumnSell     itemColumnKey = "sell"
	itemColumnQty      itemColumnKey = "qty"
	itemColumnBonus    itemColumnKey = "bonus"
	itemColumnDiscount itemColumnKey = "discount"
	itemColumnTaxable  itemColumnKey = "taxable"
	itemColumnTax      itemColumnKey = "tax"
	itemColumnTotal    itemColumnKey = "total"
)

type col struct {
	key    itemColumnKey
	header string
	width  float64
	align  string
}

// Column widths sum exactly to contentWidth (180mm). Medicine gets the most
// space because it is the primary human-readable field on a pharmacy invoice.
var itemColumns = []col{
	{itemColumnMedicine, "MEDICINE", 50, "L"},
	{itemColumnHSN, "HSN", 11, "C"},
	{itemColumnMRP, "MRP", 12, "R"},
	{itemColumnSell, "SELL", 15, "R"},
	{itemColumnQty, "QTY", 8, "R"},
	{itemColumnBonus, "BONUS", 10, "R"},
	{itemColumnDiscount, "DISC.", 12, "R"},
	{itemColumnTaxable, "TAXABLE", 14, "R"},
	{itemColumnTax, "TAX", 20, "L"},
	{itemColumnTotal, "TOTAL", 28, "R"},
}

// printTaxParts builds the compact tax string for an item, showing only the
// GST components that are actually applicable.
func printTaxParts(item repository.SalesInvoiceItemDetail) []string {
	gst := rateString(item.GSTRate)
	igst := rateString(item.IGSTRate)
	cgst := rateString(item.CGSTRate)
	sgst := rateString(item.SGSTRate)
	cess := rateString(item.CessRate)

	var parts []string

	// Prefer the actual component rates. This keeps the rendered invoice
	// meaningful even if the aggregate GST rate is nil.
	if igst != "" && (cgst == "" || cgst == "0") && (sgst == "" || sgst == "0") {
		if igst != "0" {
			parts = append(parts, "IGST "+igst+"%")
		} else {
			parts = append(parts, "GST 0%")
		}
	} else {
		if gst != "" && gst != "0" {
			parts = append(parts, "GST "+gst+"%")
		}
		if cgst != "" && cgst != "0" {
			parts = append(parts, "CGST "+cgst+"%")
		}
		if sgst != "" && sgst != "0" {
			parts = append(parts, "SGST "+sgst+"%")
		}
		if len(parts) == 0 {
			parts = append(parts, "GST 0%")
		}
	}

	if cess != "" && cess != "0" {
		parts = append(parts, "CESS "+cess+"%")
	}

	return parts
}

func rateString(v *float64) string {
	if v == nil {
		return ""
	}
	return trimRate(*v)
}

// trimRate renders a rate dropping redundant trailing zeros (e.g. 2.50 -> 2.5).
func trimRate(v float64) string {
	out := strconv.FormatFloat(v, 'f', 2, 64)
	out = strings.TrimRight(out, "0")
	out = strings.TrimRight(out, ".")
	return out
}

type itemRowValues struct {
	medicine string
	hsn      string
	mrp      string
	sell     string
	qty      string
	bonus    string
	discount string
	taxable  string
	tax      []string
	total    string
}

func buildItemRowValues(item repository.SalesInvoiceItemDetail) itemRowValues {
	medicine := strings.TrimSpace(item.MedicineName)
	if medicine == "" {
		medicine = "(unnamed)"
	}

	mrp := "-"
	if item.MRP != nil {
		mrp = "₹" + formatAmount(*item.MRP)
	}

	hsn := "-"
	if item.HSNCode != nil && strings.TrimSpace(*item.HSNCode) != "" {
		hsn = strings.TrimSpace(*item.HSNCode)
	}

	discount := "-"
	if item.DiscountAmount > 0 {
		discount = "₹" + formatAmount(item.DiscountAmount)
	}

	taxable := "-"
	if item.TaxableValue != nil {
		taxable = "₹" + formatAmount(*item.TaxableValue)
	}

	total := "-"
	if item.LineTotal != nil {
		total = "₹" + formatAmount(*item.LineTotal)
	}

	return itemRowValues{
		medicine: medicine,
		hsn:      hsn,
		mrp:      mrp,
		sell:     "₹" + formatAmount(item.UnitSalePrice),
		qty:      strconv.Itoa(item.Quantity),
		bonus:    strconv.Itoa(item.BonusQuantity),
		discount: discount,
		taxable:  taxable,
		tax:      printTaxParts(item),
		total:    total,
	}
}

func (r itemRowValues) textFor(key itemColumnKey) string {
	switch key {
	case itemColumnMedicine:
		return r.medicine
	case itemColumnHSN:
		return r.hsn
	case itemColumnMRP:
		return r.mrp
	case itemColumnSell:
		return r.sell
	case itemColumnQty:
		return r.qty
	case itemColumnBonus:
		return r.bonus
	case itemColumnDiscount:
		return r.discount
	case itemColumnTaxable:
		return r.taxable
	case itemColumnTotal:
		return r.total
	default:
		return ""
	}
}

func drawItemTable(pdf *gopdf.GoPdf, data InvoiceData, startY float64) float64 {
	bodyFont := 7.0
	taxFont := 6.3
	headerH := 9.5
	baseRowH := 8.0

	drawHeaderRow := func(y float64) {
		if len(itemColumns) == 0 {
			return
		}

		x := marginLeft
		pdf.SetFillColor(226, 232, 236)
		for _, c := range itemColumns {
			fillRect(pdf, x, y, c.width, headerH)
			x += c.width
		}

		x = marginLeft
		pdf.SetTextColor(20, 35, 42)
		setFontBold(pdf, 7.2)
		for _, c := range itemColumns {
			tw := measureText(pdf, c.header)
			txText := x + 1.0
			switch c.align {
			case "R":
				txText = x + c.width - tw - 1.0
			case "C":
				txText = x + (c.width-tw)/2
			}
			pdf.SetXY(txText, y+2.7)
			pdf.Cell(nil, c.header)
			x += c.width
		}

		x = marginLeft
		for _, c := range itemColumns {
			strokeRect(pdf, x, y, c.width, headerH)
			x += c.width
		}
	}

	medicineCol := findItemColumn(itemColumnMedicine)
	if medicineCol == nil {
		return startY
	}

	taxCol := findItemColumn(itemColumnTax)

	y := startY
	drawHeaderRow(y)
	y += headerH

	for i, item := range data.Invoice.Items {
		row := buildItemRowValues(item)

		setFont(pdf, bodyFont)
		nameLines := wrapLines(pdf, row.medicine, medicineCol.width-2.4, 4)

		setFont(pdf, taxFont)
		taxLines := row.tax
		if taxCol != nil {
			taxLines = wrapLines(pdf, strings.Join(row.tax, "\n"), taxCol.width-1.6, 4)
		}

		lineCount := len(nameLines)
		if len(taxLines) > lineCount {
			lineCount = len(taxLines)
		}
		thisRowH := maxFloat(baseRowH, float64(lineCount)*lineHeightMM(bodyFont)+2.2)

		if y+thisRowH > pageBottom && (i > 0 || y > startY+headerH) {
			pdf.AddPage()
			y = continuePageY
			drawHeaderRow(y)
			y += headerH
		}

		pdf.SetFillColor(248, 250, 251)
		fillRect(pdf, marginLeft, y, contentWidth, thisRowH)

		setFont(pdf, bodyFont)
		pdf.SetTextColor(25, 25, 25)
		drawWrapped(pdf, marginLeft+0.8, y, medicineCol.width-1.6, thisRowH, nameLines, "L")

		x := marginLeft
		for _, c := range itemColumns {
			if c.key == itemColumnMedicine {
				x += c.width
				continue
			}

			if c.key == itemColumnTax {
				setFont(pdf, taxFont)
				pdf.SetTextColor(50, 50, 50)
				drawWrapped(pdf, x+0.8, y, c.width-1.6, thisRowH, taxLines, "L")
				x += c.width
				continue
			}

			setFont(pdf, bodyFont)
			pdf.SetTextColor(25, 25, 25)
			drawCellValueTop(pdf, x, y, c.width, thisRowH, 1.0, row.textFor(c.key), c.align, bodyFont)
			x += c.width
		}

		x = marginLeft
		for _, c := range itemColumns {
			strokeRect(pdf, x, y, c.width, thisRowH)
			x += c.width
		}

		y += thisRowH
	}

	return y + 2.0
}

func findItemColumn(key itemColumnKey) *col {
	for i := range itemColumns {
		if itemColumns[i].key == key {
			return &itemColumns[i]
		}
	}
	return nil
}

// ---- Bill summary ----

func drawSummary(pdf *gopdf.GoPdf, data InvoiceData, startY float64) float64 {
	inv := data.Invoice.Invoice

	boxW := 92.0
	x := marginLeft + contentWidth - boxW
	rowH := 5.6
	labelPad := 3.5
	valueRight := x + boxW - 3.5

	type row struct {
		label string
		value string
	}

	rows := []row{}
	if inv.GrossAmount != nil {
		rows = append(rows, row{"Gross Amount", "₹" + formatAmount(*inv.GrossAmount)})
	}
	if inv.DiscountTotal > 0 {
		rows = append(rows, row{"Total Discount", "₹" + formatAmount(inv.DiscountTotal)})
	}
	if inv.TaxableAmount != nil {
		rows = append(rows, row{"Taxable Value", "₹" + formatAmount(*inv.TaxableAmount)})
	}

	// preserve the CGST / SGST / IGST / cess distinction
	showCGST := inv.CGSTTotal != nil && *inv.CGSTTotal > 0
	showSGST := inv.SGSTTotal != nil && *inv.SGSTTotal > 0
	showIGST := inv.IGSTTotal != nil && *inv.IGSTTotal > 0
	showCess := inv.CessTotal != nil && *inv.CessTotal > 0

	if showCGST {
		rows = append(rows, row{"CGST", "₹" + formatAmount(*inv.CGSTTotal)})
	}
	if showSGST {
		rows = append(rows, row{"SGST", "₹" + formatAmount(*inv.SGSTTotal)})
	}
	if showIGST {
		rows = append(rows, row{"IGST", "₹" + formatAmount(*inv.IGSTTotal)})
	}
	if showCess {
		rows = append(rows, row{"Cess", "₹" + formatAmount(*inv.CessTotal)})
	}
	if inv.TaxTotal != nil && *inv.TaxTotal > 0 {
		rows = append(rows, row{"Total Tax", "₹" + formatAmount(*inv.TaxTotal)})
	}
	if inv.RoundOff != nil && *inv.RoundOff != 0 {
		rows = append(rows, row{"Round Off", formatAmount(*inv.RoundOff)})
	}

	grandTotal := inv.TotalAmount
	if inv.GrandTotal != nil {
		grandTotal = *inv.GrandTotal
	}

	grandH := 9.0
	titleH := 7.0
	boxH := titleH + float64(len(rows))*rowH + grandH

	// ensure we have room on the current page for the whole summary box,
	// otherwise start a fresh page so summary + footer stay together.
	if startY+boxH+2 > pageBottom {
		pdf.AddPage()
		startY = continuePageY
	}

	// BILL SUMMARY title band
	pdf.SetFillColor(accentR, accentG, accentB)
	fillRect(pdf, x, startY, boxW, titleH)
	pdf.SetTextColor(255, 255, 255)
	setFontBold(pdf, 8.5)
	drawTextTop(pdf, x+labelPad, startY+1.4, "BILL SUMMARY")

	top := startY + titleH
	pdf.SetFillColor(245, 248, 250)
	fillRect(pdf, x, top, boxW, boxH-titleH)

	for _, r := range rows {
		setFont(pdf, 8.5)
		pdf.SetTextColor(35, 35, 35)
		drawTextTop(pdf, x+labelPad, top+1.2, r.label)
		setFontBold(pdf, 8.5)
		pdf.SetTextColor(30, 30, 30)
		fitFont(pdf, r.value, boxW-labelPad*2, 8.5, 6.0, true)
		drawRightAligned(pdf, valueRight, top+1.2, r.value)
		top += rowH
	}

	// grand total band
	pdf.SetFillColor(accentR, accentG, accentB)
	fillRect(pdf, x, top, boxW, grandH)
	setFontBold(pdf, 10)
	pdf.SetTextColor(255, 255, 255)
	drawTextTop(pdf, x+labelPad, top+2.2, "GRAND TOTAL")
	grandText := "₹" + formatAmount(grandTotal)
	setFontBold(pdf, 11)
	drawRightAligned(pdf, valueRight, top+2.0, grandText)

	// frame
	pdf.SetStrokeColor(120, 130, 140)
	pdf.SetLineWidth(0.4)
	pdf.Rectangle(x, startY, boxW, boxH, "D", 0, 0)

	return top + grandH + 4.0
}

func drawAmountInWords(pdf *gopdf.GoPdf, data InvoiceData, startY float64) float64 {
	grandTotal := data.Invoice.Invoice.TotalAmount
	if data.Invoice.Invoice.GrandTotal != nil {
		grandTotal = *data.Invoice.Invoice.GrandTotal
	}

	words := NumberToWordsIndian(grandTotal)
	full := "Rupees " + words + " Only"

	setFontBold(pdf, 8.5)
	pdf.SetTextColor(accentR, accentG, accentB)
	drawTextTop(pdf, marginLeft, startY, "Amount in Words")

	setFont(pdf, 8.5)
	pdf.SetTextColor(30, 30, 30)
	lines := wrapLines(pdf, full, contentWidth-100, 3)
	for i, line := range lines {
		drawTextTop(pdf, marginLeft+2, startY+4+float64(i)*lineHeightMM(8.5), line)
	}

	return startY + 4 + float64(len(lines))*lineHeightMM(8.5) + 5.0
}

func drawFooter(pdf *gopdf.GoPdf, data InvoiceData, y float64) {
	pdf.SetStrokeColor(180, 180, 180)
	pdf.SetLineWidth(0.3)
	pdf.Line(marginLeft, y, pageWidth-marginRight, y)

	// signature area on the right with room to sign
	sigY := y + 26.0
	pdf.SetStrokeColor(160, 160, 160)
	pdf.SetLineWidth(0.3)
	sigX := pageWidth - marginRight - 55.0
	pdf.Line(sigX, sigY, pageWidth-marginRight, sigY)

	setFont(pdf, 8)
	pdf.SetTextColor(110, 110, 110)
	drawTextTop(pdf, marginLeft, y+6.0, "This is a computer-generated invoice.")
	// Only sign the real store name when it is actually configured — never a
	// fabricated placeholder.
	storeName := strings.TrimSpace(data.Seller.Name)
	if storeName != "" {
		setFont(pdf, 8.5)
		pdf.SetTextColor(60, 60, 60)
		drawTextTop(pdf, marginLeft, y+10.0, "For "+storeName)
	}

	// authorized signatory label under the signature line
	setFontBold(pdf, 9)
	pdf.SetTextColor(30, 30, 30)
	drawRightAligned(pdf, pageWidth-marginRight, sigY+3.0, "Authorized Signatory")
}

// formatAmount renders a float in Indian digit grouping, e.g. 1,23,456.78.
func formatAmount(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	paise := int64(math.Round((v - math.Floor(v)) * 100))
	if paise == 100 {
		paise = 0
		v = math.Floor(v) + 1
	}
	intPart := int64(v)
	s := strconv.FormatInt(intPart, 10)

	// Indian grouping: rightmost 3, then pairs.
	parts := []string{}
	pos := len(s)
	if pos > 3 {
		parts = append(parts, s[pos-3:])
		pos -= 3
		for pos > 2 {
			parts = append(parts, s[pos-2:pos])
			pos -= 2
		}
	}
	if pos > 0 {
		parts = append(parts, s[:pos])
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}

	out := strings.Join(parts, ",")
	if paise > 0 {
		out += "." + fmt.Sprintf("%02d", paise)
	} else {
		out += ".00"
	}
	if neg {
		out = "-" + out
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func NumberToWordsIndian(amount float64) string {
	if amount == 0 {
		return "Zero"
	}

	ones := []string{"", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine",
		"Ten", "Eleven", "Twelve", "Thirteen", "Fourteen", "Fifteen", "Sixteen", "Seventeen", "Eighteen", "Nineteen"}
	tens := []string{"", "", "Twenty", "Thirty", "Forty", "Fifty", "Sixty", "Seventy", "Eighty", "Ninety"}

	totalPaise := int(math.Round(amount * 100))
	paise := totalPaise % 100
	intPart := totalPaise / 100

	var result strings.Builder

	if intPart == 0 {
		result.WriteString("Zero")
	} else {
		crore := intPart / 10000000
		intPart %= 10000000
		lakh := intPart / 100000
		intPart %= 100000
		thousand := intPart / 1000
		intPart %= 1000
		hundred := intPart

		writeGroup := func(n int) {
			if n == 0 {
				return
			}
			if n >= 100 {
				result.WriteString(ones[n/100] + " Hundred ")
				n %= 100
			}
			if n >= 20 {
				result.WriteString(tens[n/10] + " ")
				n %= 10
			}
			if n > 0 {
				result.WriteString(ones[n] + " ")
			}
		}

		if crore > 0 {
			writeGroup(crore)
			result.WriteString("Crore ")
		}
		if lakh > 0 {
			writeGroup(lakh)
			result.WriteString("Lakh ")
		}
		if thousand > 0 {
			writeGroup(thousand)
			result.WriteString("Thousand ")
		}
		if hundred > 0 {
			writeGroup(hundred)
		}
	}

	if paise > 0 {
		result.WriteString("and ")
		if paise >= 20 {
			result.WriteString(tens[paise/10] + " ")
			paise %= 10
		}
		if paise > 0 {
			result.WriteString(ones[paise] + " ")
		}
		result.WriteString("Paise")
	}

	return strings.TrimSpace(result.String())
}
