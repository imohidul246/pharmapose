package handlers

import (
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
func (d PDFDeps) generateB2BInvoicePDF(c *gin.Context) {
	fmt.Println("inside generate b2b pdf")
	id := c.Param("id")
	detail, err := d.SaleRepo.GetInvoice(c.Request.Context(), storeIDFor(c), id)
	if mapRepoError(c, err) {
		return
	}

	fmt.Printf("store id: %v", storeIDFor(c))

	// Only allow PDF for B2B invoices
	if detail.Invoice.SaleType != "B2B" {
		respondBadRequest(c, fmt.Errorf("PDF invoice is only available for B2B sales"))
		return
	}

	// Fetch store/seller info from the real store configuration. We never
	// hard-code a placeholder seller identity — fields are left empty and are
	// populated only from authoritative store/GST-registration data. Empty
	// optional fields (GSTIN/PAN) are rendered as "-" by the PDF layer.
	seller := pdf.SellerInfo{}
	if detail.Invoice.StoreID != nil && *detail.Invoice.StoreID != "" {
		store, err := d.TaxRepo.GetStore(c.Request.Context(), *detail.Invoice.StoreID)

		if err == nil {
			seller.Name = store.Name
			seller.Address = store.Address
			seller.Phone = store.Phone
			if store.GSTRegistrationID != nil {
				gr, err := d.TaxRepo.GetGSTRegistration(c.Request.Context(), *store.GSTRegistrationID)
				if err == nil {
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

	// Fetch buyer/customer info when a customer is linked to the invoice.
	buyer := pdf.BuyerInfo{}
	if detail.Invoice.CustomerID != nil && *detail.Invoice.CustomerID != "" {
		if cust, err := d.CustomerRepo.GetByID(c.Request.Context(), *detail.Invoice.CustomerID); err == nil {
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

	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=B2B_%s.pdf", detail.Invoice.InvoiceNo))

	if err := pdf.GenerateInvoicePDF(c.Writer, data); err != nil {
		respondInternal(c, err)
	}
}
