package panel

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/auth"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/ratelimit"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// loadUserTierForResponse resolves the tier used to populate panel limits.
// Missing tiers are logged and represented as unavailable instead of unlimited.
func (handler *Handler) loadUserTierForResponse(ctx context.Context, user *store.User) *store.Tier {
	if user == nil {
		return nil
	}
	tierID := strings.TrimSpace(user.TierID)
	if tierID == "" {
		log.Printf("user %s has empty tier_id; limits unavailable", user.ID)
		return nil
	}
	tier, err := handler.Store.GetTierByID(ctx, tierID)
	if err != nil {
		if errors.Is(err, store.ErrTierNotFound) {
			log.Printf("user %s tier_id %q not found; limits unavailable", user.ID, tierID)
			return nil
		}
		log.Printf("user %s load tier %q failed: %v; limits unavailable", user.ID, tierID, err)
		return nil
	}
	if tier == nil {
		log.Printf("user %s tier_id %q returned nil; limits unavailable", user.ID, tierID)
		return nil
	}
	return tier
}

// dummyBcryptHash equalizes login timing when a username does not exist.
var dummyBcryptHash = func() []byte {
	dummyHash, err := bcrypt.GenerateFromPassword([]byte("grok-mcp-timing-dummy-password"), bcryptCost)
	if err != nil {
		return nil
	}
	return dummyHash
}()

func (handler *Handler) login(writer http.ResponseWriter, request *http.Request) {
	var loginRequest LoginRequest
	if !decodeJSONBody(writer, request, &loginRequest) {
		return
	}
	username, err := validatePanelAuthCredentials(loginRequest.Username, loginRequest.Password)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	authProtector := handler.authProtector()
	clientIP, shouldApplyIPProtection, clientIPError := authProtector.clientIPForProtection(request)
	if clientIPError != nil {
		writeError(writer, http.StatusBadRequest, ratelimit.ErrInvalidForwardedClientIPHeaders.Error())
		return
	}
	if shouldApplyIPProtection {
		if locked, retryAfter := authProtector.LoginLocked(username, clientIP); locked {
			writeRetryAfter(writer, retryAfter)
			writeError(writer, http.StatusTooManyRequests, "too many failed login attempts")
			return
		}
	}
	user, err := handler.Store.GetUserByUsername(request.Context(), username)

	// Always execute bcrypt so unknown usernames cannot be enumerated by timing.
	hashToCheck := dummyBcryptHash
	if err == nil && user != nil {
		hashToCheck = []byte(user.PasswordHash)
	}
	compareErr := bcrypt.CompareHashAndPassword(hashToCheck, []byte(loginRequest.Password))
	if err != nil || user == nil || compareErr != nil {
		if shouldApplyIPProtection {
			authProtector.RecordLoginFailure(username, clientIP)
		}
		writeError(writer, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !user.Enabled {
		if shouldApplyIPProtection {
			authProtector.RecordLoginFailure(username, clientIP)
		}
		writeError(writer, http.StatusForbidden, "user disabled")
		return
	}
	if shouldApplyIPProtection {
		authProtector.RecordLoginSuccess(username, clientIP)
	}
	token, expiresAt, err := auth.IssuePanelToken(handler.JWTSecret, user, 0)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "token issue failed")
		return
	}
	writeJSON(writer, http.StatusOK, LoginResponse{
		Token: token, ExpiresAt: expiresAt, User: toUserResponseWithTier(user, handler.loadUserTierForResponse(request.Context(), user)),
	})
}

func (handler *Handler) me(writer http.ResponseWriter, request *http.Request) {
	user, ok := auth.UserFromContext(request.Context())
	if !ok {
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return
	}
	freshUser, err := handler.Store.GetUserByID(request.Context(), user.ID)
	if err != nil {
		log.Printf("get user %s failed: %v", user.ID, err)
		writeError(writer, http.StatusInternalServerError, "failed to load user")
		return
	}
	writeJSON(writer, http.StatusOK, toUserResponseWithTier(freshUser, handler.loadUserTierForResponse(request.Context(), freshUser)))
}
