package admin_audit

import (
	"time"

	"github.com/google/uuid"
)

type ListQuery struct {
	Actor           string    `form:"actor"`
	Action          string    `form:"action"`
	Service         string    `form:"service"`
	RoleID          string    `form:"role_id" binding:"omitempty,uuid"`
	KeyID           string    `form:"key_id" binding:"omitempty,uuid"`
	ServiceClientID string    `form:"service_client_id" binding:"omitempty,uuid"`
	From            time.Time `form:"from" time_format:"2006-01-02T15:04:05Z07:00"`
	To              time.Time `form:"to" time_format:"2006-01-02T15:04:05Z07:00"`
}

type RecordResponse struct {
	ID              uuid.UUID      `json:"id"`
	CreatedAt       time.Time      `json:"created_at"`
	Actor           string         `json:"actor"`
	Source          string         `json:"source"`
	Action          string         `json:"action"`
	Service         string         `json:"service,omitempty"`
	RoleID          *uuid.UUID     `json:"role_id,omitempty"`
	KeyID           *uuid.UUID     `json:"key_id,omitempty"`
	ServiceClientID *uuid.UUID     `json:"service_client_id,omitempty"`
	Details         map[string]any `json:"details" swaggertype:"object"`
}

type PageResponse struct {
	Records []RecordResponse `json:"records"`
	Total   int64            `json:"total"`
}

func NewPageResponse(records []Record, total int64) PageResponse {
	resp := PageResponse{Records: make([]RecordResponse, len(records)), Total: total}
	for i, record := range records {
		resp.Records[i] = RecordResponse{
			ID:              record.ID,
			CreatedAt:       record.CreatedAt,
			Actor:           record.Actor,
			Source:          record.Source,
			Action:          record.Action,
			Service:         record.Service,
			RoleID:          record.RoleID,
			KeyID:           record.KeyID,
			ServiceClientID: record.ServiceClientID,
			Details:         record.Details,
		}
	}

	return resp
}
