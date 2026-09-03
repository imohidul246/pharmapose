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
	PlatformRepo                *repository.PlatformRepo
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
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "X-Store-ID", "Authorization"},
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
		protected.Use(auth.RequireAuth(d.AuthRepo, d.CookieOptions), auth.CSRFProtect(d.DevOrigins), auth.ValidateStoreHeader())
		{
			protected.POST("/auth/logout", d.logout)
			protected.GET("/auth/me", d.me)
			protected.POST("/auth/change-password", d.changePassword)

			// Global platform administration: super-admin only. Deliberately
			// outside any tenant scope — the handlers list/renew/suspend any
			// store. RequireAuth binds the principal; RequirePlatformAdmin
			// rejects every non-admin (including store owners).
			platform := protected.Group("/platform")
			platform.Use(auth.RequirePlatformAdmin())
			{
				platform.GET("/stores", d.listPlatformStores)
				platform.POST("/stores/:id/renew", d.renewPlatformStore)
				platform.PUT("/stores/:id/status", d.updatePlatformStoreStatus)
				platform.GET("/stores/:id/payments", d.listPlatformStorePayments)
			}

			employees := protected.Group("/employees")
			{
				employees.Use(auth.RequireOwner())
				employees.GET("", d.listEmployees)
				employees.POST("", d.inviteEmployee)
				employees.DELETE("/:userID", d.deactivateEmployee)
			}

			store := protected.Group("/store")
			{
				// Store settings are admin-only: GET for viewing and PUT for
				// editing both require the owner (admin) role so a cashier
				// token receives 403 on either.
				store.Use(auth.RequireRole(auth.RoleAdmin))
				store.GET("", d.getStore)
				store.PUT("", d.updateStore)
			}

		purchases := protected.Group("/purchases")
		{
			purchases.GET("", auth.RequirePermission(auth.PermPurchaseView), d.listPurchases)
			// Direct inward posts stock immediately: owner-only. Employees
			// submit purchase-requests for approval instead.
			purchases.POST("", auth.RequireRole(auth.RoleStoreOwner), d.createPurchase)
		}

		// Employee-held workflows: submit here, the owner confirms below.
		// The approve/reject routes carry an explicit owner-role allow-list
		// so an employee (cashier) token can never approve a request, even
		// if the permission map is later widened.
		purchaseRequests := protected.Group("/purchase-requests")
		{
			purchaseRequests.POST("", auth.RequirePermission(auth.PermPurchaseCreate), d.createPurchaseRequest)
			purchaseRequests.GET("", auth.RequirePermission(auth.PermPurchaseView), d.listPurchaseRequests)
			purchaseRequests.GET("/:id", auth.RequirePermission(auth.PermPurchaseView), d.getPurchaseRequest)
			purchaseRequests.POST("/:id/approve", auth.RequireRole(auth.RoleStoreOwner), d.approvePurchaseRequest)
			purchaseRequests.POST("/:id/reject", auth.RequireRole(auth.RoleStoreOwner), d.rejectPurchaseRequest)
			purchaseRequests.POST("/:id/cancel", auth.RequirePermission(auth.PermPurchaseCreate), d.cancelPurchaseRequest)
		}

		stockAuditRequests := protected.Group("/stock-audit-requests")
		{
			stockAuditRequests.POST("", auth.RequirePermission(auth.PermStockAuditCreate), d.createStockAuditRequest)
			stockAuditRequests.GET("", auth.RequirePermission(auth.PermStockView), d.listStockAuditRequests)
			stockAuditRequests.GET("/:id", auth.RequirePermission(auth.PermStockView), d.getStockAuditRequest)
			stockAuditRequests.POST("/:id/approve", auth.RequireRole(auth.RoleStoreOwner), d.approveStockAuditRequest)
			stockAuditRequests.POST("/:id/reject", auth.RequireRole(auth.RoleStoreOwner), d.rejectStockAuditRequest)
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
			// Direct reconciliation force-corrects live stock: owner-only.
			// Both the mutation and the audit listing are guarded so a
			// cashier receives 403 on any reconcile action.
			inv.POST("/reconcile", auth.RequireRole(auth.RoleAdmin, auth.RoleStoreManager), d.reconcile)
			inv.GET("/reconciliations", auth.RequireRole(auth.RoleAdmin, auth.RoleStoreManager), d.listReconciliations)
		}

		// Canonical reconcile route group (alias of the inventory
		// reconciliation endpoints above): same handlers, same
		// admin/manager-only guards.
		reconcile := protected.Group("/reconcile")
		{
			reconcile.POST("", auth.RequireRole(auth.RoleAdmin, auth.RoleStoreManager), d.reconcile)
			reconcile.GET("", auth.RequireRole(auth.RoleAdmin, auth.RoleStoreManager), d.listReconciliations)
		}

		meds := protected.Group("/medicines")
		{
			meds.GET("", auth.RequirePermission(auth.PermStockView), d.listMedicines)
			meds.POST("", auth.RequirePermission(auth.PermStockView), d.createMedicine)
			meds.GET("/:id", auth.RequirePermission(auth.PermStockView), d.getMedicine)
			meds.GET("/:id/detail", auth.RequirePermission(auth.PermStockView), d.getMedicineDetail)
			meds.PUT("/:id", auth.RequirePermission(auth.PermStockView), d.updateMedicine)
			meds.DELETE("/:id", auth.RequirePermission(auth.PermStockView), d.deleteMedicine)
			meds.GET("/:id/tax-config", auth.RequirePermission(auth.PermStockView), d.getMedicineTaxConfig)
			// Tax overrides rewrite billing for the whole store: owner-only.
			meds.PUT("/:id/tax-config", auth.RequireRole(auth.RoleStoreOwner), d.upsertMedicineTaxConfig)
		}

		customers := protected.Group("/customers")
		{
			customers.GET("", auth.RequirePermission(auth.PermCustomerView), d.listCustomers)
			customers.POST("", auth.RequirePermission(auth.PermCustomerCreate), d.createCustomer)
			customers.GET("/:id", auth.RequirePermission(auth.PermCustomerView), d.getCustomer)
			customers.PUT("/:id", auth.RequirePermission(auth.PermCustomerCreate), d.updateCustomer)
			customers.GET("/:id/ledger", auth.RequirePermission(auth.PermKhataView), d.customerLedger)
			customers.POST("/:id/payments", auth.RequirePermission(auth.PermKhataView), d.recordPayment)
		}

		suppliers := protected.Group("/suppliers")
		{
			suppliers.GET("", auth.RequirePermission(auth.PermPurchaseView), d.listSuppliers)
			suppliers.POST("", auth.RequirePermission(auth.PermPurchaseCreate), d.createSupplier)
			suppliers.GET("/:id", auth.RequirePermission(auth.PermPurchaseView), d.getSupplier)
			suppliers.PUT("/:id", auth.RequirePermission(auth.PermPurchaseCreate), d.updateSupplier)
			suppliers.DELETE("/:id", auth.RequirePermission(auth.PermPurchaseCreate), d.deleteSupplier)
		}

		hsn := protected.Group("/hsn")
		{
			hsn.GET("", auth.RequirePermission(auth.PermStockView), d.listHSNCodes)
			hsn.POST("", auth.RequireRole(auth.RoleStoreOwner), d.createHSNCode)
			hsn.PUT("/:id/tax-rate", auth.RequireRole(auth.RoleStoreOwner), d.upsertTaxRate)
		}

			protected.POST("/sales/checkout", auth.RequirePermission(auth.PermSalesCreate), d.checkout)

			salesInvoices := protected.Group("/sales/invoices")
			{
				salesInvoices.GET("", auth.RequirePermission(auth.PermSalesView), d.listSalesInvoices)
				salesInvoices.GET("/resolve", auth.RequirePermission(auth.PermSalesView), d.getSalesInvoiceByNo)
				salesInvoices.GET("/:id", auth.RequirePermission(auth.PermSalesView), d.getSalesInvoice)
				salesInvoices.GET("/:id/pdf", auth.RequirePermission(auth.PermSalesView), PDFDeps{SaleRepo: d.SaleRepo, TaxRepo: d.TaxRepo, CustomerRepo: d.CustomerRepo}.generateB2BInvoicePDF)
			}

			purchaseInvoices := protected.Group("/purchases/invoices")
			{
				purchaseInvoices.GET("", auth.RequirePermission(auth.PermPurchaseView), d.listPurchaseInvoices)
				purchaseInvoices.GET("/:id", auth.RequirePermission(auth.PermPurchaseView), d.getPurchaseInvoice)
			}

			reports := protected.Group("/reports")
			{
				// Profit, sales and stock reports are sensitive: admin/manager
				// only. A cashier token receives 403 on every report route.
				reports.GET("/sales", auth.RequireRole(auth.RoleAdmin, auth.RoleStoreManager), d.salesReport)
				reports.GET("/purchase", auth.RequireRole(auth.RoleAdmin, auth.RoleStoreManager), d.purchaseReport)
				reports.GET("/profit-loss", auth.RequireRole(auth.RoleAdmin, auth.RoleStoreManager), d.profitLossReport)
				reports.GET("/expiry", auth.RequireRole(auth.RoleAdmin, auth.RoleStoreManager), d.expiryReport)
				reports.GET("/low-stock", auth.RequireRole(auth.RoleAdmin, auth.RoleStoreManager), d.lowStockReport)
			}

			gst := protected.Group("/gst")
			{
			gst.GET("/gstr1", auth.RequirePermission(auth.PermSalesView), d.GSTHandler.GetGSTR1)
			gst.GET("/gstr1/preview", auth.RequirePermission(auth.PermSalesView), d.GSTHandler.GetGSTR1Preview)
			gst.GET("/gstr1/excel", auth.RequirePermission(auth.PermSalesView), d.GSTHandler.DownloadGSTR1CSV)
			gst.GET("/gstr3b", auth.RequirePermission(auth.PermSalesView), d.GSTHandler.GetGSTR3B)
			gst.POST("/gstr2b/import", auth.RequirePermission(auth.PermPurchaseCreate), d.GSTHandler.ImportGSTR2B)
			gst.GET("/gstr2b/batches", auth.RequirePermission(auth.PermPurchaseView), d.GSTHandler.ListGSTR2BBatches)
			gst.GET("/gstr2b/batches/:id", auth.RequirePermission(auth.PermPurchaseView), d.GSTHandler.GetGSTR2BBatch)
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