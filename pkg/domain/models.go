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
	ZMW Currency = "ZMW" // Zambian Kwacha
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
	TOTPSecret           *string         `json:"-" db:"totp_secret"`
	IsTOTPEnabled        bool            `json:"is_totp_enabled" db:"is_totp_enabled"`
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
	SenderWalletID    *uuid.UUID        `json:"sender_wallet_id" db:"sender_wallet_id"`
	ReceiverWalletID  *uuid.UUID        `json:"receiver_wallet_id" db:"receiver_wallet_id"`
	Amount            decimal.Decimal   `json:"amount" db:"amount"`
	Currency          Currency          `json:"currency" db:"currency"`
	ExchangeRate      decimal.Decimal   `json:"exchange_rate" db:"exchange_rate"`
	ConvertedAmount   decimal.Decimal   `json:"converted_amount" db:"converted_amount"`
	ConvertedCurrency Currency          `json:"converted_currency" db:"converted_currency"`
	FeeAmount         decimal.Decimal   `json:"fee_amount" db:"fee_amount"`
	FeeCurrency       Currency          `json:"fee_currency" db:"fee_currency"`
	NetAmount         decimal.Decimal   `json:"net_amount" db:"net_amount"`
	Status            TransactionStatus `json:"status" db:"status"`
	StatusReason      string            `json:"status_reason" db:"status_reason"`
	TransactionType   TransactionType   `json:"transaction_type" db:"transaction_type"`
	Channel           string            `json:"channel" db:"channel"`
	Category          string            `json:"category" db:"category"`
	Description       string            `json:"description" db:"description"`
	Metadata          Metadata          `json:"metadata" db:"metadata"`
	BlockchainTxHash  string            `json:"blockchain_tx_hash" db:"blockchain_tx_hash"`
	SettlementID      *uuid.UUID        `json:"settlement_id" db:"settlement_id"`
	InitiatedAt       time.Time         `json:"initiated_at" db:"initiated_at"`
	CompletedAt       *time.Time        `json:"completed_at" db:"completed_at"`
	CreatedAt         time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at" db:"updated_at"`
}

type TransactionStatus string

const (
	TransactionStatusPending            TransactionStatus = "pending"
	TransactionStatusPendingApproval    TransactionStatus = "pending_approval"
	TransactionStatusProcessing         TransactionStatus = "processing"
	TransactionStatusReserved           TransactionStatus = "reserved"
	TransactionStatusSettling           TransactionStatus = "settling"
	TransactionStatusPendingSettlement  TransactionStatus = "pending_settlement"
	TransactionStatusCompleted          TransactionStatus = "completed"
	TransactionStatusFailed             TransactionStatus = "failed"
	TransactionStatusDisputed           TransactionStatus = "disputed"
	TransactionStatusReversed           TransactionStatus = "reversed"
	TransactionStatusRefunded           TransactionStatus = "refunded"
	TransactionStatusCancelled          TransactionStatus = "cancelled"
	TransactionStatusRequiresReview     TransactionStatus = "requires_review"
	TransactionStatusAdminInvestigation TransactionStatus = "admin_investigation"
)

type TransactionType string

const (
	TransactionTypeP2P           TransactionType = "p2p"
	TransactionTypeMerchantPay   TransactionType = "merchant_pay"
	TransactionTypeAgentDeposit  TransactionType = "agent_deposit"
	TransactionTypeAgentWithdraw TransactionType = "agent_withdraw"
	TransactionTypeCrossBorder   TransactionType = "cross_border"
	TransactionTypePayment       TransactionType = "payment"
	TransactionTypeTransfer      TransactionType = "transfer"
	TransactionTypeWithdrawal    TransactionType = "withdrawal"
	TransactionTypeDeposit       TransactionType = "deposit"
	TransactionTypeRefund        TransactionType = "refund"
	TransactionTypeReversal      TransactionType = "reversal"
	TransactionTypeSettlement    TransactionType = "settlement"
)

// Metadata is a JSON-compatible map
type Metadata map[string]interface{}

func (m Metadata) Value() (driver.Value, error) {
	return json.Marshal(m)
}

func (m *Metadata) Scan(value interface{}) error {
	b, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	return json.Unmarshal(b, &m)
}

// ExchangeRate represents a currency exchange rate
type ExchangeRate struct {
	ID             uuid.UUID       `json:"id" db:"id"`
	BaseCurrency   Currency        `json:"base_currency" db:"base_currency"`
	TargetCurrency Currency        `json:"target_currency" db:"target_currency"`
	Rate           decimal.Decimal `json:"rate" db:"rate"`
	BuyRate        decimal.Decimal `json:"buy_rate" db:"buy_rate"`
	SellRate       decimal.Decimal `json:"sell_rate" db:"sell_rate"`
	Spread         decimal.Decimal `json:"spread" db:"spread"`
	ValidFrom      time.Time       `json:"valid_from" db:"valid_from"`
	ValidTo        *time.Time      `json:"valid_to" db:"valid_to"`
	Source         string          `json:"source" db:"source"`
	Provider       string          `json:"provider" db:"provider"`
	IsInterbank    bool            `json:"is_interbank" db:"is_interbank"`
	LastUpdated    time.Time       `json:"last_updated" db:"last_updated"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
	VolatilityRisk string          `json:"volatility_risk" db:"volatility_risk"`
}

// Settlement represents a daily settlement batch
type Settlement struct {
	ID                 uuid.UUID         `json:"id" db:"id"`
	BatchID            string            `json:"batch_id" db:"batch_id"`
	BatchReference     string            `json:"batch_reference" db:"batch_reference"`
	Network            BlockchainNetwork `json:"network" db:"network"`
	SourceAccount      string            `json:"source_account" db:"source_account"`
	DestinationAccount string            `json:"destination_account" db:"destination_account"`
	TotalAmount        decimal.Decimal   `json:"total_amount" db:"total_amount"`
	Currency           Currency          `json:"currency" db:"currency"`
	TransactionCount   int               `json:"transaction_count" db:"transaction_count"`
	SubmissionCount    int               `json:"submission_count" db:"submission_count"`
	Status             SettlementStatus  `json:"status" db:"status"`
	BlockchainTxHash   string            `json:"blockchain_tx_hash" db:"blockchain_tx_hash"`
	TransactionHash    string            `json:"transaction_hash" db:"transaction_hash"`
	NetworkFee         decimal.Decimal   `json:"network_fee" db:"network_fee"`
	FeeAmount          decimal.Decimal   `json:"fee_amount" db:"fee_amount"`
	FeeCurrency        Currency          `json:"fee_currency" db:"fee_currency"`
	Metadata           Metadata          `json:"metadata" db:"metadata"`
	SettledAt          *time.Time        `json:"settled_at" db:"settled_at"`
	LastSubmittedAt    *time.Time        `json:"last_submitted_at" db:"last_submitted_at"`
	ConfirmedAt        *time.Time        `json:"confirmed_at" db:"confirmed_at"`
	CompletedAt        *time.Time        `json:"completed_at" db:"completed_at"`
	ReconciliationID   *string           `json:"reconciliation_id" db:"reconciliation_id"`
	CreatedAt          time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at" db:"updated_at"`
}

type BlockchainNetwork string

const (
	BlockchainStellar   BlockchainNetwork = "stellar"
	BlockchainRipple    BlockchainNetwork = "ripple"
	BlockchainLocal     BlockchainNetwork = "local"
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

// AuditLog represents a security audit event
type AuditLog struct {
	ID           uuid.UUID       `json:"id" db:"id"`
	Action       string          `json:"action" db:"action"`
	Resource     string          `json:"resource" db:"resource"`
	EntityType   string          `json:"entity_type" db:"entity_type"`
	ResourceID   string          `json:"resource_id" db:"resource_id"`
	EntityID     string          `json:"entity_id" db:"entity_id"`
	UserID       *uuid.UUID      `json:"user_id" db:"user_id"`
	UserEmail    string          `json:"user_email" db:"user_email"`
	IPAddress    string          `json:"ip_address" db:"ip_address"`
	UserAgent    string          `json:"user_agent" db:"user_agent"`
	Changes      json.RawMessage `json:"changes" db:"changes"`
	NewValues    json.RawMessage `json:"new_values" db:"new_values"`
	OldValues    json.RawMessage `json:"old_values" db:"old_values"`
	Status       string          `json:"status" db:"status"`
	StatusCode   int             `json:"status_code" db:"status_code"`
	ErrorMessage string          `json:"error_message" db:"error_message"`
	RequestID    string          `json:"request_id" db:"request_id"`
	Metadata     Metadata        `json:"metadata" db:"metadata"`
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`
}

// TransactionLedger represents an immutable ledger entry
type TransactionLedger struct {
	ID             uuid.UUID       `json:"id" db:"id"`
	TransactionID  uuid.UUID       `json:"transaction_id" db:"transaction_id"`
	WalletID       uuid.UUID       `json:"wallet_id" db:"wallet_id"`
	Type           string          `json:"type" db:"type"` // CREDIT / DEBIT
	EventType      string          `json:"event_type" db:"event_type"`
	Amount         decimal.Decimal `json:"amount" db:"amount"`
	Currency       Currency        `json:"currency" db:"currency"`
	BalanceAfter   decimal.Decimal `json:"balance_after" db:"balance_after"`
	RunningBalance decimal.Decimal `json:"running_balance" db:"running_balance"`
	PreviousHash   string          `json:"previous_hash" db:"previous_hash"`
	Hash           string          `json:"hash" db:"hash"`
	Nonce          int64           `json:"nonce" db:"nonce"`
	Status         string          `json:"status" db:"status"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
}

// SecurityEvent represents a security incident or alert
type SecurityEvent struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	Type        string     `json:"type" db:"type"`         // e.g., "suspicious_login", "velocity_limit"
	Severity    string     `json:"severity" db:"severity"` // low, medium, high, critical
	Description string     `json:"description" db:"description"`
	UserID      *uuid.UUID `json:"user_id" db:"user_id"`
	ResourceID  *string    `json:"resource_id" db:"resource_id"`
	IPAddress   string     `json:"ip_address" db:"ip_address"`
	Location    string     `json:"location" db:"location"`
	Status      string     `json:"status" db:"status"` // open, investigating, resolved
	ResolvedBy  *uuid.UUID `json:"resolved_by" db:"resolved_by"`
	ResolvedAt  *time.Time `json:"resolved_at" db:"resolved_at"`
	Metadata    Metadata   `json:"metadata" db:"metadata"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
}

// BlocklistEntry represents a blocked entity (IP, User, Device)
type BlocklistEntry struct {
	ID        uuid.UUID  `json:"id" db:"id"`
	Type      string     `json:"type" db:"type"` // ip, user, device, wallet
	Value     string     `json:"value" db:"value"`
	Reason    string     `json:"reason" db:"reason"`
	AddedBy   uuid.UUID  `json:"added_by" db:"added_by"`
	ExpiresAt *time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
}

// SystemHealthMetric represents a system metric (e.g. error rate)
type SystemHealthMetric struct {
	MetricName string    `json:"metric_name" db:"metric_name"`
	Value      float64   `json:"value" db:"value"`
	RecordedAt time.Time `json:"recorded_at" db:"recorded_at"`
}

// TransactionVolume represents daily transaction volume
type TransactionVolume struct {
	Period string          `json:"period" db:"period"`
	CNY    decimal.Decimal `json:"cny" db:"cny"`
	MWK    decimal.Decimal `json:"mwk" db:"mwk"`
	ZMW    decimal.Decimal `json:"zmw" db:"zmw"`
	Total  decimal.Decimal `json:"total" db:"total"`
}

// SystemStats represents aggregated system statistics
type SystemStats struct {
	TotalTransactions int64           `json:"total_transactions" db:"total_transactions"`
	Completed         int64           `json:"completed" db:"completed"`
	Pending           int64           `json:"pending" db:"pending"`
	PendingApprovals  int64           `json:"pending_approvals" db:"pending_approvals"`
	Flagged           int64           `json:"flagged" db:"flagged"`
	TotalVolume       decimal.Decimal `json:"total_volume" db:"total_volume"`
	TotalFees         decimal.Decimal `json:"total_fees" db:"total_fees"`
	ActiveUsers       int64           `json:"active_users" db:"active_users"`
}

const (
	SecurityEventTypeBruteForce       = "brute_force"
	SecurityEventTypeSuspiciousIP     = "suspicious_ip"
	SecurityEventTypeAdminLoginFailed = "admin_login_failed"
	SecurityEventTypeVelocityLimit    = "velocity_limit"

	SecuritySeverityCritical = "critical"
	SecuritySeverityHigh     = "high"
	SecuritySeverityMedium   = "medium"
	SecuritySeverityLow      = "low"

	SecurityEventStatusOpen          = "open"
	SecurityEventStatusInvestigating = "investigating"
	SecurityEventStatusResolved      = "resolved"
	SecurityEventStatusFalsePositive = "false_positive"
)
