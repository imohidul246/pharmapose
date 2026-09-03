package auth

// Role is the membership-level role that governs what a user can do in a store.
type Role string

const (
	// RoleStoreOwner is the store's admin: full access, approves requests,
	// manages employees and settings, and is the only role that may post
	// purchase inwards / stock reconciliations directly.
	RoleStoreOwner Role = "STORE_OWNER"
	// RoleEmployee is a seat-limited staff member. Employees submit purchase
	// and stock-audit requests for owner approval instead of mutating stock.
	RoleEmployee Role = "EMPLOYEE"
)

// Permission names the granular capabilities an employee may hold.
type Permission string

const (
	PermSalesCreate     Permission = "sales:create"
	PermSalesView       Permission = "sales:view"
	PermCustomerCreate  Permission = "customers:create"
	PermCustomerView    Permission = "customers:view"
	PermPurchaseCreate  Permission = "purchases:create" // → submit purchase requests
	PermPurchaseView    Permission = "purchases:view"
	PermStockAuditCreate Permission = "stock_audit:create" // → submit stock audit requests
	PermStockView       Permission = "stock:view"
	PermKhataView       Permission = "khata:view" // view customer ledger/payments
)

// Principal is the resolved identity for an authenticated request: the logged-in
// user, the store they belong to, and their membership role. Handlers MUST use
// Principal.StoreID for every store-scoped query and MUST ignore any client
// supplied store_id.
type Principal struct {
	UserID  string `json:"user_id"`
	Name    string `json:"name"`
	StoreID string `json:"store_id"`
	Role    Role   `json:"role"`
}

// employeePermissions is the exact capability set granted to an EMPLOYEE.
// Adding or removing a capability here changes authorization for every
// employee in every store.
var employeePermissions = map[Permission]bool{
	PermSalesCreate:      true,
	PermSalesView:        true,
	PermCustomerCreate:   true,
	PermCustomerView:     true,
	PermPurchaseCreate:   true,
	PermPurchaseView:     true,
	PermStockAuditCreate: true,
	PermStockView:        true,
	PermKhataView:        true,
}

// Can reports whether the given role holds the permission. STORE_OWNER
// implicitly holds every permission; EMPLOYEE holds the fixed employee set.
func Can(role Role, p Permission) bool {
	if role == RoleStoreOwner {
		return true
	}
	return role == RoleEmployee && employeePermissions[p]
}

// IsEmployee reports whether the role is exactly EMPLOYEE.
func IsEmployee(role Role) bool { return role == RoleEmployee }

// EmployeePermissions exposes the fixed employee capability set so the API can
// render, e.g. the permission list for the current principal.
func EmployeePermissions() map[Permission]bool {
	out := make(map[Permission]bool, len(employeePermissions))
	for p, ok := range employeePermissions {
		out[p] = ok
	}
	return out
}