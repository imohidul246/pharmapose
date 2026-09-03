package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mohi/pms-marg-inspired/internal/pdf"
	"github.com/mohi/pms-marg-inspired/internal/repository"
)

type PDFDeps struct {
	SaleRepo     *repository.SaleRepo
	TaxRepo      *repository.TaxRepo
	CustomerRepo *repository.CustomerRepo
}

// GET /api/sales/invoices/:id/pdf — B2B invoice PDF download.
//
// The PDF is rendered into a buffer first; headers are sent only after
// generation succeeds, so a mid-generation failure yields a clean JSON
// error (400/500) instead of a truncated 200 OK PDF. Seller identity is
// validated BEFORE generation, so a store with missing GSTIN / trade name /
// state code gets a clean 400 instead of a corrupt download.
func (d PDFDeps) generateB2BInvoicePDF(c *gin.Context) {
	id := c.Param("id")
	detail, err := d.SaleRepo.GetInvoice(c.Request.Context(), storeIDFor(c), id)
	if mapRepoError(c, err) {
		return
	}

	// Only allow PDF for B2B invoices
	if detail.Invoice.SaleType != "B2B" {
		respondBadRequest(c, fmt.Errorf("PDF invoice is only available for B2B sales"))
		return
	}

	// Fetch store/seller info from the real store configuration. We never
	// hard-code a placeholder seller identity — fields are left empty and are
	// populated only from authoritative store/GST-registration data.
	// Every access is nil-guarded: a store without a profile row or without
	// a linked business registration yields an empty SellerInfo, which the
	// validation below rejects with a clean error (never a nil panic).
	seller := pdf.SellerInfo{}
	if detail.Invoice.StoreID != nil && *detail.Invoice.StoreID != "" {
		store, err := d.TaxRepo.GetStore(c.Request.Context(), *detail.Invoice.StoreID)

		if err == nil && store != nil {
			seller.Name = store.Name
			seller.Address = store.Address
			seller.Phone = store.Phone
			if store.GSTRegistrationID != nil && *store.GSTRegistrationID != "" {
				gr, err := d.TaxRepo.GetGSTRegistration(c.Request.Context(), *store.GSTRegistrationID)
				if err == nil && gr != nil {
					if gr.GSTIN != nil {
						seller.GSTIN = *gr.GSTIN
					}
					if gr.PAN != nil {
						seller.PAN = *gr.PAN
					}
					seller.StateCode = gr.StateCode
					seller.StateName = gr.StateName
					if strings.TrimSpace(seller.Name) == "" {
						seller.Name = gr.TradeName
					}
					if strings.TrimSpace(seller.Address) == "" {
						seller.Address = gr.Address
					}
				}
			}
		}
	}

	// Mandatory seller metadata (GSTIN, trade name, state code) must be
	// present before generation starts; otherwise abort with a validation
	// error while the status line is still writable.
	if err := seller.Validate(); err != nil {
		respondBadRequest(c, err)
		return
	}

	// Fetch buyer/customer info when a customer is linked to the invoice.
	buyer := pdf.BuyerInfo{}
	if detail.Invoice.CustomerID != nil && *detail.Invoice.CustomerID != "" {
		if cust, err := d.CustomerRepo.GetByID(c.Request.Context(), *detail.Invoice.CustomerID); err == nil && cust != nil {
			buyer.Name = cust.Name
			buyer.Phone = cust.Phone
			if cust.GSTIN != nil {
				buyer.GSTIN = *cust.GSTIN
			}
			if cust.BillingAddress != nil {
				buyer.Address = *cust.BillingAddress
			}
			if cust.StateCode != nil {
				buyer.StateCode = *cust.StateCode
			}
			if cust.State != nil {
				buyer.StateName = *cust.State
			}
		}
	} else if detail.Invoice.BuyerName != nil {
		buyer.Name = *detail.Invoice.BuyerName
	}
	if detail.Invoice.BuyerGSTIN != nil {
		buyer.GSTIN = *detail.Invoice.BuyerGSTIN
	}
	if detail.Invoice.CustomerGSTIN != nil {
		buyer.GSTIN = *detail.Invoice.CustomerGSTIN
	}
	if detail.Invoice.BuyerAddress != nil {
		buyer.Address = *detail.Invoice.BuyerAddress
	}

	data := pdf.InvoiceData{
		Invoice: *detail,
		Seller:  seller,
		Buyer:   buyer,
	}

	// Render into a buffer first: if generation fails, no headers have been
	// flushed yet, so a normal JSON error is still possible.
	var buf bytes.Buffer
	if err := pdf.GenerateInvoicePDF(&buf, data); err != nil {
		if errors.Is(err, pdf.ErrSellerIncomplete) {
			respondBadRequest(c, err)
			return
		}
		respondInternal(c, err)
		return
	}

	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=B2B_%s.pdf", detail.Invoice.InvoiceNo))
	c.Data(200, "application/pdf", buf.Bytes())
}
