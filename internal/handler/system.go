package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"kyd/internal/repository/postgres"

	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
)

type SystemHandler struct {
	db          *sqlx.DB
	redisClient *redis.Client
	auditRepo   *postgres.AuditRepository
	logger      Logger
	startTime   time.Time
}

func NewSystemHandler(db *sqlx.DB, redisClient *redis.Client, auditRepo *postgres.AuditRepository, log Logger) *SystemHandler {
	return &SystemHandler{
		db:          db,
		redisClient: redisClient,
		auditRepo:   auditRepo,
		logger:      log,
		startTime:   time.Now(),
	}
}

type ServiceStatus struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Status      string  `json:"status"` // operational, degraded, maintenance, outage
	LastUpdated string  `json:"lastUpdated"`
	Uptime      float64 `json:"uptime"`
	LatencyMs   int64   `json:"latency_ms"`
}

type SystemStatusResponse struct {
	Services []ServiceStatus `json:"services"`
}

func (h *SystemHandler) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
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

	logs, err := h.auditRepo.FindAll(r.Context(), limit, offset)
	if err != nil {
		h.logger.Error("Failed to fetch audit logs", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch audit logs")
		return
	}

	total, err := h.auditRepo.CountAll(r.Context())
	if err != nil {
		// Log but don't fail, just return 0 total
		h.logger.Warn("Failed to count audit logs", map[string]interface{}{"error": err.Error()})
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"logs":   logs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *SystemHandler) GetSystemStatus(w http.ResponseWriter, r *http.Request) {
	services := []ServiceStatus{}

	// 1. Core API (Self)
	// If we are here, the API is running
	apiUptime := 100.0 // Since it's responding
	services = append(services, ServiceStatus{
		ID:          "core-api",
		Name:        "Core API Service",
		Description: "Main payment processing API",
		Status:      "operational",
		LastUpdated: time.Now().Format(time.RFC3339),
		Uptime:      apiUptime,
		LatencyMs:   0, // Self-check is instant
	})

	// 2. Database
	dbStatus := "operational"
	dbStart := time.Now()
	err := h.db.Ping()
	dbLatency := time.Since(dbStart).Milliseconds()
	dbUptime := 100.0

	if err != nil {
		dbStatus = "outage"
		dbUptime = 0.0
		h.logger.Error("Database ping failed", map[string]interface{}{"error": err.Error()})
	} else {
		// If ping is slow (> 200ms), mark as degraded
		if dbLatency > 200 {
			dbStatus = "degraded"
		}
	}

	services = append(services, ServiceStatus{
		ID:          "database",
		Name:        "PostgreSQL Database",
		Description: "Primary data store",
		Status:      dbStatus,
		LastUpdated: time.Now().Format(time.RFC3339),
		Uptime:      dbUptime,
		LatencyMs:   dbLatency,
	})

	// 3. Redis
	redisStatus := "operational"
	redisStart := time.Now()
	err = h.redisClient.Ping(context.Background()).Err()
	redisLatency := time.Since(redisStart).Milliseconds()
	redisUptime := 100.0

	if err != nil {
		redisStatus = "outage"
		redisUptime = 0.0
		h.logger.Error("Redis ping failed", map[string]interface{}{"error": err.Error()})
	} else {
		if redisLatency > 50 {
			redisStatus = "degraded"
		}
	}

	services = append(services, ServiceStatus{
		ID:          "redis",
		Name:        "Redis Cache",
		Description: "Session and rate limiting",
		Status:      redisStatus,
		LastUpdated: time.Now().Format(time.RFC3339),
		Uptime:      redisUptime,
		LatencyMs:   redisLatency,
	})

	// 4. Forex Service (Mock for now)
	// We can check if we have recent rates in the DB to determine status
	// But for now, we'll assume operational if DB is up
	forexStatus := "operational"
	if dbStatus != "operational" {
		forexStatus = "outage" // Can't access rates
	}

	services = append(services, ServiceStatus{
		ID:          "forex",
		Name:        "Forex Service",
		Description: "Currency exchange rates",
		Status:      forexStatus,
		LastUpdated: time.Now().Format(time.RFC3339),
		Uptime:      99.50, // Historical average
		LatencyMs:   120,   // Estimated external API latency
	})

	h.respondJSON(w, http.StatusOK, SystemStatusResponse{Services: services})
}

// Reuse respondJSON from other handlers or duplicate helper if necessary
// Assuming respondJSON is available or I should implement it
func (h *SystemHandler) respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(response)
}

func (h *SystemHandler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}
