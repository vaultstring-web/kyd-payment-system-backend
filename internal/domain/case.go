package domain

import (
	"time"

	"github.com/google/uuid"
)

type CaseStatus string

const (
	CaseStatusOpen          CaseStatus = "open"
	CaseStatusInvestigating CaseStatus = "investigating"
	CaseStatusResolved      CaseStatus = "resolved"
	CaseStatusFalsePositive CaseStatus = "false_positive"
)

type CasePriority string

const (
	CasePriorityLow      CasePriority = "low"
	CasePriorityMedium   CasePriority = "medium"
	CasePriorityHigh     CasePriority = "high"
	CasePriorityCritical CasePriority = "critical"
)

type CaseEntityType string

const (
	CaseEntityUser        CaseEntityType = "user"
	CaseEntityTransaction CaseEntityType = "transaction"
	CaseEntityWallet      CaseEntityType = "wallet"
	CaseEntityIP          CaseEntityType = "ip"
)

type Case struct {
	ID          uuid.UUID      `json:"id" db:"id"`
	Title       string         `json:"title" db:"title"`
	Description *string        `json:"description,omitempty" db:"description"`
	Status      CaseStatus     `json:"status" db:"status"`
	Priority    CasePriority   `json:"priority" db:"priority"`
	EntityType  CaseEntityType `json:"entity_type" db:"entity_type"`
	EntityID    string         `json:"entity_id" db:"entity_id"`
	CreatedBy   *uuid.UUID     `json:"created_by,omitempty" db:"created_by"`
	AssignedTo  *uuid.UUID     `json:"assigned_to,omitempty" db:"assigned_to"`
	ResolvedAt  *time.Time     `json:"resolved_at,omitempty" db:"resolved_at"`
	CreatedAt   time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at" db:"updated_at"`
}

type CaseEventType string

const (
	CaseEventNote        CaseEventType = "note"
	CaseEventStatus      CaseEventType = "status_change"
	CaseEventAssignment  CaseEventType = "assignment"
	CaseEventLink        CaseEventType = "link"
)

type CaseEvent struct {
	ID        uuid.UUID     `json:"id" db:"id"`
	CaseID    uuid.UUID     `json:"case_id" db:"case_id"`
	EventType CaseEventType `json:"event_type" db:"event_type"`
	Message   *string       `json:"message,omitempty" db:"message"`
	Metadata  Metadata      `json:"metadata" db:"metadata"`
	CreatedBy *uuid.UUID    `json:"created_by,omitempty" db:"created_by"`
	CreatedAt time.Time     `json:"created_at" db:"created_at"`
}

