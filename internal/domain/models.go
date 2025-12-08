// Package domain re-exports core domain types so internal code can import
// `kyd/internal/domain` while using definitions from `kyd/pkg/domain`.
package domain

import pkg "kyd/pkg/domain"

// Money represents a monetary amount.
type Money = pkg.Money

// Currency represents a currency code.
type Currency = pkg.Currency

// User represents a platform user.
type User = pkg.User

// UserType represents the type of user.
type UserType = pkg.UserType

// KYCStatus represents the KYC state of a user.
type KYCStatus = pkg.KYCStatus

// Wallet represents a user's wallet.
type Wallet = pkg.Wallet

// WalletStatus represents the status of a wallet.
type WalletStatus = pkg.WalletStatus

// Transaction represents a ledger transaction.
type Transaction = pkg.Transaction

// TransactionStatus represents transaction lifecycle states.
type TransactionStatus = pkg.TransactionStatus

// TransactionType represents categories of transactions.
type TransactionType = pkg.TransactionType

// Metadata holds arbitrary key-value metadata.
type Metadata = pkg.Metadata

// ExchangeRate represents an FX rate between currencies.
type ExchangeRate = pkg.ExchangeRate

// Settlement represents a settlement batch.
type Settlement = pkg.Settlement

// BlockchainNetwork identifies supported settlement networks.
type BlockchainNetwork = pkg.BlockchainNetwork

// SettlementStatus represents settlement lifecycle states.
type SettlementStatus = pkg.SettlementStatus

// Re-exported currency codes.
const (
	MWK = pkg.MWK
	CNY = pkg.CNY
	USD = pkg.USD
	EUR = pkg.EUR
)

// Re-exported user types.
const (
	UserTypeIndividual = pkg.UserTypeIndividual
	UserTypeMerchant   = pkg.UserTypeMerchant
	UserTypeAgent      = pkg.UserTypeAgent
	UserTypeAdmin      = pkg.UserTypeAdmin
)

// Re-exported KYC statuses.
const (
	KYCStatusPending    = pkg.KYCStatusPending
	KYCStatusProcessing = pkg.KYCStatusProcessing
	KYCStatusVerified   = pkg.KYCStatusVerified
	KYCStatusRejected   = pkg.KYCStatusRejected
)

// Re-exported wallet statuses.
const (
	WalletStatusActive    = pkg.WalletStatusActive
	WalletStatusSuspended = pkg.WalletStatusSuspended
	WalletStatusClosed    = pkg.WalletStatusClosed
)

// Re-exported transaction statuses.
const (
	TransactionStatusPending    = pkg.TransactionStatusPending
	TransactionStatusProcessing = pkg.TransactionStatusProcessing
	TransactionStatusReserved   = pkg.TransactionStatusReserved
	TransactionStatusSettling   = pkg.TransactionStatusSettling
	TransactionStatusCompleted  = pkg.TransactionStatusCompleted
	TransactionStatusFailed     = pkg.TransactionStatusFailed
	TransactionStatusCancelled  = pkg.TransactionStatusCancelled
	TransactionStatusRefunded   = pkg.TransactionStatusRefunded
)

// Re-exported transaction types.
const (
	TransactionTypePayment    = pkg.TransactionTypePayment
	TransactionTypeTransfer   = pkg.TransactionTypeTransfer
	TransactionTypeWithdrawal = pkg.TransactionTypeWithdrawal
	TransactionTypeDeposit    = pkg.TransactionTypeDeposit
	TransactionTypeRefund     = pkg.TransactionTypeRefund
	TransactionTypeReversal   = pkg.TransactionTypeReversal
	TransactionTypeSettlement = pkg.TransactionTypeSettlement
)

// Re-exported blockchain networks.
const (
	NetworkStellar      = pkg.NetworkStellar
	NetworkRipple       = pkg.NetworkRipple
	NetworkBankTransfer = pkg.NetworkBankTransfer
)

// Re-exported settlement statuses.
const (
	SettlementStatusPending    = pkg.SettlementStatusPending
	SettlementStatusProcessing = pkg.SettlementStatusProcessing
	SettlementStatusSubmitted  = pkg.SettlementStatusSubmitted
	SettlementStatusConfirmed  = pkg.SettlementStatusConfirmed
	SettlementStatusCompleted  = pkg.SettlementStatusCompleted
	SettlementStatusFailed     = pkg.SettlementStatusFailed
	SettlementStatusReconciled = pkg.SettlementStatusReconciled
)
