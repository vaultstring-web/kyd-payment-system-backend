// ==============================================================================
// DOMAIN MODELS - pkg/domain/models.go
// ==============================================================================
package domain

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Money represents a monetary amount with currency
type Money struct {
	Amount   decimal.Decimal `json:"amount"`
	Currency Currency        `json:"currency"`
}

// Currency represents ISO 4217 currency codes
type Currency string

const (
	MWK Currency = "MWK" // Malawi Kwacha
	CNY Currency = "CNY" // Chinese Yuan
	USD Currency = "USD" // US Dollar
	EUR Currency = "EUR" // Euro
)

// User represents a system user
type User struct {
	ID                   uuid.UUID       `json:"id" db:"id"`
	Email                string          `json:"email" db:"email"`
	Phone                string          `json:"phone" db:"phone"`
	PasswordHash         string          `json:"-" db:"password_hash"`
	FirstName            string          `json:"first_name" db:"first_name"`
	LastName             string          `json:"last_name" db:"last_name"`
	UserType             UserType        `json:"user_type" db:"user_type"`
	KYCLevel             int             `json:"kyc_level" db:"kyc_level"`
	KYCStatus            KYCStatus       `json:"kyc_status" db:"kyc_status"`
	CountryCode          string          `json:"country_code" db:"country_code"`
	DateOfBirth          *time.Time      `json:"date_of_birth,omitempty" db:"date_of_birth"`
	BusinessName         *string         `json:"business_name,omitempty" db:"business_name"`
	BusinessRegistration *string         `json:"business_registration,omitempty" db:"business_registration"`
	RiskScore            decimal.Decimal `json:"risk_score" db:"risk_score"`
	IsActive             bool            `json:"is_active" db:"is_active"`
	EmailVerified        bool            `json:"email_verified" db:"email_verified"`
	LastLogin            *time.Time      `json:"last_login,omitempty" db:"last_login"`
	FailedLoginAttempts  int             `json:"failed_login_attempts" db:"failed_login_attempts"`
	LockedUntil          *time.Time      `json:"locked_until,omitempty" db:"locked_until"`
	CreatedAt            time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at" db:"updated_at"`
}

type UserType string

const (
	UserTypeIndividual UserType = "individual"
	UserTypeMerchant   UserType = "merchant"
	UserTypeAgent      UserType = "agent"
	UserTypeAdmin      UserType = "admin"
)

type KYCStatus string

const (
	KYCStatusPending    KYCStatus = "pending"
	KYCStatusProcessing KYCStatus = "processing"
	KYCStatusVerified   KYCStatus = "verified"
	KYCStatusRejected   KYCStatus = "rejected"
)

// Wallet represents a user's currency wallet
type Wallet struct {
	ID                uuid.UUID       `json:"id" db:"id"`
	UserID            uuid.UUID       `json:"user_id" db:"user_id"`
	WalletAddress     *string         `json:"wallet_address,omitempty" db:"wallet_address"`
	Currency          Currency        `json:"currency" db:"currency"`
	AvailableBalance  decimal.Decimal `json:"available_balance" db:"available_balance"`
	LedgerBalance     decimal.Decimal `json:"ledger_balance" db:"ledger_balance"`
	ReservedBalance   decimal.Decimal `json:"reserved_balance" db:"reserved_balance"`
	Status            WalletStatus    `json:"status" db:"status"`
	LastTransactionAt *time.Time      `json:"last_transaction_at,omitempty" db:"last_transaction_at"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at" db:"updated_at"`
}

type WalletStatus string

const (
	WalletStatusActive    WalletStatus = "active"
	WalletStatusSuspended WalletStatus = "suspended"
	WalletStatusClosed    WalletStatus = "closed"
)

// Transaction represents a payment transaction
type Transaction struct {
	ID                uuid.UUID         `json:"id" db:"id"`
	Reference         string            `json:"reference" db:"reference"`
	SenderID          uuid.UUID         `json:"sender_id" db:"sender_id"`
	ReceiverID        uuid.UUID         `json:"receiver_id" db:"receiver_id"`
	SenderWalletID    uuid.UUID         `json:"sender_wallet_id" db:"sender_wallet_id"`
	ReceiverWalletID  uuid.UUID         `json:"receiver_wallet_id" db:"receiver_wallet_id"`
	Amount            decimal.Decimal   `json:"amount" db:"amount"`
	Currency          Currency          `json:"currency" db:"currency"`
	ExchangeRate      decimal.Decimal   `json:"exchange_rate" db:"exchange_rate"`
	ConvertedAmount   decimal.Decimal   `json:"converted_amount" db:"converted_amount"`
	ConvertedCurrency Currency          `json:"converted_currency" db:"converted_currency"`
	FeeAmount         decimal.Decimal   `json:"fee_amount" db:"fee_amount"`
	FeeCurrency       Currency          `json:"fee_currency" db:"fee_currency"`
	NetAmount         decimal.Decimal   `json:"net_amount" db:"net_amount"`
	Status            TransactionStatus `json:"status" db:"status"`
	StatusReason      *string           `json:"status_reason,omitempty" db:"status_reason"`
	TransactionType   TransactionType   `json:"transaction_type" db:"transaction_type"`
	Channel           *string           `json:"channel,omitempty" db:"channel"`
	Category          *string           `json:"category,omitempty" db:"category"`
	Description       *string           `json:"description,omitempty" db:"description"`
	Metadata          Metadata          `json:"metadata" db:"metadata"`
	BlockchainTxHash  *string           `json:"blockchain_tx_hash,omitempty" db:"blockchain_tx_hash"`
	SettlementID      *uuid.UUID        `json:"settlement_id,omitempty" db:"settlement_id"`
	InitiatedAt       time.Time         `json:"initiated_at" db:"initiated_at"`
	CompletedAt       *time.Time        `json:"completed_at,omitempty" db:"completed_at"`
	CreatedAt         time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at" db:"updated_at"`
}

type TransactionStatus string

const (
	TransactionStatusPending    TransactionStatus = "pending"
	TransactionStatusProcessing TransactionStatus = "processing"
	TransactionStatusReserved   TransactionStatus = "reserved"
	TransactionStatusSettling   TransactionStatus = "settling"
	TransactionStatusCompleted  TransactionStatus = "completed"
	TransactionStatusFailed     TransactionStatus = "failed"
	TransactionStatusCancelled  TransactionStatus = "cancelled"
	TransactionStatusRefunded   TransactionStatus = "refunded"
)

type TransactionType string

const (
	TransactionTypePayment    TransactionType = "payment"
	TransactionTypeTransfer   TransactionType = "transfer"
	TransactionTypeWithdrawal TransactionType = "withdrawal"
	TransactionTypeDeposit    TransactionType = "deposit"
	TransactionTypeRefund     TransactionType = "refund"
	TransactionTypeReversal   TransactionType = "reversal"
	TransactionTypeSettlement TransactionType = "settlement"
)

// Metadata is a JSON field type
type Metadata map[string]interface{}

func (m Metadata) Value() (driver.Value, error) {
	return json.Marshal(m)
}

func (m *Metadata) Scan(value interface{}) error {
	if value == nil {
		*m = make(Metadata)
		return nil
	}

	b, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}

	return json.Unmarshal(b, m)
}

// ExchangeRate represents currency exchange rates
type ExchangeRate struct {
	ID             uuid.UUID       `json:"id" db:"id"`
	BaseCurrency   Currency        `json:"base_currency" db:"base_currency"`
	TargetCurrency Currency        `json:"target_currency" db:"target_currency"`
	Rate           decimal.Decimal `json:"rate" db:"rate"`
	BuyRate        decimal.Decimal `json:"buy_rate" db:"buy_rate"`
	SellRate       decimal.Decimal `json:"sell_rate" db:"sell_rate"`
	Source         string          `json:"source" db:"source"`
	Provider       *string         `json:"provider,omitempty" db:"provider"`
	IsInterbank    bool            `json:"is_interbank" db:"is_interbank"`
	Spread         decimal.Decimal `json:"spread" db:"spread"`
	ValidFrom      time.Time       `json:"valid_from" db:"valid_from"`
	ValidTo        *time.Time      `json:"valid_to,omitempty" db:"valid_to"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
}

// Settlement represents batch settlement
type Settlement struct {
	ID                 uuid.UUID         `json:"id" db:"id"`
	BatchReference     string            `json:"batch_reference" db:"batch_reference"`
	Network            BlockchainNetwork `json:"network" db:"network"`
	TransactionHash    *string           `json:"transaction_hash,omitempty" db:"transaction_hash"`
	SourceAccount      *string           `json:"source_account,omitempty" db:"source_account"`
	DestinationAccount *string           `json:"destination_account,omitempty" db:"destination_account"`
	TotalAmount        decimal.Decimal   `json:"total_amount" db:"total_amount"`
	Currency           Currency          `json:"currency" db:"currency"`
	FeeAmount          decimal.Decimal   `json:"fee_amount" db:"fee_amount"`
	FeeCurrency        Currency          `json:"fee_currency" db:"fee_currency"`
	Status             SettlementStatus  `json:"status" db:"status"`
	SubmissionCount    int               `json:"submission_count" db:"submission_count"`
	LastSubmittedAt    *time.Time        `json:"last_submitted_at,omitempty" db:"last_submitted_at"`
	ConfirmedAt        *time.Time        `json:"confirmed_at,omitempty" db:"confirmed_at"`
	CompletedAt        *time.Time        `json:"completed_at,omitempty" db:"completed_at"`
	Metadata           Metadata          `json:"metadata" db:"metadata"`
	CreatedAt          time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at" db:"updated_at"`
}

type BlockchainNetwork string

const (
	NetworkStellar      BlockchainNetwork = "stellar"
	NetworkRipple       BlockchainNetwork = "ripple"
	NetworkBankTransfer BlockchainNetwork = "bank_transfer"
)

type SettlementStatus string

const (
	SettlementStatusPending    SettlementStatus = "pending"
	SettlementStatusProcessing SettlementStatus = "processing"
	SettlementStatusSubmitted  SettlementStatus = "submitted"
	SettlementStatusConfirmed  SettlementStatus = "confirmed"
	SettlementStatusCompleted  SettlementStatus = "completed"
	SettlementStatusFailed     SettlementStatus = "failed"
	SettlementStatusReconciled SettlementStatus = "reconciled"
)

// AuditLog represents a system audit log entry
type AuditLog struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	UserID      *uuid.UUID `json:"user_id,omitempty" db:"user_id"`
	Action      string     `json:"action" db:"action"`
	EntityType  *string    `json:"entity_type,omitempty" db:"entity_type"`
	EntityID    *uuid.UUID `json:"entity_id,omitempty" db:"entity_id"`
	OldValues   *Metadata  `json:"old_values,omitempty" db:"old_values"`
	NewValues   *Metadata  `json:"new_values,omitempty" db:"new_values"`
	IPAddress   *string    `json:"ip_address,omitempty" db:"ip_address"`
	UserAgent   *string    `json:"user_agent,omitempty" db:"user_agent"`
	RequestID   *string    `json:"request_id,omitempty" db:"request_id"`
	StatusCode  *int       `json:"status_code,omitempty" db:"status_code"`
	ErrorMessage *string   `json:"error_message,omitempty" db:"error_message"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
}