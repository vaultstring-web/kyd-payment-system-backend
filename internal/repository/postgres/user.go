package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"kyd/internal/domain"
	"kyd/internal/security"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type UserRepository struct {
	db     *sqlx.DB
	crypto *security.CryptoService
}

func NewUserRepository(db *sqlx.DB, crypto *security.CryptoService) *UserRepository {
	return &UserRepository{db: db, crypto: crypto}
}

func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	// Encrypt sensitive fields
	encEmail, err := r.crypto.Encrypt(user.Email)
	if err != nil {
		return errors.Wrap(err, "failed to encrypt email")
	}
	encPhone, err := r.crypto.Encrypt(user.Phone)
	if err != nil {
		return errors.Wrap(err, "failed to encrypt phone")
	}
	encFirstName, err := r.crypto.Encrypt(user.FirstName)
	if err != nil {
		return errors.Wrap(err, "failed to encrypt first name")
	}
	encLastName, err := r.crypto.Encrypt(user.LastName)
	if err != nil {
		return errors.Wrap(err, "failed to encrypt last name")
	}
	var encTOTPSecret *string
	if user.TOTPSecret != nil {
		enc, err := r.crypto.Encrypt(*user.TOTPSecret)
		if err != nil {
			return errors.Wrap(err, "failed to encrypt TOTP secret")
		}
		encTOTPSecret = &enc
	}

	// Generate blind indexes
	emailHash := r.crypto.BlindIndex(user.Email)
	var phoneHash *string
	if user.Phone != "" {
		ph := r.crypto.BlindIndex(user.Phone)
		phoneHash = &ph
	}

	query := `
		INSERT INTO customer_schema.users (
			id, email, phone, password_hash, first_name, last_name,
			user_type, kyc_level, kyc_status, country_code, date_of_birth,
			business_name, risk_score, is_active, created_at, updated_at,
			email_hash, phone_hash, totp_secret, is_totp_enabled,
			bio, city, postal_code, tax_id
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
			$21, $22, $23, $24
		)
	`

	_, err = r.db.ExecContext(ctx, query,
		user.ID, encEmail, encPhone, user.PasswordHash, encFirstName, encLastName,
		user.UserType, user.KYCLevel, user.KYCStatus, user.CountryCode, user.DateOfBirth,
		user.BusinessName, user.RiskScore, user.IsActive, user.CreatedAt, user.UpdatedAt,
		emailHash, phoneHash, encTOTPSecret, user.IsTOTPEnabled,
		user.Bio, user.City, user.PostalCode, user.TaxID,
	)

	if err != nil {
		return errors.Wrap(err, "failed to create user")
	}

	return nil
}

func (r *UserRepository) decryptUser(user *domain.User) error {
	var err error
	if user.Email != "" {
		user.Email, err = r.crypto.Decrypt(user.Email)
		if err != nil {
			return errors.Wrap(err, "failed to decrypt email")
		}
	}
	if user.Phone != "" {
		user.Phone, err = r.crypto.Decrypt(user.Phone)
		if err != nil {
			return errors.Wrap(err, "failed to decrypt phone")
		}
	}
	if user.FirstName != "" {
		user.FirstName, err = r.crypto.Decrypt(user.FirstName)
		if err != nil {
			return errors.Wrap(err, "failed to decrypt first name")
		}
	}
	if user.LastName != "" {
		user.LastName, err = r.crypto.Decrypt(user.LastName)
		if err != nil {
			return errors.Wrap(err, "failed to decrypt last name")
		}
	}
	if user.TOTPSecret != nil {
		dec, err := r.crypto.Decrypt(*user.TOTPSecret)
		if err != nil {
			return errors.Wrap(err, "failed to decrypt TOTP secret")
		}
		user.TOTPSecret = &dec
	}
	return nil
}

func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	var user domain.User
	query := `
		SELECT 
			id, email, phone, password_hash, first_name, last_name,
			user_type, kyc_level, kyc_status, country_code, date_of_birth,
			business_name, business_registration, risk_score, is_active,
			email_verified, totp_secret, is_totp_enabled, last_login,
			failed_login_attempts, locked_until, created_at, updated_at,
			bio, city, postal_code, tax_id
		FROM customer_schema.users WHERE id = $1`

	err := r.db.GetContext(ctx, &user, query, id)
	if err == sql.ErrNoRows {
		return nil, errors.ErrUserNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find user")
	}

	if err := r.decryptUser(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

func (r *UserRepository) FindByIDs(ctx context.Context, ids []uuid.UUID) ([]*domain.User, error) {
	if len(ids) == 0 {
		return []*domain.User{}, nil
	}
	var users []*domain.User
	query, args, err := sqlx.In(`
		SELECT 
			id, email, phone, first_name, last_name, user_type, kyc_level, kyc_status,
			country_code, date_of_birth, business_name, risk_score, is_active,
			failed_login_attempts, locked_until, last_login, created_at, updated_at, is_totp_enabled,
			bio, city, postal_code, tax_id
		FROM customer_schema.users
		WHERE id IN (?)`, ids)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build query")
	}
	query = r.db.Rebind(query)

	err = r.db.SelectContext(ctx, &users, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch users by ids")
	}

	for _, user := range users {
		if err := r.decryptUser(user); err != nil {
			return nil, err
		}
	}

	return users, nil
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	var user domain.User
	// Use blind index for search
	emailHash := r.crypto.BlindIndex(email)
	query := `
		SELECT 
			id, email, phone, password_hash, first_name, last_name,
			user_type, kyc_level, kyc_status, country_code, date_of_birth,
			business_name, business_registration, risk_score, is_active,
			email_verified, totp_secret, is_totp_enabled, last_login,
			failed_login_attempts, locked_until, created_at, updated_at,
			bio, city, postal_code, tax_id
		FROM customer_schema.users WHERE email_hash = $1`

	err := r.db.GetContext(ctx, &user, query, emailHash)
	if err == sql.ErrNoRows {
		return nil, errors.ErrUserNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find user")
	}

	if err := r.decryptUser(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

func (r *UserRepository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	var exists bool
	emailHash := r.crypto.BlindIndex(email)
	query := `SELECT EXISTS(SELECT 1 FROM customer_schema.users WHERE email_hash = $1)`

	err := r.db.GetContext(ctx, &exists, query, emailHash)
	if err != nil {
		return false, errors.Wrap(err, "failed to check user existence")
	}

	return exists, nil
}

func (r *UserRepository) Update(ctx context.Context, user *domain.User) error {
	// Encrypt fields
	encEmail, err := r.crypto.Encrypt(user.Email)
	if err != nil {
		return err
	}
	encPhone, err := r.crypto.Encrypt(user.Phone)
	if err != nil {
		return err
	}
	encFirstName, err := r.crypto.Encrypt(user.FirstName)
	if err != nil {
		return err
	}
	encLastName, err := r.crypto.Encrypt(user.LastName)
	if err != nil {
		return err
	}
	var encTOTPSecret *string
	if user.TOTPSecret != nil {
		enc, err := r.crypto.Encrypt(*user.TOTPSecret)
		if err != nil {
			return err
		}
		encTOTPSecret = &enc
	}

	// Hashes
	emailHash := r.crypto.BlindIndex(user.Email)
	var phoneHash *string
	if user.Phone != "" {
		ph := r.crypto.BlindIndex(user.Phone)
		phoneHash = &ph
	}

	query := `
		UPDATE customer_schema.users SET
			email = $1, phone = $2, first_name = $3, last_name = $4,
			user_type = $5, kyc_level = $6, kyc_status = $7, last_login = $8,
			password_hash = $9, failed_login_attempts = $10, locked_until = $11,
			updated_at = $12, email_hash = $13, phone_hash = $14,
			totp_secret = $15, is_totp_enabled = $16,
			bio = $17, city = $18, postal_code = $19, tax_id = $20
		WHERE id = $21
	`

	_, err = r.db.ExecContext(ctx, query,
		encEmail, encPhone, encFirstName, encLastName,
		user.UserType, user.KYCLevel, user.KYCStatus, user.LastLogin,
		user.PasswordHash, user.FailedLoginAttempts, user.LockedUntil,
		user.UpdatedAt, emailHash, phoneHash, encTOTPSecret, user.IsTOTPEnabled,
		user.Bio, user.City, user.PostalCode, user.TaxID,
		user.ID,
	)

	return errors.Wrap(err, "failed to update user")
}

func (r *UserRepository) SetEmailVerified(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE customer_schema.users SET
			email_verified = TRUE,
			updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id)
	return errors.Wrap(err, "failed to set email verified")
}

func (r *UserRepository) UpdateLoginSecurity(ctx context.Context, id uuid.UUID, attempts int, lockedUntil *time.Time) error {
	query := `
		UPDATE customer_schema.users SET
			failed_login_attempts = $1,
			locked_until = $2,
			updated_at = NOW()
		WHERE id = $3
	`
	_, err := r.db.ExecContext(ctx, query, attempts, lockedUntil, id)
	return errors.Wrap(err, "failed to update login security")
}

func (r *UserRepository) FindAll(ctx context.Context, limit, offset int, userType string) ([]*domain.User, error) {
	var users []*domain.User
	query := `
		SELECT 
			id, email, phone, first_name, last_name, user_type, kyc_level, kyc_status,
			country_code, date_of_birth, business_name, risk_score, is_active,
			failed_login_attempts, locked_until, last_login, created_at, updated_at,
			bio, city, postal_code, tax_id
		FROM customer_schema.users
	`
	args := []interface{}{}
	if userType != "" {
		query += ` WHERE user_type = $1`
		args = append(args, userType)
	}
	query += ` ORDER BY created_at DESC LIMIT $` + fmt.Sprint(len(args)+1) + ` OFFSET $` + fmt.Sprint(len(args)+2)
	args = append(args, limit, offset)

	err := r.db.SelectContext(ctx, &users, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find all users")
	}

	for _, user := range users {
		if err := r.decryptUser(user); err != nil {
			return nil, err
		}
	}
	return users, nil
}

func (r *UserRepository) CountAll(ctx context.Context, userType string) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM customer_schema.users`
	args := []interface{}{}
	if userType != "" {
		query += ` WHERE user_type = $1`
		args = append(args, userType)
	}
	err := r.db.GetContext(ctx, &count, query, args...)
	return count, errors.Wrap(err, "failed to count users")
}

func (r *UserRepository) FindAllByKYCStatus(ctx context.Context, status string, limit, offset int) ([]*domain.User, error) {
	var users []*domain.User
	query := `
		SELECT 
			id, email, phone, first_name, last_name, user_type, kyc_level, kyc_status,
			country_code, date_of_birth, business_name, risk_score, is_active,
			failed_login_attempts, locked_until, last_login, created_at, updated_at,
			bio, city, postal_code, tax_id
		FROM customer_schema.users
		WHERE kyc_status = $1
		ORDER BY updated_at DESC
		LIMIT $2 OFFSET $3
	`
	err := r.db.SelectContext(ctx, &users, query, status, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find users by kyc status")
	}
	for _, user := range users {
		if err := r.decryptUser(user); err != nil {
			return nil, err
		}
	}
	return users, nil
}

func (r *UserRepository) CountAllByKYCStatus(ctx context.Context, status string) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM customer_schema.users WHERE kyc_status = $1`
	err := r.db.GetContext(ctx, &count, query, status)
	return count, errors.Wrap(err, "failed to count users by kyc status")
}

func (r *UserRepository) UpdateKYCStatus(ctx context.Context, userID uuid.UUID, status domain.KYCStatus) error {
	query := `
		UPDATE customer_schema.users SET
			kyc_status = $1,
			updated_at = NOW()
		WHERE id = $2
	`
	_, err := r.db.ExecContext(ctx, query, status, userID)
	return errors.Wrap(err, "failed to update kyc status")
}
