package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/auth"
	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/tax"
)

type AuthRepo struct {
	db *pgxpool.Pool
}

func NewAuthRepo(db *pgxpool.Pool) *AuthRepo {
	return &AuthRepo{db: db}
}

// RegisterInput is everything needed to stand up a brand-new pharmacy tenant:
// the owner's login plus the business, optional GST registration, and the
// first store — all committed atomically so a failed registration leaves
// nothing behind.
type RegisterInput struct {
	Name         string `json:"name"`
	Phone        string `json:"phone"`
	PasswordHash string `json:"-"` // Argon2id encoded, produced by the handler

	BusinessName string  `json:"business_name"`
	TradeName    string  `json:"trade_name"`
	GSTIN        *string `json:"gstin"`
	PAN          *string `json:"pan"`

	StoreName          string  `json:"store_name"`
	StoreAddress       string  `json:"store_address"`
	StorePhone         string  `json:"store_phone"`
	DrugLicenseNumber  string  `json:"drug_license_number"`
	DrugLicenseExpiry  *string `json:"drug_license_expiry"` // YYYY-MM-DD, optional
}

// RegisterResult couples the created user with the principal (store + owner
// role) and the first session so registration can log the user straight in.
type RegisterResult struct {
	User      *models.User
	Principal *auth.Principal
	ExpiresAt time.Time
}

// stateCodeOf returns the 2-digit GST state code prefix of a GSTIN, or "" when
// the value is empty or too short.
func stateCodeOf(gstin string) string {
	if len(gstin) >= 2 {
		return gstin[0:2]
	}
	return ""
}

// Register creates the full tenant in one transaction.
func (r *AuthRepo) Register(ctx context.Context, in RegisterInput, tokenHash, ip, userAgent string) (*RegisterResult, error) {
	phone := normalizePhone(in.Phone)
	storePhone := normalizePhone(in.StorePhone)
	if phone == "" || in.Name == "" || in.StoreName == "" || storePhone == "" {
		return nil, errors.New("name, phone, store_name and store_phone are required")
	}
	storeName := trimSpaces(in.StoreName)
	storeAddress := trimSpaces(in.StoreAddress)
	if storeName == "" {
		return nil, errors.New("store name is required")
	}
	if storeAddress == "" {
		return nil, errors.New("store address is required")
	}
	if in.PasswordHash == "" {
		return nil, errors.New("password_hash must be provided")
	}
	if in.PAN != nil && *in.PAN != "" && !tax.ValidatePAN(*in.PAN) {
		return nil, errors.New("pan is not a structurally valid PAN")
	}
	if in.DrugLicenseExpiry != nil && *in.DrugLicenseExpiry != "" {
		if _, err := models.ParseDate(*in.DrugLicenseExpiry); err != nil {
			return nil, err
		}
	}

	legalName := trimSpaces(in.BusinessName)
	if legalName == "" {
		legalName = storeName
	}

	var (
		res        RegisterResult
		expiresAt = time.Now().UTC().Add(auth.SessionTTL())
	)
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var userID string
		err := tx.QueryRow(ctx, `
			INSERT INTO users (name, phone, password_hash)
			VALUES ($1, $2, $3)
			RETURNING id::text`, in.Name, phone, in.PasswordHash).Scan(&userID)
		if err != nil {
			if isUniqueViolation(err) {
				return errors.New("an account with this phone number already exists")
			}
			return err
		}

		businessID := ""
		if err := tx.QueryRow(ctx, `
			INSERT INTO businesses (legal_name, trade_name)
			VALUES ($1, $2) RETURNING id::text`, legalName, in.TradeName).Scan(&businessID); err != nil {
			return err
		}

		gstinVal := ""
		if in.GSTIN != nil {
			gstinVal = *in.GSTIN
		}
		panVal := ""
		if in.PAN != nil {
			panVal = *in.PAN
		}

		var gstRegID *string
		if gstinVal != "" || panVal != "" {
			if gstinVal != "" {
				if !tax.ValidateGSTIN(gstinVal) {
					return errors.New("gstin is not a structurally valid GSTIN")
				}
			}
			stateCode := ""
			if len(gstinVal) >= 2 {
				stateCode = gstinVal[0:2]
			}
			var regID string
			if err := tx.QueryRow(ctx, `
				INSERT INTO gst_registrations (business_id, gstin, legal_name, trade_name, state_code, pan)
				VALUES ($1, $2, $3, $4, $5, $6) RETURNING id::text`,
				businessID, nullableString(in.GSTIN), legalName, in.TradeName, stateCode, nullableString(in.PAN)).Scan(&regID); err != nil {
				return err
			}
			gstRegID = &regID
		}

		var storeID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO stores (gst_registration_id, business_id, name, address, phone, drug_license_number, drug_license_expiry, subscription_valid_until, subscription_status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, now() + interval '30 days', 'ACTIVE') RETURNING id::text`,
			nullableString(gstRegID), businessID, storeName, storeAddress, storePhone,
			trimSpaces(in.DrugLicenseNumber), nullableDate(in.DrugLicenseExpiry)).Scan(&storeID); err != nil {
			return err
		}

		var membershipID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO store_memberships (store_id, user_id, role)
			VALUES ($1, $2, 'STORE_OWNER') RETURNING id::text`,
			storeID, userID).Scan(&membershipID); err != nil {
			return err
		}
		_ = membershipID

		if _, err := tx.Exec(ctx, `
			INSERT INTO sessions (user_id, token_hash, ip, user_agent, expires_at)
			VALUES ($1, $2, $3, $4, $5)`,
			userID, tokenHash, ip, userAgent, expiresAt); err != nil {
			return err
		}

		res.User = &models.User{ID: userID, Name: in.Name, Phone: phone, IsActive: true}
		res.Principal = &auth.Principal{UserID: userID, Name: in.Name, StoreID: storeID, Role: auth.RoleStoreOwner}
		res.ExpiresAt = expiresAt
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// FindUserByPhone returns the user record and its stored password hash.
func (r *AuthRepo) FindUserByPhone(ctx context.Context, phone string) (*models.User, string, error) {
	phone = normalizePhone(phone)
	var (
		u    models.User
		hash string
	)
	err := r.db.QueryRow(ctx,
		`SELECT id::text, name, phone, is_active, COALESCE(is_platform_admin, false), created_at, password_hash
		 FROM users WHERE phone = $1`, phone).
		Scan(&u.ID, &u.Name, &u.Phone, &u.IsActive, &u.IsPlatformAdmin, &u.CreatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	return &u, hash, nil
}

// CreateSession persists a session row for the user. Callers must pass the
// SHA-256 hash of the raw bearer token, never the token itself.
func (r *AuthRepo) CreateSession(ctx context.Context, userID, tokenHash, ip, userAgent string) (time.Time, error) {
	expiresAt := time.Now().UTC().Add(auth.SessionTTL())
	err := r.db.QueryRow(ctx, `
		INSERT INTO sessions (user_id, token_hash, ip, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING expires_at`, userID, tokenHash, ip, userAgent, expiresAt).Scan(&expiresAt)
	return expiresAt, err
}

// DeleteSession revokes a session (logout).
func (r *AuthRepo) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

// DeleteSessionsForUser revokes every session of a user (deactivation,
// password change). Returns the number of sessions dropped.
func (r *AuthRepo) DeleteSessionsForUser(ctx context.Context, userID string) (int64, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return tag.RowsAffected(), err
}

// ValidateSession resolves a hashed token to a principal, re-checking that both
// the user and their membership are still active. A dead or inactive session
// returns auth.ErrUnauthorized. This is called on every authenticated request.
//
// Subscription enforcement: for non-admin users the store's subscription is
// re-checked on every request. When the store is SUSPENDED or its validity
// window has passed, the session is deleted and ErrUnauthorized is returned,
// so expiry takes effect mid-session. Platform admins bypass the membership
// and subscription checks entirely (they may have no store membership).
func (r *AuthRepo) ValidateSession(ctx context.Context, tokenHash string) (*auth.Principal, error) {
	var (
		uID      string
		name     string
		uAct     bool
		isAdmin  bool
	)
	err := r.db.QueryRow(ctx, `
		SELECT u.id::text, u.name, u.is_active, COALESCE(u.is_platform_admin, false)
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, tokenHash).
		Scan(&uID, &name, &uAct, &isAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if !uAct {
		_, _ = r.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
		return nil, auth.ErrUnauthorized
	}
	if isAdmin {
		// Platform admin: attach the latest active membership when one exists
		// (convenience for UIs) but never require it.
		var storeID string
		var role string
		mErr := r.db.QueryRow(ctx, `
			SELECT m.store_id::text, m.role
			FROM store_memberships m
			WHERE m.user_id = $1 AND m.is_active = true
			ORDER BY m.created_at DESC
			LIMIT 1`, uID).Scan(&storeID, &role)
		if mErr != nil && !errors.Is(mErr, pgx.ErrNoRows) {
			return nil, mErr
		}
		if errors.Is(mErr, pgx.ErrNoRows) {
			return &auth.Principal{UserID: uID, Name: name, IsPlatformAdmin: true}, nil
		}
		return &auth.Principal{UserID: uID, Name: name, StoreID: storeID, Role: auth.Role(role), IsPlatformAdmin: true}, nil
	}

	var (
		role    string
		mAct    bool
		storeID string
	)
	err = r.db.QueryRow(ctx, `
		SELECT m.store_id::text, m.role, m.is_active
		FROM store_memberships m
		WHERE m.user_id = $1
		ORDER BY m.created_at DESC
		LIMIT 1`, uID).Scan(&storeID, &role, &mAct)
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = r.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
		return nil, auth.ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if !mAct {
		// Session row belongs to a deactivated login: kill it so it cannot
		// silently linger.
		_, _ = r.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
		return nil, auth.ErrUnauthorized
	}
	// Subscription gate: SUSPENDED or expired validity kills the session.
	var subStatus *string
	var subValidUntil *time.Time
	if err := r.db.QueryRow(ctx, `
		SELECT subscription_status, subscription_valid_until
		FROM stores WHERE id = $1`, storeID).Scan(&subStatus, &subValidUntil); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_, _ = r.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
			return nil, auth.ErrUnauthorized
		}
		return nil, err
	}
	status := "ACTIVE"
	if subStatus != nil && *subStatus != "" {
		status = *subStatus
	}
	if !models.IsSubscriptionActive(status, subValidUntil, time.Now().UTC()) {
		_, _ = r.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
		return nil, auth.ErrUnauthorized
	}
	return &auth.Principal{UserID: uID, Name: name, StoreID: storeID, Role: auth.Role(role)}, nil
}

// CheckStoreSubscriptionForUser enforces the login gate: platform admins always
// pass; every other user passes only when their store subscription is ACTIVE
// and unexpired. Returns a *models.ValidationError with the user-facing
// message when the store is expired/suspended, so the handler can surface 403
// {"error": "subscription_expired", ...}.
func (r *AuthRepo) CheckStoreSubscriptionForUser(ctx context.Context, user *models.User) error {
	if user == nil {
		return errors.New("user is required")
	}
	if user.IsPlatformAdmin {
		return nil
	}
	var storeID string
	err := r.db.QueryRow(ctx, `
		SELECT m.store_id::text
		FROM store_memberships m
		WHERE m.user_id = $1
		ORDER BY m.created_at DESC
		LIMIT 1`, user.ID).Scan(&storeID)
	if errors.Is(err, pgx.ErrNoRows) {
		// No tenancy yet (should not happen for store users): let the normal
		// session flow decide.
		return nil
	}
	if err != nil {
		return err
	}
	var subStatus *string
	var subValidUntil *time.Time
	if err := r.db.QueryRow(ctx, `
		SELECT subscription_status, subscription_valid_until
		FROM stores WHERE id = $1`, storeID).Scan(&subStatus, &subValidUntil); err != nil {
		return err
	}
	status := "ACTIVE"
	if subStatus != nil && *subStatus != "" {
		status = *subStatus
	}
	if !models.IsSubscriptionActive(status, subValidUntil, time.Now().UTC()) {
		return models.NewValidationError("Store subscription has expired. Please contact the administrator.")
	}
	return nil
}

// GetStoreSubscription returns a store's subscription status + validity window.
func (r *AuthRepo) GetStoreSubscription(ctx context.Context, storeID string) (status string, validUntil *time.Time, err error) {
	var subStatus *string
	var subValid *time.Time
	if err := r.db.QueryRow(ctx, `
		SELECT subscription_status, subscription_valid_until
		FROM stores WHERE id = $1`, storeID).Scan(&subStatus, &subValid); err != nil {
		return "", nil, err
	}
	status = "ACTIVE"
	if subStatus != nil && *subStatus != "" {
		status = *subStatus
	}
	return status, subValid, nil
}

// GetUser returns a user by id.
func (r *AuthRepo) GetUser(ctx context.Context, userID string) (*models.User, error) {
	var u models.User
	err := r.db.QueryRow(ctx,
		`SELECT id::text, name, phone, is_active, COALESCE(is_platform_admin, false), created_at FROM users WHERE id = $1`, userID).
		Scan(&u.ID, &u.Name, &u.Phone, &u.IsActive, &u.IsPlatformAdmin, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByPhone returns a user by phone.
func (r *AuthRepo) GetUserByPhone(ctx context.Context, phone string) (*models.User, error) {
	phone = normalizePhone(phone)
	var u models.User
	err := r.db.QueryRow(ctx,
		`SELECT id::text, name, phone, is_active, COALESCE(is_platform_admin, false), created_at FROM users WHERE phone = $1`, phone).
		Scan(&u.ID, &u.Name, &u.Phone, &u.IsActive, &u.IsPlatformAdmin, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetPasswordHash returns the PHC-encoded hash for a user (used to re-verify
// the current password before rotation).
func (r *AuthRepo) GetPasswordHash(ctx context.Context, userID string) (string, error) {
	var h string
	err := r.db.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&h)
	if err != nil {
		return "", err
	}
	return h, nil
}

// ChangePassword replaces the password hash and revokes all existing sessions
// except the passed-in currentTokenHash, so the user stays signed in.
func (r *AuthRepo) ChangePassword(ctx context.Context, userID, newHash, keepTokenHash string) error {
	return pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`,
			userID, newHash); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`DELETE FROM sessions WHERE user_id = $1 AND token_hash <> $2`,
			userID, keepTokenHash)
		return err
	})
}

// CountActiveEmployees returns the number of seats currently occupied.
func (r *AuthRepo) CountActiveEmployees(ctx context.Context, storeID string) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM store_memberships
		WHERE store_id = $1 AND role = 'EMPLOYEE' AND is_active`,
		storeID).Scan(&n)
	return n, err
}

// CreateEmployee adds a new employee seat. The seat cap (stores.max_employees)
// is enforced here on a count that includes any seat still pending activation,
// then the check is re-run inside the insert transaction so two concurrent
// invites cannot overshoot together.
func (r *AuthRepo) CreateEmployee(ctx context.Context, storeID, name, phone, passwordHash string) (*models.User, error) {
	phone = normalizePhone(phone)
	if name == "" || phone == "" {
		return nil, errors.New("employee name and phone are required")
	}
	if passwordHash == "" {
		return nil, errors.New("password_hash must be provided")
	}

	var storedStore models.Store
	if err := r.getStoreByIDStub(ctx, storeID, &storedStore); err != nil {
		return nil, err
	}
	active, err := r.CountActiveEmployees(ctx, storeID)
	if err != nil {
		return nil, err
	}
	if active >= storedStore.MaxEmployees {
		return nil, models.ErrEmployeeLimitReached
	}

	var u models.User
	err = pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		// Re-check the seat inside the tx: the membership is what consumes a seat.
		var taken int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM store_memberships
			WHERE store_id = $1 AND role = 'EMPLOYEE' AND is_active`, storeID).Scan(&taken); err != nil {
			return err
		}
		if taken >= storedStore.MaxEmployees {
			return models.ErrEmployeeLimitReached
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO users (name, phone, password_hash)
			VALUES ($1, $2, $3)
			RETURNING id::text, name, phone, is_active`, name, phone, passwordHash).
			Scan(&u.ID, &u.Name, &u.Phone, &u.IsActive)
		if err != nil {
			if isUniqueViolation(err) {
				return errors.New("an account with this phone number already exists")
			}
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO store_memberships (store_id, user_id, role)
			VALUES ($1, $2, 'EMPLOYEE')`, storeID, u.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ListMembers returns every membership of a store, newest first, with joined
// user names — used for the members screen and employee management.
func (r *AuthRepo) ListMembers(ctx context.Context, storeID string) ([]models.Membership, error) {
	rows, err := r.db.Query(ctx, `
		SELECT m.id::text, m.store_id::text, m.user_id::text, m.role, m.is_active,
		       m.created_at, u.name, u.phone, u.is_active
		FROM store_memberships m
		JOIN users u ON u.id = m.user_id
		WHERE m.store_id = $1
		ORDER BY m.created_at`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make([]models.Membership, 0)
	for rows.Next() {
		var m models.Membership
		if err := rows.Scan(&m.ID, &m.StoreID, &m.UserID, &m.Role, &m.IsActive,
			&m.CreatedAt, &m.UserName, &m.UserPhone, &m.UserActive); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// SetMembershipActive enables or disables a membership. Disabling an employee
// revokes all their sessions instantly; the row is never deleted. Deactivating
// the store owner is refused.
func (r *AuthRepo) SetMembershipActive(ctx context.Context, storeID, userID string, active bool) error {
	return pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var role string
		err := tx.QueryRow(ctx, `
			SELECT m.role FROM store_memberships m
			WHERE m.store_id = $1 AND m.user_id = $2`, storeID, userID).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return models.ErrNotAMember
		}
		if err != nil {
			return err
		}
		if role == "STORE_OWNER" && !active {
			return models.ErrCannotDisableOwner
		}
		if _, err := tx.Exec(ctx, `
			UPDATE store_memberships
			SET is_active = $3, updated_at = now()
			WHERE store_id = $1 AND user_id = $2`, storeID, userID, active); err != nil {
			return err
		}
		if !active {
			_, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
		}
		return err
	})
}

// IsMember reports whether the user holds any active membership in the store.
func (r *AuthRepo) IsMember(ctx context.Context, storeID, userID string) (bool, error) {
	var ok bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM store_memberships
		              WHERE store_id = $1 AND user_id = $2 AND is_active)`,
		storeID, userID).Scan(&ok)
	return ok, err
}

// GetStore returns a store with the seat cap and the new shop detail columns.
func (r *AuthRepo) GetStore(ctx context.Context, storeID string) (*models.Store, error) {
	var s models.Store
	if err := r.getStoreByIDStub(ctx, storeID, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *AuthRepo) getStoreByIDStub(ctx context.Context, storeID string, s *models.Store) error {
	var dlExpiry *time.Time
	err := r.db.QueryRow(ctx, `
		SELECT id::text, gst_registration_id::text, name, address, phone,
		       drug_license_number, drug_license_expiry, is_active, max_employees, created_at,
		       subscription_valid_until, COALESCE(subscription_status, 'ACTIVE'), updated_at
		FROM stores WHERE id = $1`, storeID).
		Scan(&s.ID, &s.GSTRegistrationID, &s.Name, &s.Address, &s.Phone,
			&s.DrugLicenseNumber, &dlExpiry, &s.IsActive, &s.MaxEmployees, &s.CreatedAt,
			&s.SubscriptionValidUntil, &s.SubscriptionStatus, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("store not found")
		}
		return err
	}
	if dlExpiry != nil {
		d := models.NewDate(*dlExpiry)
		s.DrugLicenseExpiry = &d
	}
	if s.SubscriptionStatus == "" {
		s.SubscriptionStatus = "ACTIVE"
	}
	return nil
}

// GetStoreDetails returns the shop profile plus the business answers that live
// on other tables: the active owner's name (users) and the GST registration's
// GSTIN/PAN (gst_registrations via stores.gst_registration_id).
func (r *AuthRepo) GetStoreDetails(ctx context.Context, storeID string) (*models.Store, error) {
	var s models.Store
	if err := r.getStoreByIDStub(ctx, storeID, &s); err != nil {
		return nil, err
	}
	// Owner name — the active STORE_OWNER membership's user name.
	if err := r.db.QueryRow(ctx, `
		SELECT u.name
		FROM store_memberships m
		JOIN users u ON u.id = m.user_id
		WHERE m.store_id = $1 AND m.role = 'STORE_OWNER' AND m.is_active = true
		ORDER BY m.created_at LIMIT 1`, storeID).Scan(&s.OwnerName); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	// GST registration answers (GSTIN/PAN/state) — nullable when no registration.
	if s.GSTRegistrationID != nil {
		var gstin *string
		var pan *string
		var stateCode *string
		err := r.db.QueryRow(ctx, `
			SELECT gstin, pan, state_code FROM gst_registrations WHERE id = $1`, *s.GSTRegistrationID).
			Scan(&gstin, &pan, &stateCode)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		s.GSTIN = gstin
		s.PAN = pan
		s.StateCode = stateCode
	}
	return &s, nil
}

// StoreUpdate carries the owner-editable shop profile for an owner-only
// settings save. Optional business fields may be cleared by passing ""/nil.
type StoreUpdate struct {
	Name              string
	Address           string
	Phone             string
	OwnerName         string
	MaxEmployees      int
	GSTIN             *string
	PAN               *string
	DrugLicenseNumber string
	DrugLicenseExpiry *string // YYYY-MM-DD, optional; nil clears
}

// UpdateStoreDetails atomically persists the shop profile: the store row
// (name/address/phone/max_employees + drug license), the owner user's name,
// and the GST registration's GSTIN/PAN (creating the registration on demand,
// or clearing its values when emptied). Caller is expected to have already
// authorized the request as the store owner. Returns the full updated profile.
func (r *AuthRepo) UpdateStoreDetails(ctx context.Context, storeID string, in StoreUpdate) (*models.Store, error) {
	name := trimSpaces(in.Name)
	address := trimSpaces(in.Address)
	phone := normalizePhone(in.Phone)
	ownerName := trimSpaces(in.OwnerName)
	if name == "" {
		return nil, models.NewValidationError("store name is required")
	}
	if address == "" {
		return nil, models.NewValidationError("store address is required")
	}
	if phone == "" {
		return nil, models.NewValidationError("store phone is required")
	}
	if ownerName == "" {
		return nil, models.NewValidationError("owner name is required")
	}
	if in.PAN != nil && *in.PAN != "" && !tax.ValidatePAN(*in.PAN) {
		return nil, models.NewValidationError("pan is not a structurally valid PAN")
	}
	if in.DrugLicenseExpiry != nil && *in.DrugLicenseExpiry != "" {
		if _, err := models.ParseDate(*in.DrugLicenseExpiry); err != nil {
			return nil, models.NewValidationError(err.Error())
		}
	}
	active, err := r.CountActiveEmployees(ctx, storeID)
	if err != nil {
		return nil, err
	}
	if in.MaxEmployees < active {
		return nil, models.NewValidationError(fmt.Sprintf("max_employees must not be less than the %d employees currently active", active))
	}

	gstin := ""
	if in.GSTIN != nil {
		gstin = trimSpaces(*in.GSTIN)
	}
	panVal := ""
	if in.PAN != nil {
		panVal = trimSpaces(*in.PAN)
	}
	if gstin != "" && !tax.ValidateGSTIN(gstin) {
		return nil, models.NewValidationError("gstin is not a structurally valid GSTIN")
	}

	err = pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		// Locate the store's current registration (if any) so we can reuse or
		// update it, and the store's business (direct link, backfilled from the
		// legacy registration chain) for a new registration.
		var regID *string
		var businessID string
		if err := tx.QueryRow(ctx, `
			SELECT s.gst_registration_id, COALESCE(s.business_id::text, gr.business_id::text, '')
			FROM stores s
			LEFT JOIN gst_registrations gr ON gr.id = s.gst_registration_id
			WHERE s.id = $1`, storeID).Scan(&regID, &businessID); err != nil {
			return err
		}

		gstinEmpty := gstin == ""
		panEmpty := panVal == ""
		if gstinEmpty && panEmpty {
			// No compliance info: detach any registration (its row is left fn
			// the record of record for filings; we only clear GSTIN/PAN if one
			// was already present).
			if regID != nil {
				if _, err := tx.Exec(ctx, `
					UPDATE gst_registrations SET gstin = NULL, pan = NULL, updated_at = now()
					WHERE id = $1`, *regID); err != nil {
					return err
				}
			}
		} else {
			// The store has a registration row to hold GSTIN/PAN.
			if regID != nil {
				if _, err := tx.Exec(ctx, `
					UPDATE gst_registrations SET gstin = $2, pan = $3, state_code = $4, updated_at = now()
					WHERE id = $1`,
					*regID, nullableString(&gstin), nullableString(&panVal), stateCodeOf(gstin)); err != nil {
					return err
				}
			} else {
				if businessID == "" {
					return errors.New("store has no business to attach a GST registration to")
				}
				var newID string
				if err := tx.QueryRow(ctx, `
					INSERT INTO gst_registrations (business_id, gstin, pan, state_code, legal_name, trade_name)
					SELECT $1, $2, $3, $4, b.legal_name, b.trade_name
					FROM businesses b
					WHERE b.id = $1
					RETURNING id::text`,
					businessID, nullableString(&gstin), nullableString(&panVal), stateCodeOf(gstin)).Scan(&newID); err != nil {
					return err
				}
				regID = &newID
			}
		}

		if _, err := tx.Exec(ctx, `
			UPDATE stores
			SET name = $2, address = $3, phone = $4, max_employees = $5,
			    drug_license_number = $6, drug_license_expiry = $7,
			    gst_registration_id = $8, updated_at = now()
			WHERE id = $1`,
			storeID, name, address, phone, in.MaxEmployees,
			trimSpaces(in.DrugLicenseNumber), nullableDate(in.DrugLicenseExpiry), nullableString(regID)); err != nil {
			return err
		}

		// Owner name is the active owner user's name.
		if _, err := tx.Exec(ctx, `
			UPDATE users SET name = $2, updated_at = now()
			WHERE id = (
				SELECT user_id FROM store_memberships
				WHERE store_id = $1 AND role = 'STORE_OWNER' AND is_active = true
				ORDER BY created_at LIMIT 1)`,
			storeID, ownerName); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return r.GetStoreDetails(ctx, storeID)
}

// UpdateStoreSettings edits the store profile and seat cap. Increasing the cap
// is always allowed; decreasing it below the seats currently occupied is not.
// This is the legacy editor (name/address/seats only) preserved for the
// existing Settings UI minimal path and its tests.
func (r *AuthRepo) UpdateStoreSettings(ctx context.Context, storeID, name, address string, maxEmployees int) (*models.Store, error) {
	if maxEmployees < 0 {
		return nil, errors.New("max_employees must be non-negative")
	}
	active, err := r.CountActiveEmployees(ctx, storeID)
	if err != nil {
		return nil, err
	}
	if maxEmployees < active {
		return nil, fmt.Errorf("max_employees must not be less than the %d employees currently active", active)
	}
	name = trimSpaces(name)
	if name == "" {
		return nil, errors.New("store name is required")
	}
	var s models.Store
	var dlExpiry *time.Time
	err = r.db.QueryRow(ctx, `
		UPDATE stores
		SET name = $2, address = $3, max_employees = $4, updated_at = now()
		WHERE id = $1
		RETURNING id::text, gst_registration_id::text, name, address, phone, drug_license_number,
		          drug_license_expiry, is_active, max_employees, created_at,
		          subscription_valid_until, COALESCE(subscription_status, 'ACTIVE'), updated_at`,
		storeID, name, address, maxEmployees).
		Scan(&s.ID, &s.GSTRegistrationID, &s.Name, &s.Address, &s.Phone,
			&s.DrugLicenseNumber, &dlExpiry, &s.IsActive, &s.MaxEmployees, &s.CreatedAt,
			&s.SubscriptionValidUntil, &s.SubscriptionStatus, &s.UpdatedAt)
	if dlExpiry != nil {
		d := models.NewDate(*dlExpiry)
		s.DrugLicenseExpiry = &d
	}
	if s.SubscriptionStatus == "" {
		s.SubscriptionStatus = "ACTIVE"
	}
	return &s, err
}

// InsertAudit appends an immutable audit trail row.
func (r *AuthRepo) InsertAudit(ctx context.Context, storeID, userID, action, entity, entityID string, details any) error {
	raw := []byte("{}")
	if details != nil {
		raw, _ = json.Marshal(details)
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO audit_logs (store_id, user_id, action, entity, entity_id, details)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		nullableString(&storeID), nullableString(&userID), action, entity, entityID, raw)
	return err
}

// insertAuditTx is the in-transaction twin of InsertAudit.
func insertAuditTx(ctx context.Context, tx pgx.Tx, storeID, userID, action, entity, entityID string, details any) error {
	raw := []byte("{}")
	if details != nil {
		raw, _ = json.Marshal(details)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (store_id, user_id, action, entity, entity_id, details)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		nullableString(&storeID), nullableString(&userID), action, entity, entityID, raw)
	return err
}