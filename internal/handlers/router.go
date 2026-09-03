package handlers

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"

	"github.com/mohi/pms-marg-inspired/internal/auth"
	"github.com/mohi/pms-marg-inspired/internal/gst"
	"github.com/mohi/pms-marg-inspired/internal/repository"
)

type Deps struct {
	AuthRepo                    *repository.AuthRepo
	PurchaseRequestRepo         *repository.PurchaseRequestRepo
	StockAuditRequestRepo       *repository.StockAuditRequestRepo
	CookieOptions               auth.CookieOptions
	DevOrigins                  []string
	MedicineRepo                *repository.MedicineRepo
	CustomerRepo                *repository.CustomerRepo
	SaleRepo                    *repository.SaleRepo
	PurchaseRepo                *repository.PurchaseRepo
	ReconcileRepo               *repository.ReconcileRepo
	ReportRepo                  *repository.ReportRepo
	SupplierRepo                *repository.SupplierRepo
	TaxRepo                     *repository.TaxRepo
	GSTHandler                  *gst.Handler
}

func NewRouter(d Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	if len(d.DevOrigins) == 0 {
		d.DevOrigins = []string{"http://localhost:5173"}
	}
	r := gin.New()

	r.Use(gin.Recovery())
	r.Use(jsonErrorMiddleware())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     d.DevOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept"},
		AllowCredentials: true,
	}))
	r.Use(gzip.Gzip(gzip.DefaultCompression))

	api := r.Group("/api")
	{
		api.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Public auth: bootstrap registration + login. Everything else is
		// behind RequireAuth.
		api.POST("/auth/register", d.register)
		api.POST("/auth/login", d.login)

		protected := api.Group("")
		protected.Use(auth.RequireAuth(d.AuthRepo, d.CookieOptions), auth.CSRFProtect(d.DevOrigins))
		{
			protected.POST("/auth/logout", d.logout)
			protected.GET("/auth/me", d.me)
			protected.POST("/auth/change-password", d.changePassword)

			employees := protected.Group("/employees")
			{
				employees.Use(auth.RequireOwner())
				employees.GET("", d.listEmployees)
				employees.POST("", d.inviteEmployee)
				employees.DELETE("/:userID", d.deactivateEmployee)
			}

			store := protected.Group("/store")
			{
				store.Use(auth.RequireOwner())
				store.GET("", d.getStore)
				store.PUT("", d.updateStore)
			}

			purchases := protected.Group("/purchases")
			{
				purchases.GET("", auth.RequirePermission(auth.PermPurchaseView), d.listPurchases)
				purchases.POST("", auth.RequireOwner(), d.createPurchase)
			}

			// Employee-held workflows: submit here, the owner confirms below.
			purchaseRequests := protected.Group("/purchase-requests")
			{
				purchaseRequests.POST("", auth.RequirePermission(auth.PermPurchaseCreate), d.createPurchaseRequest)
				purchaseRequests.GET("", auth.RequirePermission(auth.PermPurchaseView), d.listPurchaseRequests)
				purchaseRequests.GET("/:id", auth.RequirePermission(auth.PermPurchaseView), d.getPurchaseRequest)
				purchaseRequests.POST("/:id/approve", auth.RequireOwner(), d.approvePurchaseRequest)
				purchaseRequests.POST("/:id/reject", auth.RequireOwner(), d.rejectPurchaseRequest)
				purchaseRequests.POST("/:id/cancel", auth.RequirePermission(auth.PermPurchaseCreate), d.cancelPurchaseRequest)
			}

			stockAuditRequests := protected.Group("/stock-audit-requests")
			{
				stockAuditRequests.POST("", auth.RequirePermission(auth.PermStockAuditCreate), d.createStockAuditRequest)
				stockAuditRequests.GET("", auth.RequirePermission(auth.PermStockView), d.listStockAuditRequests)
				stockAuditRequests.GET("/:id", auth.RequirePermission(auth.PermStockView), d.getStockAuditRequest)
				stockAuditRequests.POST("/:id/approve", auth.RequireOwner(), d.approveStockAuditRequest)
				stockAuditRequests.POST("/:id/reject", auth.RequireOwner(), d.rejectStockAuditRequest)
				stockAuditRequests.POST("/:id/cancel", auth.RequirePermission(auth.PermStockAuditCreate), d.cancelStockAuditRequest)
			}

			// Offline cache: the SPA mirrors inventory + customers into
			// IndexedDB; these are the plumbing endpoints it calls on login.
			sync := protected.Group("/sync")
			{
				sync.GET("/inventory", auth.RequirePermission(auth.PermStockView), d.getInventorySync)
				sync.GET("/customers", auth.RequirePermission(auth.PermCustomerView), d.getCustomersSync)
				sync.GET("/tax", auth.RequirePermission(auth.PermStockView), d.getTaxConfigSync)
			}

			inv := protected.Group("/inventory")
			{
				inv.POST("/reconcile", auth.RequireOwner(), d.reconcile)
				inv.GET("/reconciliations", auth.RequirePermission(auth.PermStockView), d.listReconciliations)
			}

			meds := protected.Group("/medicines")
			{
				meds.GET("", d.listMedicines)
				meds.POST("", d.createMedicine)
				meds.GET("/:id", d.getMedicine)
				meds.GET("/:id/detail", d.getMedicineDetail)
				meds.PUT("/:id", d.updateMedicine)
				meds.DELETE("/:id", d.deleteMedicine)
				meds.GET("/:id/tax-config", d.getMedicineTaxConfig)
				meds.PUT("/:id/tax-config", auth.RequireOwner(), d.upsertMedicineTaxConfig)
			}

			customers := protected.Group("/customers")
			{
				customers.GET("", d.listCustomers)
				customers.POST("", d.createCustomer)
				customers.GET("/:id", d.getCustomer)
				customers.PUT("/:id", d.updateCustomer)
				customers.GET("/:id/ledger", d.customerLedger)
				customers.POST("/:id/payments", d.recordPayment)
			}

			suppliers := protected.Group("/suppliers")
			{
				suppliers.GET("", d.listSuppliers)
				suppliers.POST("", d.createSupplier)
				suppliers.GET("/:id", d.getSupplier)
				suppliers.PUT("/:id", d.updateSupplier)
				suppliers.DELETE("/:id", d.deleteSupplier)
			}

			hsn := protected.Group("/hsn")
			{
				hsn.GET("", d.listHSNCodes)
				hsn.POST("", auth.RequireOwner(), d.createHSNCode)
				hsn.PUT("/:id/tax-rate", auth.RequireOwner(), d.upsertTaxRate)
			}

			protected.POST("/sales/checkout", auth.RequirePermission(auth.PermSalesCreate), d.checkout)

			salesInvoices := protected.Group("/sales/invoices")
			{
				salesInvoices.GET("", auth.RequirePermission(auth.PermSalesView), d.listSalesInvoices)
				salesInvoices.GET("/resolve", auth.RequirePermission(auth.PermSalesView), d.getSalesInvoiceByNo)
				salesInvoices.GET("/:id", auth.RequirePermission(auth.PermSalesView), d.getSalesInvoice)
				salesInvoices.GET("/:id/pdf", PDFDeps{SaleRepo: d.SaleRepo, TaxRepo: d.TaxRepo, CustomerRepo: d.CustomerRepo}.generateB2BInvoicePDF)
			}

			purchaseInvoices := protected.Group("/purchases/invoices")
			{
				purchaseInvoices.GET("", auth.RequirePermission(auth.PermPurchaseView), d.listPurchaseInvoices)
				purchaseInvoices.GET("/:id", auth.RequirePermission(auth.PermPurchaseView), d.getPurchaseInvoice)
			}

			reports := protected.Group("/reports")
			{
				reports.GET("/sales", d.salesReport)
				reports.GET("/purchase", d.purchaseReport)
				reports.GET("/profit-loss", d.profitLossReport)
				reports.GET("/expiry", d.expiryReport)
				reports.GET("/low-stock", d.lowStockReport)
			}

			gst := protected.Group("/gst")
			{
				gst.GET("/gstr1", d.GSTHandler.GetGSTR1)
				gst.GET("/gstr1/preview", d.GSTHandler.GetGSTR1Preview)
				gst.GET("/gstr1/excel", d.GSTHandler.DownloadGSTR1CSV)
				gst.GET("/gstr3b", d.GSTHandler.GetGSTR3B)
				gst.POST("/gstr2b/import", d.GSTHandler.ImportGSTR2B)
				gst.GET("/gstr2b/batches", d.GSTHandler.ListGSTR2BBatches)
				gst.GET("/gstr2b/batches/:id", d.GSTHandler.GetGSTR2BBatch)
			}
		}
	}

	d.serveStatic(r)
	return r
}

// serveStatic mounts the compiled SPA from web/dist when present so a single
// binary can serve both the API and the frontend.
func (d Deps) serveStatic(r *gin.Engine) {
	const dist = "web/dist"
	if _, err := os.Stat(dist + "/index.html"); err != nil {
		return
	}
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api") {
			respondError(c, http.StatusNotFound, "endpoint not found")
			return
		}
		if path != "/" {
			if _, err := os.Stat(dist + path); err == nil {
				c.File(dist + path)
				return
			}
		}
		c.File(dist + "/index.html")
	})
}

// jsonErrorMiddleware guarantees every error response is JSON, never a bare string.
func jsonErrorMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}
		respondInternal(c, c.Errors.Last().Err)
	}
}