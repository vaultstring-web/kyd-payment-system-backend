package postgres

import (
	"context"
	"database/sql"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type KYCRepository struct {
	db *sqlx.DB
}

func NewKYCRepository(db *sqlx.DB) *KYCRepository {
	return &KYCRepository{db: db}
}

func (r *KYCRepository) Create(ctx context.Context, doc *domain.KYCDocument) error {
	query := `
		INSERT INTO customer_schema.kyc_documents (
			id, user_id, document_type, document_number, issuing_country,
			issue_date, expiry_date, front_image_url, back_image_url, selfie_image_url,
			verification_status, metadata, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)
	`

	_, err := r.db.ExecContext(ctx, query,
		doc.ID, doc.UserID, doc.DocumentType, doc.DocumentNumber, doc.IssuingCountry,
		doc.IssueDate, doc.ExpiryDate, doc.FrontImageURL, doc.BackImageURL, doc.SelfieImageURL,
		doc.VerificationStatus, doc.Metadata, doc.CreatedAt, doc.UpdatedAt,
	)

	if err != nil {
		return errors.Wrap(err, "failed to create kyc document")
	}

	return nil
}

func (r *KYCRepository) GetByUserID(ctx context.Context, userID uuid.UUID) ([]domain.KYCDocument, error) {
	query := `
		SELECT * FROM customer_schema.kyc_documents
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	var docs []domain.KYCDocument
	err := r.db.SelectContext(ctx, &docs, query, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get kyc documents")
	}

	return docs, nil
}

func (r *KYCRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.KYCDocument, error) {
	query := `
		SELECT * FROM customer_schema.kyc_documents
		WHERE id = $1
	`

	var doc domain.KYCDocument
	err := r.db.GetContext(ctx, &doc, query, id)
	if err == sql.ErrNoRows {
		return nil, errors.New("kyc document not found")
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get kyc document")
	}

	return &doc, nil
}

func (r *KYCRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string, notes *string, verifiedBy *uuid.UUID) error {
	query := `
		UPDATE customer_schema.kyc_documents
		SET verification_status = $1, verification_notes = $2, verified_by = $3, verified_at = $4, updated_at = $4
		WHERE id = $5
	`

	now := time.Now()
	_, err := r.db.ExecContext(ctx, query, status, notes, verifiedBy, now, id)
	if err != nil {
		return errors.Wrap(err, "failed to update kyc status")
	}

	return nil
}
