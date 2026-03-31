package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"kyd/internal/domain"
	"kyd/internal/middleware"
	"kyd/internal/notification"
	"kyd/pkg/logger"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type NotificationHandler struct {
	service *notification.DefaultService
	logger  logger.Logger
	repo    NotificationRepository
}

// NotificationRepository defines read access to stored notifications.
type NotificationRepository interface {
	ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.Notification, int, error)
	MarkRead(ctx context.Context, id uuid.UUID) error
	Archive(ctx context.Context, id uuid.UUID) error
}

func NewNotificationHandler(service *notification.DefaultService, repo NotificationRepository, log logger.Logger) *NotificationHandler {
	return &NotificationHandler{
		service: service,
		repo:    repo,
		logger:  log,
	}
}

type apiNotification struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Subject   string                 `json:"subject"`
	Body      string                 `json:"body"`
	CreatedAt time.Time              `json:"created_at"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	h.logger.Debug("Listing notifications for user", map[string]interface{}{
		"user_id": userID,
	})

	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	records, total, err := h.repo.ListByUser(r.Context(), userID, limit, offset)
	if err != nil {
		h.logger.Error("Failed to list notifications", map[string]interface{}{
			"error":   err.Error(),
			"user_id": userID,
		})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch notifications")
		return
	}

	notifications := make([]apiNotification, 0, len(records))
	for _, n := range records {
		metadata := map[string]interface{}(n.Data)
		notifications = append(notifications, apiNotification{
			ID:        n.ID.String(),
			Type:      n.Type,
			Subject:   n.Title,
			Body:      n.Message,
			CreatedAt: n.CreatedAt,
			Metadata:  metadata,
		})
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"notifications": notifications,
		"total":         total,
		"limit":         limit,
		"offset":        offset,
	})
}

func (h *NotificationHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid notification ID")
		return
	}

	// Best-effort: we don't currently enforce ownership check at DB level here.
	// The ID is unguessable UUID; admin screens have stronger controls.
	if err := h.repo.MarkRead(r.Context(), id); err != nil {
		h.logger.Error("Failed to mark notification read", map[string]interface{}{"error": err.Error(), "user_id": userID, "id": idStr})
		h.respondError(w, http.StatusInternalServerError, "Failed to mark notification read")
		return
	}
	h.respondJSON(w, http.StatusOK, map[string]string{"message": "ok"})
}

func (h *NotificationHandler) Archive(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid notification ID")
		return
	}

	if err := h.repo.Archive(r.Context(), id); err != nil {
		h.logger.Error("Failed to archive notification", map[string]interface{}{"error": err.Error(), "user_id": userID, "id": idStr})
		h.respondError(w, http.StatusInternalServerError, "Failed to archive notification")
		return
	}
	h.respondJSON(w, http.StatusOK, map[string]string{"message": "ok"})
}

func (h *NotificationHandler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (h *NotificationHandler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}
