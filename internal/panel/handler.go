package panel

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/grok-mcp/internal/auth"
	"github.com/grok-mcp/internal/config"
	"github.com/grok-mcp/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// Handler 实现面板 API；路由由 NewMux 注册。
type Handler struct {
	Store  store.Store
	Config *config.Config
}

// NewMux 注册 /panel/v1 路由（外层需套 PanelKey + JWT + 可选 Admin 中间件）。
func NewMux(h *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /panel/v1/auth/register", h.register)
	mux.HandleFunc("POST /panel/v1/auth/login", h.login)
	mux.HandleFunc("GET /panel/v1/me", h.me)

	mux.HandleFunc("GET /panel/v1/keys", h.listKeys)
	mux.HandleFunc("POST /panel/v1/keys", h.createKey)
	mux.HandleFunc("PATCH /panel/v1/keys/{id}", h.updateKey)
	mux.HandleFunc("DELETE /panel/v1/keys/{id}", h.deleteKey)
	mux.HandleFunc("GET /panel/v1/keys/{id}/usage", h.keyUsage)

	mux.HandleFunc("GET /panel/v1/admin/users", h.adminListUsers)
	mux.HandleFunc("GET /panel/v1/admin/users/{id}", h.adminGetUser)
	mux.HandleFunc("PATCH /panel/v1/admin/users/{id}", h.adminUpdateUser)
	mux.HandleFunc("GET /panel/v1/admin/users/{id}/usage", h.adminUserUsage)
	return mux
}

func parseSince(r *http.Request) time.Time {
	raw := strings.TrimSpace(r.URL.Query().Get("since"))
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "username required and password must be at least 8 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password hash failed")
		return
	}
	user, err := h.Store.RegisterUser(r.Context(), username, string(hash),
		h.Config.DefaultUserRPM, h.Config.DefaultUserTotalLimit, h.Config.DefaultUserSuccessLimit)
	if err != nil {
		if errors.Is(err, store.ErrUsernameTaken) {
			writeError(w, http.StatusConflict, "username already taken")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toUserResponse(user))
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	user, err := h.Store.GetUserByUsername(r.Context(), req.Username)
	if err != nil || user == nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !user.Enabled {
		writeError(w, http.StatusForbidden, "user disabled")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	token, exp, err := auth.IssuePanelToken(h.Config.JWTSecret, user, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token issue failed")
		return
	}
	writeJSON(w, http.StatusOK, LoginResponse{
		Token: token, ExpiresAt: exp, User: toUserResponse(user),
	})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	fresh, err := h.Store.GetUserByID(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(fresh))
}

func (h *Handler) listKeys(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	keys, err := h.Store.ListKeysByUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]KeyResponse, 0, len(keys))
	for _, k := range keys {
		out = append(out, toKeyResponse(k))
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": out})
}

func (h *Handler) createKey(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req CreateKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	k, raw, err := h.Store.CreateKey(r.Context(), user.ID, req.Name, 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, CreateKeyResponse{Key: toKeyResponse(k), APIKey: raw})
}

func (h *Handler) updateKey(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := r.PathValue("id")
	k, err := h.Store.GetKeyByID(r.Context(), id)
	if err != nil || k.UserID != user.ID {
		writeError(w, http.StatusNotFound, "api key not found")
		return
	}
	var req UpdateKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	updated, err := h.Store.UpdateKey(r.Context(), id, store.KeyUpdates{Name: req.Name, Enabled: req.Enabled})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toKeyResponse(updated))
}

func (h *Handler) deleteKey(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := r.PathValue("id")
	k, err := h.Store.GetKeyByID(r.Context(), id)
	if err != nil || k.UserID != user.ID {
		writeError(w, http.StatusNotFound, "api key not found")
		return
	}
	if err := h.Store.DeleteKey(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) keyUsage(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := r.PathValue("id")
	k, err := h.Store.GetKeyByID(r.Context(), id)
	if err != nil || k.UserID != user.ID {
		writeError(w, http.StatusNotFound, "api key not found")
		return
	}
	stats, err := h.Store.GetUsageStats(r.Context(), id, parseSince(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toUsageStatsResponse(stats))
}

func (h *Handler) adminListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.Store.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]UserResponse, 0, len(users))
	for _, u := range users {
		out = append(out, toUserResponse(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (h *Handler) adminGetUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	u, err := h.Store.GetUserByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(u))
}

func (h *Handler) adminUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	u, err := h.Store.UpdateUser(r.Context(), id, store.UserUpdates{
		Enabled: req.Enabled, Role: req.Role,
		RPM: req.RPM, TotalLimit: req.TotalLimit, SuccessLimit: req.SuccessLimit,
	})
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(u))
}

func (h *Handler) adminUserUsage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.Store.GetUserByID(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	stats, err := h.Store.GetUserUsageStats(r.Context(), id, parseSince(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toUsageStatsResponse(stats))
}