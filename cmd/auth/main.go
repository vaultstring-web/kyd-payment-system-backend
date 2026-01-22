// ==============================================================================
// SERVICE MAIN ENTRY POINTS - cmd/
// ==============================================================================

// AUTH SERVICE - cmd/auth/main.go
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"

	"kyd/internal/aml"
	"kyd/internal/auth"
	"kyd/internal/domain"
	"kyd/internal/fileupload"
	"kyd/internal/handler"
	"kyd/internal/kyc"
	"kyd/internal/middleware"
	"kyd/internal/repository/postgres"
	"kyd/internal/virusscan"
	"kyd/pkg/config"
	"kyd/pkg/logger"
	"kyd/pkg/mailer"
	"kyd/pkg/validator"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Validate required configuration
	// Initialize logger
	log := logger.New("auth-service")

	if err := cfg.ValidateCore(); err != nil {
		log.Fatal("Invalid configuration", map[string]interface{}{"error": err.Error()})
	}

	// Connect to database
	db, err := sqlx.Connect("postgres", cfg.Database.URL)
	if err != nil {
		log.Fatal("Failed to connect to database", map[string]interface{}{"error": err.Error()})
	}
	defer db.Close()

	db.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	// Connect to Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.URL,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatal("Failed to connect to Redis", map[string]interface{}{"error": err.Error()})
	}

	// ==============================================================================
	// REPOSITORIES
	// ==============================================================================

	// Initialize repositories
	userRepo := postgres.NewUserRepository(db)

	// Create KYC repositories
	kycProfileRepo := postgres.NewKYCProfileRepository(db)
	kycRequirementsRepo := postgres.NewKYCRequirementsRepository(db)
	kycDocumentRepo := postgres.NewKYCDocumentRepository(db)

	// Create composite KYC repository
	kycRepo := postgres.NewKYCRepositoryComposite(db)

	// Create adapter for KYC repository to implement kyc.Repository interface
	kycRepoAdapter := &kycRepositoryAdapter{
		composite: kycRepo,
		logger:    log,
	}

	// ==============================================================================
	// SERVICES
	// ==============================================================================

	// Initialize services
	authService := auth.NewService(userRepo, cfg.JWT.Secret, cfg.JWT.Expiration)

	// Configure email verification
	m := mailer.New(mailer.Config{
		Host:     cfg.Email.SMTPHost,
		Port:     cfg.Email.SMTPPort,
		Username: cfg.Email.SMTPUsername,
		Password: cfg.Email.SMTPPassword,
		From:     cfg.Email.SMTPFrom,
		UseTLS:   cfg.Email.SMTPUseTLS,
	})
	authService = authService.WithEmailVerification(m, cfg.Verification.BaseURL, cfg.Verification.TokenExpiration)

	// Initialize KYC services
	// File upload service (local storage)
	fileUploadConfig := &fileupload.LocalStorageConfig{
		BasePath:          "./uploads",
		AllowedExtensions: []string{".jpg", ".jpeg", ".png", ".pdf", ".doc", ".docx"},
		MaxFileSize:       10 * 1024 * 1024, // 10MB
		CreateDirectories: true,
		FilePermissions:   0644,
		DirPermissions:    0755,
	}

	// Create file metadata repository
	fileMetadataRepo := &simpleFileMetadataRepository{
		documentRepo: kycDocumentRepo,
		logger:       log,
	}

	storageProvider := fileupload.NewLocalStorageProvider(fileUploadConfig, log)
	fileUploadService := fileupload.NewFileUploadService(storageProvider, fileMetadataRepo, log, fileUploadConfig)

	// Virus scan service (mock)
	virusScanService := virusscan.NewMockVirusScanService(cfg, log)

	// AML service (mock)
	amlService := aml.NewMockAMLService(cfg, log)

	// Create KYC user repository adapter
	kycUserRepo := &kycUserRepositoryAdapter{
		authUserRepo:     userRepo,
		profileRepo:      kycProfileRepo,
		requirementsRepo: kycRequirementsRepo,
		documentRepo:     kycDocumentRepo,
		logger:           log,
	}

	// KYC service
	kycService := kyc.NewKYCService(
		kycRepoAdapter,    // KYC Repository adapter
		kycUserRepo,       // User Repository adapter
		fileUploadService, // File upload service
		virusScanService,  // Virus scan service
		amlService,        // AML service
		cfg,               // Configuration
		log,               // Logger
	)

	// ==============================================================================
	// HANDLERS
	// ==============================================================================

	// Initialize handlers
	val := validator.New()
	authHandler := handler.NewAuthHandler(authService, val, log)
	kycHandler := handler.NewKYCHandler(kycService, val, log)

	// ==============================================================================
	// ROUTER SETUP
	// ==============================================================================

	// Setup router
	r := mux.NewRouter()

	// Middleware
	r.Use(middleware.CORS)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.NewLoggingMiddleware(log).Log)
	r.Use(middleware.NewRateLimiter(redisClient, 60, time.Minute).Limit)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.Recovery)
	r.Use(middleware.BodyLimit(1 << 20)) // 1MB limit for auth endpoints

	// Routes
	r.HandleFunc("/health", healthCheck).Methods("GET")

	// Auth routes (public)
	r.HandleFunc("/api/v1/auth/register", authHandler.Register).Methods("POST")
	r.HandleFunc("/api/v1/auth/login", authHandler.Login).Methods("POST")
	r.HandleFunc("/api/v1/auth/send-verification", authHandler.SendVerification).Methods("POST")
	r.HandleFunc("/api/v1/auth/verify", authHandler.VerifyEmail).Methods("GET")

	// Protected routes (require JWT)
	authMW := middleware.NewAuthMiddleware(cfg.JWT.Secret)
	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(authMW.Authenticate)

	// Auth protected routes
	api.HandleFunc("/auth/me", authHandler.Me).Methods("GET")

	// ==============================================================================
	// KYC ROUTES (Protected)
	// ==============================================================================

	// Increase body limit for KYC endpoints (they have larger payloads)
	kycApi := api.PathPrefix("").Subrouter()
	kycApi.Use(middleware.BodyLimit(2 << 20)) // 2MB for KYC endpoints

	// KYC Profile endpoints
	kycApi.HandleFunc("/kyc/submit", kycHandler.SubmitKYC).Methods("POST")
	kycApi.HandleFunc("/kyc/status", kycHandler.GetKYCStatus).Methods("GET")
	kycApi.HandleFunc("/kyc/documents", kycHandler.UploadDocument).Methods("POST")
	kycApi.HandleFunc("/kyc/requirements", kycHandler.GetRequirements).Methods("GET")

	// ==============================================================================
	// SERVER SETUP
	// ==============================================================================

	// Start server
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Graceful shutdown
	go func() {
		log.Info("Auth + KYC service starting", map[string]interface{}{
			"port":    cfg.Server.Port,
			"host":    cfg.Server.Host,
			"service": "auth+kyc",
		})
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Server failed", map[string]interface{}{"error": err.Error()})
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown", map[string]interface{}{"error": err.Error()})
	}

	log.Info("Server stopped", nil)
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	_ = r
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy","service":"auth+kyc","version":"1.0.0"}`))
}

// ==============================================================================
// ADAPTERS AND WRAPPERS
// ==============================================================================

// kycTransactionAdapter adapts postgres.SQLxTransaction to kyc.Transaction interface
type kycTransactionAdapter struct {
	sqlxTx *postgres.SQLxTransaction
}

func newKYCTransactionAdapter(sqlxTx *postgres.SQLxTransaction) *kycTransactionAdapter {
	return &kycTransactionAdapter{sqlxTx: sqlxTx}
}

func (a *kycTransactionAdapter) Commit() error {
	return a.sqlxTx.Commit()
}

func (a *kycTransactionAdapter) Rollback() error {
	return a.sqlxTx.Rollback()
}

func (a *kycTransactionAdapter) GetID() string {
	return a.sqlxTx.GetID()
}

// GetInternalTx exposes the internal transaction for repository access
func (a *kycTransactionAdapter) GetInternalTx() *postgres.SQLxTransaction {
	return a.sqlxTx
}

// kycRepositoryAdapter adapts postgres.KYCRepositoryComposite to kyc.Repository interface
type kycRepositoryAdapter struct {
	composite *postgres.KYCRepositoryComposite
	logger    logger.Logger
}

// Implement all kyc.Repository interface methods by delegating to composite
// This handles the interface mismatch between postgres.SQLxTransaction and kyc.Transaction

func (a *kycRepositoryAdapter) CreateProfile(ctx context.Context, profile *domain.KYCProfile) error {
	return a.composite.CreateProfile(ctx, profile)
}

func (a *kycRepositoryAdapter) UpdateProfile(ctx context.Context, profile *domain.KYCProfile) error {
	return a.composite.UpdateProfile(ctx, profile)
}

func (a *kycRepositoryAdapter) BeginTransaction(ctx context.Context) (kyc.Transaction, error) {
	sqlxTx, err := a.composite.BeginTransaction(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	return newKYCTransactionAdapter(sqlxTx), nil
}

func (a *kycRepositoryAdapter) CreateProfileTx(ctx context.Context, tx kyc.Transaction, profile *domain.KYCProfile) error {
	adapter, ok := tx.(*kycTransactionAdapter)
	if !ok {
		return fmt.Errorf("invalid transaction type")
	}
	return a.composite.CreateProfileTx(ctx, adapter.GetInternalTx(), profile)
}

func (a *kycRepositoryAdapter) UpdateProfileTx(ctx context.Context, tx kyc.Transaction, profile *domain.KYCProfile) error {
	adapter, ok := tx.(*kycTransactionAdapter)
	if !ok {
		return fmt.Errorf("invalid transaction type")
	}
	return a.composite.UpdateProfileTx(ctx, adapter.GetInternalTx(), profile)
}

func (a *kycRepositoryAdapter) UpdateSubmissionStatusTx(ctx context.Context, tx kyc.Transaction, id uuid.UUID, status domain.KYCSubmissionStatus, notes string) error {
	adapter, ok := tx.(*kycTransactionAdapter)
	if !ok {
		return fmt.Errorf("invalid transaction type")
	}
	return a.composite.UpdateSubmissionStatusTx(ctx, adapter.GetInternalTx(), id, status, notes)
}

func (a *kycRepositoryAdapter) UpdateAMLStatusTx(ctx context.Context, tx kyc.Transaction, id uuid.UUID, status domain.AMLStatus, riskScore decimal.Decimal, pepCheck, sanctionCheck bool) error {
	adapter, ok := tx.(*kycTransactionAdapter)
	if !ok {
		return fmt.Errorf("invalid transaction type")
	}
	return a.composite.UpdateAMLStatusTx(ctx, adapter.GetInternalTx(), id, status, riskScore, pepCheck, sanctionCheck)
}

func (a *kycRepositoryAdapter) UpdateKYCLevelTx(ctx context.Context, tx kyc.Transaction, id uuid.UUID, level int) error {
	adapter, ok := tx.(*kycTransactionAdapter)
	if !ok {
		return fmt.Errorf("invalid transaction type")
	}
	return a.composite.UpdateKYCLevelTx(ctx, adapter.GetInternalTx(), id, level)
}

func (a *kycRepositoryAdapter) FindProfileByIDTx(ctx context.Context, tx kyc.Transaction, id uuid.UUID) (*domain.KYCProfile, error) {
	adapter, ok := tx.(*kycTransactionAdapter)
	if !ok {
		return nil, fmt.Errorf("invalid transaction type")
	}
	return a.composite.FindProfileByIDTx(ctx, adapter.GetInternalTx(), id)
}

func (a *kycRepositoryAdapter) FindProfileByUserIDTx(ctx context.Context, tx kyc.Transaction, userID uuid.UUID) (*domain.KYCProfile, error) {
	adapter, ok := tx.(*kycTransactionAdapter)
	if !ok {
		return nil, fmt.Errorf("invalid transaction type")
	}
	return a.composite.FindProfileByUserIDTx(ctx, adapter.GetInternalTx(), userID)
}

func (a *kycRepositoryAdapter) ExistsByUserIDTx(ctx context.Context, tx kyc.Transaction, userID uuid.UUID) (bool, error) {
	adapter, ok := tx.(*kycTransactionAdapter)
	if !ok {
		return false, fmt.Errorf("invalid transaction type")
	}
	return a.composite.ExistsByUserIDTx(ctx, adapter.GetInternalTx(), userID)
}

func (a *kycRepositoryAdapter) FindProfileByID(ctx context.Context, id uuid.UUID) (*domain.KYCProfile, error) {
	return a.composite.FindProfileByIDTx(ctx, nil, id)
}

func (a *kycRepositoryAdapter) FindProfileByUserID(ctx context.Context, userID uuid.UUID) (*domain.KYCProfile, error) {
	return a.composite.FindProfileByUserIDTx(ctx, nil, userID)
}

func (a *kycRepositoryAdapter) ExistsByUserID(ctx context.Context, userID uuid.UUID) (bool, error) {
	return a.composite.ExistsByUserIDTx(ctx, nil, userID)
}

func (a *kycRepositoryAdapter) UpdateSubmissionStatus(ctx context.Context, id uuid.UUID, status domain.KYCSubmissionStatus, notes string) error {
	return a.composite.UpdateSubmissionStatusTx(ctx, nil, id, status, notes)
}

func (a *kycRepositoryAdapter) UpdateAMLStatus(ctx context.Context, id uuid.UUID, status domain.AMLStatus, riskScore decimal.Decimal, pepCheck, sanctionCheck bool) error {
	return a.composite.UpdateAMLStatusTx(ctx, nil, id, status, riskScore, pepCheck, sanctionCheck)
}

func (a *kycRepositoryAdapter) UpdateKYCLevel(ctx context.Context, id uuid.UUID, level int) error {
	return a.composite.UpdateKYCLevelTx(ctx, nil, id, level)
}

func (a *kycRepositoryAdapter) FindPendingReview(ctx context.Context, limit, offset int) ([]*domain.KYCProfile, error) {
	return a.composite.FindPendingReview(ctx, limit, offset)
}

func (a *kycRepositoryAdapter) CountPendingReview(ctx context.Context) (int, error) {
	return a.composite.CountPendingReview(ctx)
}

func (a *kycRepositoryAdapter) FindByStatus(ctx context.Context, status domain.KYCSubmissionStatus, limit, offset int) ([]*domain.KYCProfile, error) {
	return a.composite.FindByStatus(ctx, status, limit, offset)
}

// Requirements operations
func (a *kycRepositoryAdapter) FindRequirementsByCountryAndUserType(ctx context.Context, countryCode, userType string, kycLevel int) (*domain.KYCRequirements, error) {
	return a.composite.FindRequirementsByCountryAndUserType(ctx, countryCode, userType, kycLevel)
}

func (a *kycRepositoryAdapter) FindAllActiveRequirements(ctx context.Context, limit, offset int) ([]*domain.KYCRequirements, error) {
	return a.composite.FindAllActiveRequirements(ctx, limit, offset)
}

// Document operations
func (a *kycRepositoryAdapter) CreateDocument(ctx context.Context, document *domain.KYCDocument) error {
	return a.composite.CreateDocument(ctx, document)
}

func (a *kycRepositoryAdapter) UpdateDocument(ctx context.Context, document *domain.KYCDocument) error {
	return a.composite.UpdateDocument(ctx, document)
}

func (a *kycRepositoryAdapter) FindDocumentByID(ctx context.Context, id uuid.UUID) (*domain.KYCDocument, error) {
	return a.composite.FindDocumentByID(ctx, id)
}

func (a *kycRepositoryAdapter) FindDocumentsByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.KYCDocument, error) {
	return a.composite.FindDocumentsByUserID(ctx, userID)
}

func (a *kycRepositoryAdapter) FindDocumentsByUserIDAndType(ctx context.Context, userID uuid.UUID, docType domain.DocumentType) ([]*domain.KYCDocument, error) {
	return a.composite.FindDocumentsByUserIDAndType(ctx, userID, docType)
}

func (a *kycRepositoryAdapter) UpdateDocumentVerificationStatus(ctx context.Context, id uuid.UUID, status domain.DocumentVerificationStatus, notes string, verifiedBy uuid.UUID) error {
	return a.composite.UpdateDocumentVerificationStatus(ctx, id, status, notes, verifiedBy)
}

func (a *kycRepositoryAdapter) UpdateDocumentVirusScanStatus(ctx context.Context, id uuid.UUID, status domain.VirusScanStatus, resultStr, engine, version string) error {
	return a.composite.UpdateDocumentVirusScanStatus(ctx, id, status, resultStr, engine, version)
}

func (a *kycRepositoryAdapter) FindPendingVirusScan(ctx context.Context, limit int) ([]*domain.KYCDocument, error) {
	return a.composite.FindPendingVirusScan(ctx, limit)
}

func (a *kycRepositoryAdapter) FindPendingVerification(ctx context.Context, limit, offset int) ([]*domain.KYCDocument, error) {
	return a.composite.FindPendingVerification(ctx, limit, offset)
}

func (a *kycRepositoryAdapter) CountDocumentsByUserIDAndStatus(ctx context.Context, userID uuid.UUID, status domain.DocumentVerificationStatus) (int, error) {
	return a.composite.CountDocumentsByUserIDAndStatus(ctx, userID, status)
}

func (a *kycRepositoryAdapter) DeleteDocument(ctx context.Context, id uuid.UUID) error {
	return a.composite.DeleteDocument(ctx, id)
}

// kycUserRepositoryAdapter implements kyc.UserRepository interface
// by adapting existing PostgreSQL repositories
type kycUserRepositoryAdapter struct {
	authUserRepo     *postgres.UserRepository
	profileRepo      *postgres.KYCProfileRepository
	requirementsRepo *postgres.KYCRequirementsRepository
	documentRepo     *postgres.KYCDocumentRepository
	logger           logger.Logger
}

// FindByID finds user by ID
func (a *kycUserRepositoryAdapter) FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return a.authUserRepo.FindByID(ctx, id)
}

// UpdateKYCStatus updates user's KYC status
func (a *kycUserRepositoryAdapter) UpdateKYCStatus(ctx context.Context, userID uuid.UUID, status domain.KYCStatus, profileStatus *domain.KYCSubmissionStatus) error {
	user, err := a.authUserRepo.FindByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to find user: %w", err)
	}

	user.KYCStatus = status
	user.UpdatedAt = time.Now()

	return a.authUserRepo.Update(ctx, user)
}

// UpdateKYCLevel updates user's KYC level
func (a *kycUserRepositoryAdapter) UpdateKYCLevel(ctx context.Context, userID uuid.UUID, level int, updateProfile bool) error {
	user, err := a.authUserRepo.FindByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to find user: %w", err)
	}

	user.KYCLevel = level
	user.UpdatedAt = time.Now()

	err = a.authUserRepo.Update(ctx, user)
	if err != nil {
		return err
	}

	// Update profile if requested
	if updateProfile {
		profile, err := a.profileRepo.FindProfileByUserIDTx(ctx, nil, userID)
		if err != nil {
			a.logger.Warn("Failed to find profile for KYC level update", map[string]interface{}{
				"user_id": userID,
				"error":   err.Error(),
			})
			// Don't fail if profile not found
			return nil
		}

		profile.KYCLevel = level
		profile.UpdatedAt = time.Now()
		return a.profileRepo.UpdateProfileTx(ctx, nil, profile)
	}

	return nil
}

// UpdateUserRiskScore updates user's risk score
func (a *kycUserRepositoryAdapter) UpdateUserRiskScore(ctx context.Context, userID uuid.UUID, riskScore decimal.Decimal) error {
	user, err := a.authUserRepo.FindByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to find user: %w", err)
	}

	user.RiskScore = riskScore
	user.UpdatedAt = time.Now()

	return a.authUserRepo.Update(ctx, user)
}

// GetUserKYCProfile gets user's KYC profile
func (a *kycUserRepositoryAdapter) GetUserKYCProfile(ctx context.Context, userID uuid.UUID) (*domain.KYCProfile, error) {
	return a.profileRepo.FindProfileByUserIDTx(ctx, nil, userID)
}

// GetUserKYCRequirements gets user's KYC requirements
func (a *kycUserRepositoryAdapter) GetUserKYCRequirements(ctx context.Context, userID uuid.UUID) (*domain.KYCRequirements, error) {
	user, err := a.authUserRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	return a.requirementsRepo.FindByCountryAndUserType(ctx, user.CountryCode, string(user.UserType), user.KYCLevel)
}

// UpdateUserWithKYCProfile updates user with KYC profile information
func (a *kycUserRepositoryAdapter) UpdateUserWithKYCProfile(ctx context.Context, user *domain.User, profile *domain.KYCProfile) error {
	// Update user with profile data
	user.KYCLevel = profile.KYCLevel
	// Convert KYCSubmissionStatus to KYCStatus - need to handle the mapping
	// For now, we'll use a simple conversion
	user.KYCStatus = mapKYCSubmissionStatusToKYCStatus(profile.SubmissionStatus)
	user.UpdatedAt = time.Now()

	return a.authUserRepo.Update(ctx, user)
}

// UserHasKYCProfile checks if user has KYC profile
func (a *kycUserRepositoryAdapter) UserHasKYCProfile(ctx context.Context, userID uuid.UUID) (bool, error) {
	return a.profileRepo.ExistsByUserIDTx(ctx, nil, userID)
}

// GetUserKYCStatus gets user's KYC status
func (a *kycUserRepositoryAdapter) GetUserKYCStatus(ctx context.Context, userID uuid.UUID) (*domain.KYCStatusResponse, error) {
	user, err := a.authUserRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	profile, err := a.profileRepo.FindProfileByUserIDTx(ctx, nil, userID)
	if err != nil {
		// If profile not found, return basic user info
		// Map user.KYCStatus (domain.KYCStatus) to domain.KYCSubmissionStatus
		profileStatus := domain.KYCSubmissionStatusDraft // Default to draft
		if user.KYCStatus == domain.KYCStatusApproved {
			profileStatus = domain.KYCSubmissionStatusApproved
		} else if user.KYCStatus == domain.KYCStatusRejected {
			profileStatus = domain.KYCSubmissionStatusRejected
		} else if user.KYCStatus == domain.KYCStatusPending || user.KYCStatus == domain.KYCStatusUnderReview {
			profileStatus = domain.KYCSubmissionStatusSubmitted
		}

		return &domain.KYCStatusResponse{
			UserID:    userID,
			KYCLevel:  user.KYCLevel,
			KYCStatus: profileStatus, // Use the converted profile status
		}, nil
	}

	// If profile found, use its SubmissionStatus
	return &domain.KYCStatusResponse{
		UserID:         userID,
		KYCLevel:       user.KYCLevel,
		KYCStatus:      profile.SubmissionStatus, // This is already domain.KYCSubmissionStatus
		AMLStatus:      profile.AMLStatus,
		AMLRiskScore:   profile.AMLRiskScore,
		ProfileType:    profile.ProfileType,
		SubmittedAt:    profile.SubmittedAt,
		ReviewedAt:     profile.ReviewedAt,
		NextReviewDate: profile.NextReviewDate,
	}, nil
}

// Transactional methods (these would need to be implemented if the kyc service requires them)
// For now, we'll implement simple non-transactional versions

func (a *kycUserRepositoryAdapter) UpdateKYCStatusTx(ctx context.Context, tx kyc.Transaction, userID uuid.UUID, status domain.KYCStatus, profileStatus *domain.KYCSubmissionStatus) error {
	// Since we can't access the transaction directly, we'll fall back to non-transactional version
	// In production, you would need to use the transaction to execute the update
	return a.UpdateKYCStatus(ctx, userID, status, profileStatus)
}

func (a *kycUserRepositoryAdapter) UpdateKYCLevelTx(ctx context.Context, tx kyc.Transaction, userID uuid.UUID, level int, updateProfile bool) error {
	return a.UpdateKYCLevel(ctx, userID, level, updateProfile)
}

func (a *kycUserRepositoryAdapter) UpdateUserRiskScoreTx(ctx context.Context, tx kyc.Transaction, userID uuid.UUID, riskScore decimal.Decimal) error {
	return a.UpdateUserRiskScore(ctx, userID, riskScore)
}

func (a *kycUserRepositoryAdapter) FindByIDTx(ctx context.Context, tx kyc.Transaction, id uuid.UUID) (*domain.User, error) {
	// For now, ignore transaction and use non-transactional version
	return a.FindByID(ctx, id)
}

// simpleFileMetadataRepository implements fileupload.FileMetadataRepository
// by adapting the KYCDocumentRepository
type simpleFileMetadataRepository struct {
	documentRepo *postgres.KYCDocumentRepository
	logger       logger.Logger
}

func (s *simpleFileMetadataRepository) SaveMetadata(ctx context.Context, metadata *fileupload.FileMetadata) error {
	// Convert fileupload.FileMetadata to domain.KYCDocument
	document := &domain.KYCDocument{
		ID:                 metadata.FileID,
		UserID:             metadata.UserID,
		DocumentType:       metadata.DocumentType,
		DocumentNumber:     "", // Would need to extract from metadata
		IssuingCountry:     "", // Would need to extract from metadata
		FileName:           metadata.FileName,
		FileSizeBytes:      &metadata.FileSize,
		MimeType:           metadata.ContentType,
		StorageProvider:    metadata.StorageProvider,
		StorageKey:         metadata.StoragePath,
		PublicURL:          "", // Would be set separately
		FileHashSHA256:     metadata.ChecksumSHA256,
		FileHashMD5:        metadata.ChecksumMD5,
		VirusScanStatus:    domain.VirusScanStatusPending,
		VerificationStatus: domain.DocumentStatusPending,
		RetentionPolicy:    metadata.RetentionPolicy,
		AccessLevel:        metadata.AccessLevel,
		CreatedAt:          metadata.CreatedAt,
		UpdatedAt:          metadata.UpdatedAt,
		Metadata:           metadata.Metadata,
	}

	return s.documentRepo.CreateDocument(ctx, nil, document)
}

func (s *simpleFileMetadataRepository) GetMetadata(ctx context.Context, fileID uuid.UUID) (*fileupload.FileMetadata, error) {
	document, err := s.documentRepo.FindByID(ctx, fileID)
	if err != nil {
		return nil, err
	}

	return s.documentToMetadata(document), nil
}

func (s *simpleFileMetadataRepository) DeleteMetadata(ctx context.Context, fileID uuid.UUID) error {
	return s.documentRepo.Delete(ctx, fileID)
}

func (s *simpleFileMetadataRepository) ListByUserID(ctx context.Context, userID uuid.UUID, docType *domain.DocumentType) ([]*fileupload.FileMetadata, error) {
	var documents []*domain.KYCDocument
	var err error

	if docType != nil {
		documents, err = s.documentRepo.FindByUserIDAndType(ctx, userID, *docType)
	} else {
		documents, err = s.documentRepo.FindByUserID(ctx, userID)
	}

	if err != nil {
		return nil, err
	}

	metadataList := make([]*fileupload.FileMetadata, len(documents))
	for i, doc := range documents {
		metadataList[i] = s.documentToMetadata(doc)
	}

	return metadataList, nil
}

func (s *simpleFileMetadataRepository) UpdateMetadata(ctx context.Context, fileID uuid.UUID, updates map[string]interface{}) error {
	document, err := s.documentRepo.FindByID(ctx, fileID)
	if err != nil {
		return err
	}

	// Update metadata field
	if document.Metadata == nil {
		document.Metadata = make(domain.Metadata)
	}

	for key, value := range updates {
		document.Metadata[key] = value
	}

	document.UpdatedAt = time.Now()

	return s.documentRepo.Update(ctx, document)
}

func (s *simpleFileMetadataRepository) IncrementDownloadCount(ctx context.Context, fileID uuid.UUID, downloadedAt time.Time) error {
	return s.documentRepo.IncrementDownloadCount(ctx, fileID, uuid.Nil) // uuid.Nil for system
}

func (s *simpleFileMetadataRepository) documentToMetadata(doc *domain.KYCDocument) *fileupload.FileMetadata {
	return &fileupload.FileMetadata{
		FileID:           doc.ID,
		UserID:           doc.UserID,
		DocumentType:     doc.DocumentType,
		FileName:         doc.FileName,
		StoragePath:      doc.StorageKey,
		StorageProvider:  doc.StorageProvider,
		FileSize:         *doc.FileSizeBytes,
		ContentType:      doc.MimeType,
		ChecksumSHA256:   doc.FileHashSHA256,
		ChecksumMD5:      doc.FileHashMD5,
		AccessLevel:      doc.AccessLevel,
		RetentionPolicy:  doc.RetentionPolicy,
		CreatedAt:        doc.CreatedAt,
		UpdatedAt:        doc.UpdatedAt,
		Downloads:        doc.DownloadCount,
		LastDownloadedAt: doc.LastDownloadedAt,
		Metadata:         doc.Metadata,
	}
}

// Helper function to map KYCSubmissionStatus to KYCStatus
func mapKYCSubmissionStatusToKYCStatus(submissionStatus domain.KYCSubmissionStatus) domain.KYCStatus {
	switch submissionStatus {
	case domain.KYCSubmissionStatusApproved:
		return domain.KYCStatusApproved
	case domain.KYCSubmissionStatusRejected:
		return domain.KYCStatusRejected
	case domain.KYCSubmissionStatusPending, domain.KYCSubmissionStatusSubmitted:
		return domain.KYCStatusPending
	case domain.KYCSubmissionStatusUnderReview:
		return domain.KYCStatusUnderReview
	case domain.KYCSubmissionStatusAdditionalInfoRequired:
		return domain.KYCStatusAdditionalInfoRequired
	case domain.KYCSubmissionStatusSuspended:
		return domain.KYCStatusSuspended
	case domain.KYCSubmissionStatusDraft:
		return domain.KYCStatusDraft
	default:
		return domain.KYCStatusPending
	}
}
