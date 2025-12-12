// Package swift implements SWIFT message processing and cross-border payment handling.
package swift

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Processor handles SWIFT message creation, validation, and processing.
type Processor struct {
	mu        sync.RWMutex
	messages  map[string]*Message
	nostro    map[string]*NostroAccount
	validator *Validator
	formatter *Formatter
}

// Message represents a SWIFT message.
type Message struct {
	ID           string          `json:"id"`
	MessageType  string          `json:"message_type"`
	Reference    string          `json:"reference"`
	SenderBIC    string          `json:"sender_bic"`
	ReceiverBIC  string          `json:"receiver_bic"`
	Currency     string          `json:"currency"`
	Amount       decimal.Decimal `json:"amount"`
	ValueDate    time.Time       `json:"value_date"`
	Status       string          `json:"status"`
	RawMessage   string          `json:"raw_message,omitempty"`
	ParsedData   map[string]string
	Errors       []string        `json:"errors,omitempty"`
	ConfirmedAt  *time.Time      `json:"confirmed_at,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

// MT103 represents a Single Customer Credit Transfer.
type MT103 struct {
	SenderReference       string          `json:"sender_reference"`
	BankOperationCode     string          `json:"bank_operation_code"`
	ValueDate             time.Time       `json:"value_date"`
	Currency              string          `json:"currency"`
	Amount                decimal.Decimal `json:"amount"`
	OrderingCustomer      Party           `json:"ordering_customer"`
	OrderingInstitution   string          `json:"ordering_institution"`
	SenderCorrespondent   string          `json:"sender_correspondent"`
	ReceiverCorrespondent string          `json:"receiver_correspondent"`
	Beneficiary           Party           `json:"beneficiary"`
	BeneficiaryBank       string          `json:"beneficiary_bank"`
	RemittanceInfo        string          `json:"remittance_info"`
	Details               string          `json:"details"`
	SenderBIC             string          `json:"sender_bic"`
	ReceiverBIC           string          `json:"receiver_bic"`
}

// MT202 represents a General Financial Institution Transfer.
type MT202 struct {
	TransactionRef       string          `json:"transaction_ref"`
	RelatedRef           string          `json:"related_ref"`
	ValueDate            time.Time       `json:"value_date"`
	Currency             string          `json:"currency"`
	Amount               decimal.Decimal `json:"amount"`
	OrderingInstitution  string          `json:"ordering_institution"`
	SenderCorrespondent  string          `json:"sender_correspondent"`
	Intermediary         string          `json:"intermediary"`
	AccountWithInst      string          `json:"account_with_inst"`
	BeneficiaryInst      string          `json:"beneficiary_inst"`
	SenderBIC            string          `json:"sender_bic"`
	ReceiverBIC          string          `json:"receiver_bic"`
}

// Party represents a party in a SWIFT message.
type Party struct {
	Account   string `json:"account"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	City      string `json:"city"`
	Country   string `json:"country"`
}

// NostroAccount represents a nostro account with a correspondent bank.
type NostroAccount struct {
	ID           string          `json:"id"`
	BankBIC      string          `json:"bank_bic"`
	BankName     string          `json:"bank_name"`
	Currency     string          `json:"currency"`
	AccountNo    string          `json:"account_no"`
	Balance      decimal.Decimal `json:"balance"`
	Available    decimal.Decimal `json:"available"`
	ValueDate    time.Time       `json:"value_date"`
	LastUpdated  time.Time       `json:"last_updated"`
	IsActive     bool            `json:"is_active"`
}

// Validator validates SWIFT message fields.
type Validator struct{}

// Formatter formats SWIFT messages.
type Formatter struct{}

// NewProcessor creates a new SWIFT processor.
func NewProcessor() *Processor {
	return &Processor{
		messages:  make(map[string]*Message),
		nostro:    make(map[string]*NostroAccount),
		validator: &Validator{},
		formatter: &Formatter{},
	}
}

// ProcessMT103 processes a customer credit transfer.
func (p *Processor) ProcessMT103(ctx context.Context, mt103 MT103) (*Message, error) {
	// Validate the message
	if err := p.validator.ValidateMT103(mt103); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Create message record
	msg := &Message{
		ID:          uuid.New().String(),
		MessageType: "MT103",
		Reference:   mt103.SenderReference,
		SenderBIC:   mt103.SenderBIC,
		ReceiverBIC: mt103.ReceiverBIC,
		Currency:    mt103.Currency,
		Amount:      mt103.Amount,
		ValueDate:   mt103.ValueDate,
		Status:      "pending",
		CreatedAt:   time.Now(),
		ParsedData:  make(map[string]string),
	}

	// Format the raw message
	msg.RawMessage = p.formatter.FormatMT103(mt103)

	// Store message
	p.mu.Lock()
	p.messages[msg.ID] = msg
	p.mu.Unlock()

	return msg, nil
}

// ProcessMT202 processes a bank-to-bank transfer.
func (p *Processor) ProcessMT202(ctx context.Context, mt202 MT202) (*Message, error) {
	// Validate the message
	if err := p.validator.ValidateMT202(mt202); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	msg := &Message{
		ID:          uuid.New().String(),
		MessageType: "MT202",
		Reference:   mt202.TransactionRef,
		SenderBIC:   mt202.SenderBIC,
		ReceiverBIC: mt202.ReceiverBIC,
		Currency:    mt202.Currency,
		Amount:      mt202.Amount,
		ValueDate:   mt202.ValueDate,
		Status:      "pending",
		CreatedAt:   time.Now(),
		ParsedData:  make(map[string]string),
	}

	msg.RawMessage = p.formatter.FormatMT202(mt202)

	p.mu.Lock()
	p.messages[msg.ID] = msg
	p.mu.Unlock()

	return msg, nil
}

// ValidateBIC validates a SWIFT BIC code.
func (v *Validator) ValidateBIC(bic string) error {
	// BIC format: 4 letters (bank) + 2 letters (country) + 2 alphanumeric (location) + optional 3 alphanumeric (branch)
	pattern := regexp.MustCompile(`^[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}([A-Z0-9]{3})?$`)
	if !pattern.MatchString(bic) {
		return fmt.Errorf("invalid BIC format: %s", bic)
	}
	return nil
}

// ValidateIBAN validates an IBAN.
func (v *Validator) ValidateIBAN(iban string) error {
	// Basic IBAN validation (length varies by country)
	pattern := regexp.MustCompile(`^[A-Z]{2}[0-9]{2}[A-Z0-9]{4,30}$`)
	if !pattern.MatchString(iban) {
		return fmt.Errorf("invalid IBAN format: %s", iban)
	}
	return nil
}

// ValidateMT103 validates an MT103 message.
func (v *Validator) ValidateMT103(mt103 MT103) error {
	if err := v.ValidateBIC(mt103.SenderBIC); err != nil {
		return fmt.Errorf("sender BIC: %w", err)
	}
	if err := v.ValidateBIC(mt103.ReceiverBIC); err != nil {
		return fmt.Errorf("receiver BIC: %w", err)
	}
	if mt103.Amount.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("amount must be positive")
	}
	if mt103.Currency == "" || len(mt103.Currency) != 3 {
		return fmt.Errorf("invalid currency code")
	}
	if mt103.SenderReference == "" {
		return fmt.Errorf("sender reference is required")
	}
	if mt103.Beneficiary.Name == "" {
		return fmt.Errorf("beneficiary name is required")
	}
	return nil
}

// ValidateMT202 validates an MT202 message.
func (v *Validator) ValidateMT202(mt202 MT202) error {
	if err := v.ValidateBIC(mt202.SenderBIC); err != nil {
		return fmt.Errorf("sender BIC: %w", err)
	}
	if err := v.ValidateBIC(mt202.ReceiverBIC); err != nil {
		return fmt.Errorf("receiver BIC: %w", err)
	}
	if mt202.Amount.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("amount must be positive")
	}
	if mt202.TransactionRef == "" {
		return fmt.Errorf("transaction reference is required")
	}
	return nil
}

// FormatMT103 formats an MT103 message.
func (f *Formatter) FormatMT103(mt103 MT103) string {
	// Simplified SWIFT MT103 format
	return fmt.Sprintf(`{1:F01%s0000000000}{2:O1030000000000%s0000000000N}{4:
:20:%s
:23B:%s
:32A:%s%s%s
:50K:/%s
%s
%s
:59:/%s
%s
%s
:70:%s
:71A:%s
-}`,
		mt103.SenderBIC,
		mt103.ReceiverBIC,
		mt103.SenderReference,
		mt103.BankOperationCode,
		mt103.ValueDate.Format("060102"),
		mt103.Currency,
		mt103.Amount.StringFixed(2),
		mt103.OrderingCustomer.Account,
		mt103.OrderingCustomer.Name,
		mt103.OrderingCustomer.Address,
		mt103.Beneficiary.Account,
		mt103.Beneficiary.Name,
		mt103.Beneficiary.Address,
		mt103.RemittanceInfo,
		mt103.Details,
	)
}

// FormatMT202 formats an MT202 message.
func (f *Formatter) FormatMT202(mt202 MT202) string {
	return fmt.Sprintf(`{1:F01%s0000000000}{2:O2020000000000%s0000000000N}{4:
:20:%s
:21:%s
:32A:%s%s%s
:52A:%s
:58A:%s
-}`,
		mt202.SenderBIC,
		mt202.ReceiverBIC,
		mt202.TransactionRef,
		mt202.RelatedRef,
		mt202.ValueDate.Format("060102"),
		mt202.Currency,
		mt202.Amount.StringFixed(2),
		mt202.OrderingInstitution,
		mt202.BeneficiaryInst,
	)
}

// GetMessage retrieves a message by ID.
func (p *Processor) GetMessage(id string) (*Message, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	msg, exists := p.messages[id]
	if !exists {
		return nil, fmt.Errorf("message not found: %s", id)
	}
	return msg, nil
}

// UpdateMessageStatus updates the status of a message.
func (p *Processor) UpdateMessageStatus(id, status string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	msg, exists := p.messages[id]
	if !exists {
		return fmt.Errorf("message not found: %s", id)
	}

	msg.Status = status
	if status == "confirmed" {
		now := time.Now()
		msg.ConfirmedAt = &now
	}

	return nil
}

// AddNostroAccount adds a nostro account.
func (p *Processor) AddNostroAccount(account NostroAccount) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if account.ID == "" {
		account.ID = uuid.New().String()
	}
	account.LastUpdated = time.Now()
	p.nostro[account.ID] = &account
}

// GetNostroAccounts returns all nostro accounts.
func (p *Processor) GetNostroAccounts() []*NostroAccount {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]*NostroAccount, 0, len(p.nostro))
	for _, acc := range p.nostro {
		copy := *acc
		result = append(result, &copy)
	}
	return result
}

// UpdateNostroBalance updates a nostro account balance.
func (p *Processor) UpdateNostroBalance(id string, balance, available decimal.Decimal) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	acc, exists := p.nostro[id]
	if !exists {
		return fmt.Errorf("nostro account not found: %s", id)
	}

	acc.Balance = balance
	acc.Available = available
	acc.ValueDate = time.Now()
	acc.LastUpdated = time.Now()

	return nil
}

// GetNostroByBIC finds a nostro account by bank BIC.
func (p *Processor) GetNostroByBIC(bic, currency string) (*NostroAccount, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, acc := range p.nostro {
		if acc.BankBIC == bic && acc.Currency == currency && acc.IsActive {
			copy := *acc
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("nostro account not found for BIC %s, currency %s", bic, currency)
}
