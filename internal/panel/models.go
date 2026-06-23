// Package panel 提供 /panel/v1 管理面板 REST API。
package panel

import (
	"time"

	"github.com/grok-mcp/internal/store"
)

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token     string       `json:"token"`
	ExpiresAt time.Time    `json:"expires_at"`
	User      UserResponse `json:"user"`
}

type UserResponse struct {
	ID           string          `json:"id"`
	Username     string          `json:"username"`
	Role         store.UserRole  `json:"role"`
	Enabled      bool            `json:"enabled"`
	RPM          int             `json:"rpm"`
	TotalLimit   int             `json:"total_limit"`
	SuccessLimit int             `json:"success_limit"`
	TotalCalls   int64           `json:"total_calls"`
	SuccessCalls int64           `json:"success_calls"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type CreateKeyRequest struct {
	Name string `json:"name"`
}

type CreateKeyResponse struct {
	Key    KeyResponse `json:"key"`
	APIKey string      `json:"api_key"`
}

type UpdateKeyRequest struct {
	Name    *string `json:"name,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
}

type KeyResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	Enabled    bool       `json:"enabled"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	TotalCalls int64      `json:"total_calls"`
}

type UpdateUserRequest struct {
	Enabled      *bool           `json:"enabled,omitempty"`
	Role         *store.UserRole `json:"role,omitempty"`
	RPM          *int            `json:"rpm,omitempty"`
	TotalLimit   *int            `json:"total_limit,omitempty"`
	SuccessLimit *int            `json:"success_limit,omitempty"`
}

type UsageStatsResponse struct {
	TotalCalls   int64            `json:"total_calls"`
	SuccessCalls int64            `json:"success_calls"`
	ByTool       map[string]int64 `json:"by_tool"`
	Records      []UsageRecordDTO `json:"records,omitempty"`
}

type UsageRecordDTO struct {
	ID         int64     `json:"id"`
	KeyID      string    `json:"key_id"`
	ToolName   string    `json:"tool_name"`
	Timestamp  time.Time `json:"timestamp"`
	DurationMs int64     `json:"duration_ms"`
	Success    bool      `json:"success"`
}

func toUserResponse(u *store.User) UserResponse {
	return UserResponse{
		ID: u.ID, Username: u.Username, Role: u.Role, Enabled: u.Enabled,
		RPM: u.RPM, TotalLimit: u.TotalLimit, SuccessLimit: u.SuccessLimit,
		TotalCalls: u.TotalCalls, SuccessCalls: u.SuccessCalls,
		CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}

func toKeyResponse(k *store.APIKey) KeyResponse {
	return KeyResponse{
		ID: k.ID, Name: k.Name, KeyPrefix: k.KeyPrefix, Enabled: k.Enabled,
		CreatedAt: k.CreatedAt, UpdatedAt: k.UpdatedAt, LastUsedAt: k.LastUsedAt,
		TotalCalls: k.TotalCalls,
	}
}

func toUsageStatsResponse(s *store.UsageStats) UsageStatsResponse {
	out := UsageStatsResponse{
		TotalCalls: s.TotalCalls, SuccessCalls: s.SuccessCalls, ByTool: s.ByTool,
	}
	for _, r := range s.Records {
		out.Records = append(out.Records, UsageRecordDTO{
			ID: r.ID, KeyID: r.KeyID, ToolName: r.ToolName,
			Timestamp: r.Timestamp, DurationMs: r.DurationMs, Success: r.Success,
		})
	}
	return out
}