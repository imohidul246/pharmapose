package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mohi/pms-marg-inspired/internal/auth"
	"github.com/mohi/pms-marg-inspired/internal/database"
	"github.com/mohi/pms-marg-inspired/internal/gst"
	"github.com/mohi/pms-marg-inspired/internal/handlers"
	"github.com/mohi/pms-marg-inspired/internal/repository"
)

func main() {
	addr := os.Getenv("PMS_ADDR")
	if addr == "" {
		addr = ":8082"
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/pms?sslmode=disable"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, dbURL)
	if err != nil {
		log.Fatalf("database connect: %v", err)
	}
	defer pool.Close()

	if err := database.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	storeID, err := repository.FirstStoreID(ctx, pool)
	if err != nil {
		log.Fatalf("resolve store: %v", err)
	}
	if envStore := os.Getenv("STORE_ID"); envStore != "" {
		storeID = envStore
	}
	if storeID == "" {
		log.Printf("no store found yet; the first /api/auth/register will bootstrap the tenant")
	}

	authRepo := repository.NewAuthRepo(pool)
	purchaseRequestRepo := repository.NewPurchaseRequestRepo(pool)
	stockAuditRequestRepo := repository.NewStockAuditRequestRepo(pool)

	cookieOptions := auth.CookieOptions{Secure: os.Getenv("PMS_COOKIE_SECURE") == "1"}
	devOrigins := []string{"http://localhost:5173"}

	supplierRepo := repository.NewSupplierRepo(pool, storeID)
	taxRepo := repository.NewTaxRepo(pool)

	router := handlers.NewRouter(handlers.Deps{
		AuthRepo:              authRepo,
		PurchaseRequestRepo:   purchaseRequestRepo,
		StockAuditRequestRepo: stockAuditRequestRepo,
		CookieOptions:         cookieOptions,
		DevOrigins:            devOrigins,
		MedicineRepo:          repository.NewMedicineRepo(pool, storeID),
		CustomerRepo:          repository.NewCustomerRepo(pool, storeID),
		SaleRepo:              repository.NewSaleRepo(pool),
		PurchaseRepo:          repository.NewPurchaseRepo(pool),
		ReconcileRepo:         repository.NewReconcileRepo(pool),
		ReportRepo:            repository.NewReportRepo(pool),
		SupplierRepo:          supplierRepo,
		TaxRepo:               taxRepo,
		GSTHandler:            gst.NewHandler(pool),
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("pharmacy ERP listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}
