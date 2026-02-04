// Package notification implements a multi-channel notification system.
package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/logger"

	"github.com/google/uuid"
)

// AuditRepository defines the interface for audit logging.
type AuditRepository interface {
	Create(ctx context.Context, log *domain.AuditLog) error
}

// ChannelType represents the delivery method (Email, SMS, Push).
type ChannelType string

const (
	ChannelEmail ChannelType = "EMAIL"
	ChannelSMS   ChannelType = "SMS"
	ChannelPush  ChannelType = "PUSH"
)

// Priority represents the urgency of the notification.
type Priority int

const (
	PriorityLow    Priority = 0
	PriorityNormal Priority = 1
	PriorityHigh   Priority = 2
	PriorityUrgent Priority = 3
)

// Notification represents a message to be sent.
type Notification struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Type      string // e.g., "PAYMENT_RECEIVED", "LOGIN_ALERT"
	Channel   ChannelType
	Priority  Priority
	Subject   string
	Body      string
	Metadata  map[string]interface{}
	CreatedAt time.Time
}

// Service defines the notification service interface.
type Service interface {
	Notify(ctx context.Context, userID uuid.UUID, eventType string, data map[string]interface{}) error
	SendRaw(ctx context.Context, n *Notification) error
}

// DefaultService is the concrete implementation.
type DefaultService struct {
	logger    logger.Logger
	auditRepo AuditRepository
	// In a real system, we'd have providers here (e.g., SendGrid, Twilio)
	// For now, we simulate them.
	mu sync.Mutex
}

// NewService creates a new notification service.
func NewService(log logger.Logger, auditRepo AuditRepository) *DefaultService {
	return &DefaultService{
		logger:    log,
		auditRepo: auditRepo,
	}
}

// Notify constructs and sends a notification based on an event type.
func (s *DefaultService) Notify(ctx context.Context, userID uuid.UUID, eventType string, data map[string]interface{}) error {
	// Template logic would go here. For now, we hardcode a few templates.
	var subject, body string
	var priority Priority = PriorityNormal

	switch eventType {
	case "PAYMENT_SENT":
		amount := data["amount"]
		currency := data["currency"]
		receiver := data["receiver_name"]
		subject = "Payment Sent"
		body = fmt.Sprintf("You sent %v %v to %v.", amount, currency, receiver)
		priority = PriorityHigh

	case "PAYMENT_RECEIVED":
		amount := data["amount"]
		currency := data["currency"]
		sender := data["sender_name"]
		subject = "Payment Received"
		body = fmt.Sprintf("You received %v %v from %v.", amount, currency, sender)
		priority = PriorityHigh

	case "LOGIN_NEW_DEVICE":
		device := data["device_name"]
		location := data["location"]
		subject = "New Login Detected"
		body = fmt.Sprintf("New login from %v near %v. If this wasn't you, freeze your account immediately.", device, location)
		priority = PriorityUrgent

	case "RISK_ALERT":
		reason := data["reason"]
		subject = "Security Alert"
		body = fmt.Sprintf("Your transaction was flagged: %v. Please contact support.", reason)
		priority = PriorityUrgent

	default:
		subject = "Notification"
		body = fmt.Sprintf("Event: %s", eventType)
	}

	n := &Notification{
		ID:        uuid.New(),
		UserID:    userID,
		Type:      eventType,
		Channel:   ChannelEmail, // Default to Email
		Priority:  priority,
		Subject:   subject,
		Body:      body,
		Metadata:  data,
		CreatedAt: time.Now(),
	}

	return s.SendRaw(ctx, n)
}

// SendRaw handles the actual delivery simulation.
func (s *DefaultService) SendRaw(ctx context.Context, n *Notification) error {
	// In a real app, this would be async (via queue).
	// For simulation, we just log it.

	// Simulate processing time
	// time.Sleep(10 * time.Millisecond)

	s.logger.Info("Notification Sent", map[string]interface{}{
		"notification_id": n.ID,
		"user_id":         n.UserID,
		"channel":         n.Channel,
		"type":            n.Type,
		"subject":         n.Subject,
		"priority":        n.Priority,
	})

	// If it's urgent, simulate SMS as well
	if n.Priority == PriorityUrgent {
		s.logger.Info("SMS Sent (Urgent)", map[string]interface{}{
			"user_id": n.UserID,
			"body":    n.Body,
		})
	}

	// Create Audit Log
	if s.auditRepo != nil {
		action := "NOTIFICATION_SENT"
		entityType := "notification"
		newVals := domain.Metadata{
			"channel":  n.Channel,
			"type":     n.Type,
			"subject":  n.Subject,
			"priority": n.Priority,
			"body":     n.Body,
		}

		// Use background context if ctx is cancelled, or just use ctx
		// Here we use a new context with timeout to ensure audit log is written even if request finishes
		auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		newValsBytes, _ := json.Marshal(newVals)

		err := s.auditRepo.Create(auditCtx, &domain.AuditLog{
			ID:         uuid.New(),
			UserID:     &n.UserID,
			Action:     action,
			EntityType: entityType,
			EntityID:   n.ID.String(),
			NewValues:  newValsBytes,
			CreatedAt:  time.Now(),
		})
		if err != nil {
			s.logger.Error("Failed to create audit log for notification", map[string]interface{}{
				"error":           err.Error(),
				"notification_id": n.ID,
			})
		}
	}

	return nil
}
