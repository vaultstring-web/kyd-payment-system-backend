package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"kyd/internal/domain"
	"kyd/internal/payment"
	"kyd/pkg/logger"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type RecurringPayment struct {
	ID                  string
	SenderID            uuid.UUID
	ReceiverID          uuid.UUID
	Amount              decimal.Decimal
	Currency            domain.Currency
	Interval            time.Duration
	NextRun             time.Time
	Status              string // "active", "paused", "cancelled"
	Description         string
	DestinationCurrency domain.Currency
}

type Scheduler struct {
	paymentService *payment.Service
	tasks          map[string]*RecurringPayment
	mu             sync.RWMutex
	logger         logger.Logger
	stop           chan struct{}
}

func NewScheduler(ps *payment.Service, log logger.Logger) *Scheduler {
	return &Scheduler{
		paymentService: ps,
		tasks:          make(map[string]*RecurringPayment),
		logger:         log,
		stop:           make(chan struct{}),
	}
}

func (s *Scheduler) SchedulePayment(rp *RecurringPayment) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rp.ID == "" {
		rp.ID = uuid.New().String()
	}
	if rp.NextRun.IsZero() {
		rp.NextRun = time.Now().Add(rp.Interval)
	}
	rp.Status = "active"

	s.tasks[rp.ID] = rp
	s.logger.Info("Scheduled recurring payment", map[string]interface{}{
		"id":       rp.ID,
		"interval": rp.Interval.String(),
		"amount":   rp.Amount.String(),
	})
}

func (s *Scheduler) Start() {
	ticker := time.NewTicker(1 * time.Second) // Check every second for demo purposes
	go func() {
		for {
			select {
			case <-ticker.C:
				s.processTasks()
			case <-s.stop:
				ticker.Stop()
				return
			}
		}
	}()
	s.logger.Info("Payment Scheduler started", nil)
}

func (s *Scheduler) Stop() {
	close(s.stop)
}

func (s *Scheduler) processTasks() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, task := range s.tasks {
		if task.Status == "active" && now.After(task.NextRun) {
			s.executePayment(task)
			task.NextRun = now.Add(task.Interval)
		}
	}
}

func (s *Scheduler) executePayment(rp *RecurringPayment) {
	s.logger.Info("Executing recurring payment", map[string]interface{}{"id": rp.ID})

	req := payment.InitiatePaymentRequest{
		SenderID:            rp.SenderID,
		ReceiverID:          rp.ReceiverID,
		Amount:              rp.Amount,
		Currency:            rp.Currency,
		DestinationCurrency: rp.DestinationCurrency,
		Description:         fmt.Sprintf("Standing Order: %s", rp.Description),
		Channel:             "api",
		Category:            "STANDING_ORDER",
		Reference:           fmt.Sprintf("%s-%d", rp.ID, time.Now().Unix()),
		DeviceID:            "system-scheduler",
		Location:            "Internal",
		Metadata: map[string]interface{}{
			"source":               "scheduler",
			"recurring_payment_id": rp.ID,
		},
	}

	// We launch this in a goroutine to not block the scheduler loop,
	// but in a real system we might want a worker pool.
	go func() {
		ctx := context.Background()
		resp, err := s.paymentService.InitiatePayment(ctx, &req)
		if err != nil {
			s.logger.Error("Failed to execute recurring payment", map[string]interface{}{
				"id":    rp.ID,
				"error": err.Error(),
			})
		} else {
			s.logger.Info("Recurring payment success", map[string]interface{}{
				"id":    rp.ID,
				"tx_id": resp.Transaction.ID,
			})
		}
	}()
}
