package domain

import "github.com/shopspring/decimal"

// TransactionVolume represents aggregated transaction volume for a period
type TransactionVolume struct {
	Period string          `json:"period" db:"period"`
	CNY    decimal.Decimal `json:"cny" db:"cny"`
	MWK    decimal.Decimal `json:"mwk" db:"mwk"`
	ZMW    decimal.Decimal `json:"zmw" db:"zmw"`
	Total  decimal.Decimal `json:"total" db:"total"`
}

// SystemStats represents overall system statistics
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
