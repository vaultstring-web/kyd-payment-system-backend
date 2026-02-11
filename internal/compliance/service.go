package compliance

import (
	"context"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/google/uuid"
)

type UserProvider interface {
	UpdateKYCStatus(ctx context.Context, userID uuid.UUID, status domain.KYCStatus) error
	FindAllByKYCStatus(ctx context.Context, status string, limit, offset int) ([]*domain.User, error)
	CountAllByKYCStatus(ctx context.Context, status string) (int, error)
	FindAll(ctx context.Context, limit, offset int, userType string) ([]*domain.User, error)
	CountAll(ctx context.Context, userType string) (int, error)
}

type Repository interface {
	Create(ctx context.Context, doc *domain.KYCDocument) error
	GetByUserID(ctx context.Context, userID uuid.UUID) ([]domain.KYCDocument, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.KYCDocument, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, notes *string, verifiedBy *uuid.UUID) error
}

type Service struct {
	repo         Repository
	userProvider UserProvider
}

func NewService(repo Repository, userProvider UserProvider) *Service {
	return &Service{
		repo:         repo,
		userProvider: userProvider,
	}
}

type SubmitKYCRequest struct {
	UserID         uuid.UUID
	DocumentType   string
	DocumentNumber string
	IssuingCountry string
	FrontImageURL  string
	BackImageURL   string
	SelfieImageURL string
}

func (s *Service) SubmitKYC(ctx context.Context, req *SubmitKYCRequest) (*domain.KYCDocument, error) {
	doc := &domain.KYCDocument{
		ID:                 uuid.New(),
		UserID:             req.UserID,
		DocumentType:       req.DocumentType,
		DocumentNumber:     &req.DocumentNumber,
		IssuingCountry:     &req.IssuingCountry,
		FrontImageURL:      &req.FrontImageURL,
		BackImageURL:       &req.BackImageURL,
		SelfieImageURL:     &req.SelfieImageURL,
		VerificationStatus: string(domain.KYCStatusPending),
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := s.repo.Create(ctx, doc); err != nil {
		return nil, err
	}

	// Update user status
	if err := s.userProvider.UpdateKYCStatus(ctx, req.UserID, domain.KYCStatusPending); err != nil {
		// Log error but don't fail the request? Or rollback?
		// ideally rollback, but for now we return error.
		// Note: repo.Create was already committed if not in transaction.
		return nil, errors.Wrap(err, "failed to update user kyc status")
	}

	return doc, nil
}

func (s *Service) GetKYCStatus(ctx context.Context, userID uuid.UUID) ([]domain.KYCDocument, error) {
	return s.repo.GetByUserID(ctx, userID)
}

type KYCApplication struct {
	ID          uuid.UUID   `json:"id"`
	UserID      uuid.UUID   `json:"user_id"`
	Status      string      `json:"status"`
	SubmittedAt time.Time   `json:"submitted_at"`
	ReviewedAt  *time.Time  `json:"reviewed_at,omitempty"`
	ReviewerID  *uuid.UUID  `json:"reviewer_id,omitempty"`
	Documents   interface{} `json:"documents,omitempty"`
	Name        string      `json:"name"`
	Email       string      `json:"email"`
}

func (s *Service) ListApplications(ctx context.Context, status string, limit, offset int) ([]KYCApplication, int, error) {
	var users []*domain.User
	var total int
	var err error

	if status != "" {
		users, err = s.userProvider.FindAllByKYCStatus(ctx, status, limit, offset)
		if err == nil {
			total, err = s.userProvider.CountAllByKYCStatus(ctx, status)
		}
	} else {
		users, err = s.userProvider.FindAll(ctx, limit, offset, "")
		if err == nil {
			total, err = s.userProvider.CountAll(ctx, "")
		}
	}

	if err != nil {
		return nil, 0, err
	}

	apps := make([]KYCApplication, len(users))
	for i, u := range users {
		submitted := u.UpdatedAt
		if submitted.IsZero() {
			submitted = u.CreatedAt
		}

		// Fetch documents for this user to include in the application details
		docs, _ := s.repo.GetByUserID(ctx, u.ID)

		apps[i] = KYCApplication{
			ID:          u.ID, // Using UserID as ApplicationID
			UserID:      u.ID,
			Status:      string(u.KYCStatus),
			SubmittedAt: submitted,
			Name:        u.FirstName + " " + u.LastName,
			Email:       u.Email,
			Documents:   docs,
		}
	}

	return apps, total, nil
}

func (s *Service) ReviewApplication(ctx context.Context, userID uuid.UUID, status string, reason string, reviewerID uuid.UUID) error {
	// Validate status
	switch status {
	case string(domain.KYCStatusVerified), string(domain.KYCStatusRejected):
		// ok
	default:
		return errors.New("invalid kyc status")
	}

	// Update User Status
	if err := s.userProvider.UpdateKYCStatus(ctx, userID, domain.KYCStatus(status)); err != nil {
		return err
	}

	// Update associated documents status to match application status
	docs, err := s.repo.GetByUserID(ctx, userID)
	if err == nil {
		for _, doc := range docs {
			// Update document status if it's currently pending or if we are forcing an update
			// We update all documents to match the application decision
			_ = s.repo.UpdateStatus(ctx, doc.ID, status, &reason, &reviewerID)
		}
	} else {
		// Log error but don't fail the operation since user status is updated
		// In a real system we might want better error handling/logging here
	}

	// TODO: Log audit trail or create a review record (out of scope for now, handled by controller audit log)

	return nil
}

func (s *Service) ReviewKYC(ctx context.Context, docID uuid.UUID, status string, notes string, reviewerID uuid.UUID) error {
	// Validate status
	switch status {
	case string(domain.KYCStatusVerified), string(domain.KYCStatusRejected):
		// ok
	default:
		return errors.New("invalid kyc status")
	}

	return s.repo.UpdateStatus(ctx, docID, status, &notes, &reviewerID)
}
