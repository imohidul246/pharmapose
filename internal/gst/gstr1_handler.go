package gst

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/repository"
)

type Handler struct {
	builder *GSTR1Builder
	gstr3b  *GSTR3BService
	gstr2b  *repository.GSTR2BRepo
	db      *pgxpool.Pool
}

func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{
		builder: NewGSTR1Builder(db),
		gstr3b:  NewGSTR3BService(db),
		gstr2b:  repository.NewGSTR2BRepo(db),
		db:      db,
	}
}

// GET /api/gst/gstr1?start_date=&end_date=&store_id=
func (h *Handler) GetGSTR1(c *gin.Context) {
	req, err := h.bindRequest(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	// Fetch GSTIN for the store
	gstin, err := h.fetchStoreGSTIN(c.Request.Context(), req.StoreID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not resolve GSTIN for store")
		return
	}
	req.GSTIN = gstin

	gstr, err := h.builder.BuildGSTR1(c.Request.Context(), req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to build GSTR-1: "+err.Error())
		return
	}

	// Validate before export so a non-compliant return is not silently produced.
	if err := ValidateGSTR1(gstr); err != nil {
		respondError(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", "attachment; filename=gstr1_"+req.StartDate.Format("200601")+".json")
	c.JSON(http.StatusOK, gstr)
}

// GET /api/gst/gstr1/preview?start_date=&end_date=&store_id=
func (h *Handler) GetGSTR1Preview(c *gin.Context) {
	req, err := h.bindRequest(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	summary, err := h.builder.PreviewSummary(c.Request.Context(), req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to build preview: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, summary)
}

// GET /api/gst/gstr1/excel?start_date=&end_date=&store_id=
func (h *Handler) DownloadGSTR1CSV(c *gin.Context) {
	req, err := h.bindRequest(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	gstin, err := h.fetchStoreGSTIN(c.Request.Context(), req.StoreID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not resolve GSTIN for store")
		return
	}
	req.GSTIN = gstin

	gstr, err := h.builder.BuildGSTR1(c.Request.Context(), req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to build GSTR-1: "+err.Error())
		return
	}

	if err := ValidateGSTR1(gstr); err != nil {
		respondError(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	csv := buildGSTR1CSV(gstr)
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", "attachment; filename=gstr1_"+req.StartDate.Format("200601")+".csv")
	c.Data(http.StatusOK, "text/csv", []byte(csv))
}

func (h *Handler) bindRequest(c *gin.Context) (GSTR1Request, error) {
	storeID := principalStoreID(c, c.Query("store_id"))
	startDateStr := c.Query("start_date")
	endDateStr := c.Query("end_date")
	period := c.Query("period")

	req := GSTR1Request{StoreID: storeID, Period: period}

	if period != "" {
		start, end, err := periodRange(period)
		if err != nil {
			return req, errors.New("period must be in YYYY-MM format")
		}
		req.StartDate, req.EndDate = start, end
		return req, nil
	}

	if startDateStr == "" || endDateStr == "" {
		return req, errors.New("period (YYYY-MM) or start_date and end_date are required")
	}

	startDate, err := time.Parse("2006-01-02", startDateStr)
	if err != nil {
		return req, errors.New("start_date must be YYYY-MM-DD")
	}
	endDate, err := time.Parse("2006-01-02", endDateStr)
	if err != nil {
		return req, errors.New("end_date must be YYYY-MM-DD")
	}
	endDate = endDate.AddDate(0, 0, 1) // inclusive end

	if startDate.After(endDate) {
		return req, errors.New("start_date must not exceed end_date")
	}

	req.StartDate, req.EndDate = startDate, endDate
	return req, nil
}

func (h *Handler) fetchStoreGSTIN(ctx context.Context, storeID string) (string, error) {
	gstin, _, err := h.fetchStoreGSTContext(ctx, storeID)
	return gstin, err
}

// fetchStoreGSTContext resolves the active GSTIN and state code for a store.
// When no store is given, the single active registration is used so the
// single-registration layout (no store selector) still resolves a GSTIN. If
// zero registrations exist the resolution is left empty so validation can
// surface the missing setup instead of guessing.
func (h *Handler) fetchStoreGSTContext(ctx context.Context, storeID string) (string, string, error) {
	var gstin, stateCode *string
	var err error
	if storeID != "" {
		err = h.db.QueryRow(ctx, `
			SELECT gr.gstin, gr.state_code
			FROM stores s
			JOIN gst_registrations gr ON gr.id = s.gst_registration_id
			WHERE s.id = $1 AND gr.is_active = true`,
			storeID).Scan(&gstin, &stateCode)
	} else {
		err = h.db.QueryRow(ctx, `
			SELECT gr.gstin, gr.state_code
			FROM gst_registrations gr
			WHERE gr.is_active = true
			ORDER BY gr.created_at
			LIMIT 1`).Scan(&gstin, &stateCode)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	gstinStr, stateStr := "", ""
	if gstin != nil {
		gstinStr = *gstin
	}
	if stateCode != nil {
		stateStr = *stateCode
	}
	return gstinStr, stateStr, nil
}

func respondError(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": msg})
}

// buildGSTR1CSV generates a CSV representation of the GSTR-1 file for
// local review. Invoice rows are emitted per item detail line; the HSN and
// document-series summaries are emitted as separate blocks.
func buildGSTR1CSV(g *GSTR1) string {
	var sb strings.Builder

	// B2B Section (Table 4)
	sb.WriteString("Section,Invoice No,Invoice Date,Invoice Value,POS,Recipient GSTIN,Rate,Taxable Value,CGST,SGST,IGST,Cess\n")
	for _, entry := range g.B2B {
		for _, inv := range entry.Inv {
			for _, item := range inv.Itms {
				sb.WriteString("B2B,")
				sb.WriteString(csvEscape(inv.Inum))
				sb.WriteString(",")
				sb.WriteString(csvEscape(inv.Idt))
				sb.WriteString(",")
				sb.WriteString(fmtNum(inv.Val))
				sb.WriteString(",")
				sb.WriteString(csvEscape(inv.Pos))
				sb.WriteString(",")
				sb.WriteString(csvEscape(entry.Ctin))
				sb.WriteString(",")
				sb.WriteString(fmtNum(item.ItmDet.Rt))
				sb.WriteString(",")
				sb.WriteString(fmtNum(item.ItmDet.Txval))
				sb.WriteString(",")
				sb.WriteString(fmtNum(item.ItmDet.Camt))
				sb.WriteString(",")
				sb.WriteString(fmtNum(item.ItmDet.Samt))
				sb.WriteString(",")
				sb.WriteString(fmtNum(item.ItmDet.Iamt))
				sb.WriteString(",")
				sb.WriteString(fmtNum(item.ItmDet.Csamt))
				sb.WriteString("\n")
			}
		}
	}

	// B2CL Section (Table 6B)
	for _, entry := range g.B2CL {
		for _, inv := range entry.Inv {
			for _, item := range inv.Itms {
				sb.WriteString("B2CL,")
				sb.WriteString(csvEscape(inv.Inum))
				sb.WriteString(",")
				sb.WriteString(csvEscape(inv.Idt))
				sb.WriteString(",")
				sb.WriteString(fmtNum(inv.Val))
				sb.WriteString(",")
				sb.WriteString(csvEscape(entry.Pos))
				sb.WriteString(",,,")
				sb.WriteString(fmtNum(item.ItmDet.Txval))
				sb.WriteString(",")
				sb.WriteString(fmtNum(item.ItmDet.Camt))
				sb.WriteString(",")
				sb.WriteString(fmtNum(item.ItmDet.Samt))
				sb.WriteString(",")
				sb.WriteString(fmtNum(item.ItmDet.Iamt))
				sb.WriteString(",")
				sb.WriteString(fmtNum(item.ItmDet.Csamt))
				sb.WriteString("\n")
			}
		}
	}

	// B2CS Section (Table 7)
	for _, item := range g.B2CS {
		sb.WriteString("B2CS,")
		sb.WriteString(",")
		sb.WriteString(",")
		sb.WriteString(",")
		sb.WriteString(csvEscape(item.Pos))
		sb.WriteString(",")
		sb.WriteString(csvEscape(item.SplyTy))
		sb.WriteString(",")
		sb.WriteString(fmtNum(item.Rt))
		sb.WriteString(",")
		sb.WriteString(fmtNum(item.Txval))
		sb.WriteString(",")
		sb.WriteString(fmtNum(item.Camt))
		sb.WriteString(",")
		sb.WriteString(fmtNum(item.Samt))
		sb.WriteString(",")
		sb.WriteString(fmtNum(item.Iamt))
		sb.WriteString(",")
		sb.WriteString(fmtNum(item.Csamt))
		sb.WriteString("\n")
	}

	// HSN Section (Table 12)
	sb.WriteString("\nHSN Summary (Table 12)\n")
	sb.WriteString("HSN,Description,UQC,Rate,Qty,Taxable Value,CGST,SGST,IGST,Cess\n")
	for _, h := range g.Hsn.Data {
		sb.WriteString(csvEscape(h.HSNCode))
		sb.WriteString(",")
		sb.WriteString(csvEscape(h.Desc))
		sb.WriteString(",")
		sb.WriteString(csvEscape(h.UQC))
		sb.WriteString(",")
		sb.WriteString(fmtNum(h.Rt))
		sb.WriteString(",")
		sb.WriteString(fmtNum(h.Qty))
		sb.WriteString(",")
		sb.WriteString(fmtNum(h.Txval))
		sb.WriteString(",")
		sb.WriteString(fmtNum(h.Camt))
		sb.WriteString(",")
		sb.WriteString(fmtNum(h.Samt))
		sb.WriteString(",")
		sb.WriteString(fmtNum(h.Iamt))
		sb.WriteString(",")
		sb.WriteString(fmtNum(h.Csamt))
		sb.WriteString("\n")
	}

	// Document Series (Table 13)
	sb.WriteString("\nDocument Series (Table 13)\n")
	sb.WriteString("Document Type,From,To,Issued,Cancelled,Net Issued\n")
	for _, d := range g.DocIssue.DocDet {
		for _, doc := range d.Docs {
			sb.WriteString(csvEscape(d.DocTyp))
			sb.WriteString(",")
			sb.WriteString(csvEscape(doc.From))
			sb.WriteString(",")
			sb.WriteString(csvEscape(doc.To))
			sb.WriteString(",")
			sb.WriteString(fmtNum(doc.TotNum))
			sb.WriteString(",")
			sb.WriteString(fmtNum(doc.Cancel))
			sb.WriteString(",")
			sb.WriteString(fmtNum(doc.NetIssue))
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func fmtNum(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// MarshalJSON is a helper for handlers that need to serialize GSTR-1 to JSON.
func MarshalJSON(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
