package panel

import (
	"errors"
	"hash/maphash"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/ratelimit"
	"golang.org/x/time/rate"
)

const (
	authEndpointLogin                 authEndpoint = "login"
	authEndpointRegister              authEndpoint = "register"
	authEndpointRegistrationChallenge authEndpoint = "registration-challenge"

	minPanelUsernameBytes = 1
	maxPanelUsernameBytes = 128
	minPanelPasswordBytes = 8
	// bcrypt only accepts passwords up to 72 bytes. Rejecting longer inputs before
	// hashing avoids spending CPU on requests that cannot produce a valid login.
	maxPanelPasswordBytes = 72

	defaultLoginIPMaximumEntries                 = 4096
	defaultRegisterIPMaximumEntries              = 2048
	defaultRegistrationChallengeIPMaximumEntries = 2048
	defaultLoginFailureMaximumEntries            = 8192
	defaultAuthEndpointFallbackBuckets           = 16
	maximumAuthEndpointEntries                   = 65536
	maximumLoginFailureEntries                   = 131072
	maximumAuthEndpointFallbackBuckets           = 1024
	authRateLimiterIdleTTL                       = 30 * time.Minute
	authProtectorCleanupInterval                 = 5 * time.Minute
)

var errInvalidPanelAuthCredentials = errors.New("username must be 1-128 bytes and password must be 8-72 bytes")

type authEndpoint string

type authEndpointLimit struct {
	requestsPerMinute int
	burst             int
}

type authEndpointState struct {
	limitConfig           authEndpointLimit
	entries               map[string]*authRateLimitEntry
	fallbackLimiters      []*rate.Limiter
	maximumEntries        int
	nextExpiryCheck       time.Time
	currentEntries        atomic.Int64
	dedicatedAdmissions   atomic.Uint64
	expiredEntriesRemoved atomic.Uint64
	fallbackRequests      atomic.Uint64
	fallbackRejections    atomic.Uint64
}

type authRateLimitEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type loginFailureEntry struct {
	failureCount     int
	lastFailure      time.Time
	lockedUntil      time.Time
	inFlightAttempts int
}

// AuthEndpointMetricsSnapshot reports bounded endpoint limiter state without
// exposing client addresses or token-bucket contents.
type AuthEndpointMetricsSnapshot struct {
	CurrentEntries        int64  `json:"current_entries"`
	MaximumEntries        int64  `json:"maximum_entries"`
	FallbackBucketCount   int64  `json:"fallback_bucket_count"`
	DedicatedAdmissions   uint64 `json:"dedicated_admissions"`
	ExpiredEntriesRemoved uint64 `json:"expired_entries_removed"`
	FallbackRequests      uint64 `json:"fallback_requests"`
	FallbackRejections    uint64 `json:"fallback_rejections"`
}

// LoginFailureMetricsSnapshot reports bounded failed-login registry state.
type LoginFailureMetricsSnapshot struct {
	CurrentEntries        int64  `json:"current_entries"`
	MaximumEntries        int64  `json:"maximum_entries"`
	Admissions            uint64 `json:"admissions"`
	ExpiredEntriesRemoved uint64 `json:"expired_entries_removed"`
	CapacityRejections    uint64 `json:"capacity_rejections"`
}

// AuthProtectorMetricsSnapshot is an aggregate, non-sensitive protector view.
type AuthProtectorMetricsSnapshot struct {
	Login                 AuthEndpointMetricsSnapshot `json:"login"`
	Register              AuthEndpointMetricsSnapshot `json:"register"`
	RegistrationChallenge AuthEndpointMetricsSnapshot `json:"registration_challenge"`
	LoginFailures         LoginFailureMetricsSnapshot `json:"login_failures"`
}

// AuthProtectorConfig controls the in-memory protections for unauthenticated
// panel auth endpoints. Zero values are replaced with conservative defaults.
type AuthProtectorConfig struct {
	LoginIPRequestsPerMinute              int
	LoginIPBurst                          int
	RegisterIPRequestsPerMinute           int
	RegisterIPBurst                       int
	RegistrationProofDifficultyBits       int
	RegistrationProofValidity             time.Duration
	RegistrationProofMaxUsedChallenges    int
	LoginFailureThreshold                 int
	LoginFailureWindow                    time.Duration
	LoginBaseLockout                      time.Duration
	LoginMaxLockout                       time.Duration
	LoginIPMaximumEntries                 int
	RegisterIPMaximumEntries              int
	RegistrationChallengeIPMaximumEntries int
	LoginFailureMaximumEntries            int
	AuthEndpointFallbackBuckets           int
}

// AuthProtector adds request throttling, registration proof-of-work state, and
// short-term login-failure lockouts ahead of expensive bcrypt work.
type AuthProtector struct {
	mu sync.Mutex

	now              func() time.Time
	clientIPResolver *ratelimit.ClientIPResolver

	endpointStates   map[authEndpoint]*authEndpointState
	fallbackHashSeed maphash.Seed
	failures         map[string]*loginFailureEntry

	loginFailureThreshold             int
	loginFailureWindow                time.Duration
	loginBaseLockout                  time.Duration
	loginMaxLockout                   time.Duration
	loginFailureMaximumEntries        int
	loginFailureNextExpiryCheck       time.Time
	loginFailureExpiryCheckKnown      bool
	registrationProof                 *registrationProofState
	loginFailureCurrentEntries        atomic.Int64
	loginFailureAdmissions            atomic.Uint64
	loginFailureExpiredEntriesRemoved atomic.Uint64
	loginFailureCapacityRejections    atomic.Uint64
	loginFailureCleanupScans          atomic.Uint64

	lastCleanup time.Time
}

// NewAuthProtector creates an in-memory auth endpoint protector.
func NewAuthProtector(config AuthProtectorConfig) *AuthProtector {
	if config.LoginIPRequestsPerMinute <= 0 {
		config.LoginIPRequestsPerMinute = 30
	}
	if config.LoginIPBurst <= 0 {
		config.LoginIPBurst = config.LoginIPRequestsPerMinute
	}
	if config.RegisterIPRequestsPerMinute <= 0 {
		config.RegisterIPRequestsPerMinute = 10
	}
	if config.RegisterIPBurst <= 0 {
		config.RegisterIPBurst = config.RegisterIPRequestsPerMinute
	}
	if config.LoginFailureThreshold <= 0 {
		config.LoginFailureThreshold = 5
	}
	if config.LoginFailureWindow <= 0 {
		config.LoginFailureWindow = 15 * time.Minute
	}
	if config.LoginBaseLockout <= 0 {
		config.LoginBaseLockout = time.Minute
	}
	if config.LoginMaxLockout <= 0 {
		config.LoginMaxLockout = 15 * time.Minute
	}
	if config.LoginMaxLockout < config.LoginBaseLockout {
		config.LoginMaxLockout = config.LoginBaseLockout
	}
	config.LoginIPMaximumEntries = normalizeBoundedAuthCapacity(
		config.LoginIPMaximumEntries,
		defaultLoginIPMaximumEntries,
		maximumAuthEndpointEntries,
	)
	config.RegisterIPMaximumEntries = normalizeBoundedAuthCapacity(
		config.RegisterIPMaximumEntries,
		defaultRegisterIPMaximumEntries,
		maximumAuthEndpointEntries,
	)
	config.RegistrationChallengeIPMaximumEntries = normalizeBoundedAuthCapacity(
		config.RegistrationChallengeIPMaximumEntries,
		defaultRegistrationChallengeIPMaximumEntries,
		maximumAuthEndpointEntries,
	)
	config.LoginFailureMaximumEntries = normalizeBoundedAuthCapacity(
		config.LoginFailureMaximumEntries,
		defaultLoginFailureMaximumEntries,
		maximumLoginFailureEntries,
	)
	config.AuthEndpointFallbackBuckets = normalizeBoundedAuthCapacity(
		config.AuthEndpointFallbackBuckets,
		defaultAuthEndpointFallbackBuckets,
		maximumAuthEndpointFallbackBuckets,
	)

	now := time.Now
	protector := &AuthProtector{
		now:                        now,
		clientIPResolver:           ratelimit.NewClientIPResolver(),
		endpointStates:             make(map[authEndpoint]*authEndpointState, 3),
		fallbackHashSeed:           maphash.MakeSeed(),
		failures:                   make(map[string]*loginFailureEntry),
		loginFailureThreshold:      config.LoginFailureThreshold,
		loginFailureWindow:         config.LoginFailureWindow,
		loginBaseLockout:           config.LoginBaseLockout,
		loginMaxLockout:            config.LoginMaxLockout,
		loginFailureMaximumEntries: config.LoginFailureMaximumEntries,
		registrationProof: newRegistrationProofState(
			config.RegistrationProofDifficultyBits,
			config.RegistrationProofValidity,
			config.RegistrationProofMaxUsedChallenges,
			now(),
		),
		lastCleanup: now(),
	}
	protector.endpointStates[authEndpointLogin] = newAuthEndpointState(
		authEndpointLimit{requestsPerMinute: config.LoginIPRequestsPerMinute, burst: config.LoginIPBurst},
		config.LoginIPMaximumEntries,
		config.AuthEndpointFallbackBuckets,
	)
	protector.endpointStates[authEndpointRegister] = newAuthEndpointState(
		authEndpointLimit{requestsPerMinute: config.RegisterIPRequestsPerMinute, burst: config.RegisterIPBurst},
		config.RegisterIPMaximumEntries,
		config.AuthEndpointFallbackBuckets,
	)
	protector.endpointStates[authEndpointRegistrationChallenge] = newAuthEndpointState(
		authEndpointLimit{requestsPerMinute: config.RegisterIPRequestsPerMinute, burst: config.RegisterIPBurst},
		config.RegistrationChallengeIPMaximumEntries,
		config.AuthEndpointFallbackBuckets,
	)
	return protector
}

func normalizeBoundedAuthCapacity(value, defaultValue, maximumValue int) int {
	if value <= 0 {
		return defaultValue
	}
	if value > maximumValue {
		return maximumValue
	}
	return value
}

func newAuthEndpointState(limitConfig authEndpointLimit, maximumEntries, fallbackBucketCount int) *authEndpointState {
	state := &authEndpointState{
		limitConfig:      limitConfig,
		entries:          make(map[string]*authRateLimitEntry),
		fallbackLimiters: make([]*rate.Limiter, fallbackBucketCount),
		maximumEntries:   maximumEntries,
	}
	for fallbackIndex := range state.fallbackLimiters {
		state.fallbackLimiters[fallbackIndex] = newAuthRateLimiter(limitConfig)
	}
	return state
}

func (h *Handler) authProtector() *AuthProtector {
	if h.AuthProtector == nil {
		h.AuthProtector = NewAuthProtector(AuthProtectorConfig{})
	}
	return h.AuthProtector
}

// RateLimitAuthEndpoint applies an IP-scoped token bucket to public auth
// endpoints only when a valid forwarded client IP can be resolved.
func (p *AuthProtector) RateLimitAuthEndpoint(endpoint authEndpoint, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP, shouldApplyIPProtection, err := p.clientIPForProtection(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, ratelimit.ErrInvalidForwardedClientIPHeaders.Error())
			return
		}
		if !shouldApplyIPProtection {
			next.ServeHTTP(w, r)
			return
		}

		allowed, retryAfter := p.allowAuthRequest(endpoint, clientIP)
		if !allowed {
			writeRetryAfter(w, retryAfter)
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (p *AuthProtector) clientIP(request *http.Request) string {
	return p.clientIPResolver.Resolve(request)
}

func (p *AuthProtector) clientIPForProtection(request *http.Request) (string, bool, error) {
	clientAddress, err := p.clientIPResolver.ResolveAddress(request)
	if err != nil {
		return "", false, err
	}
	if !clientAddress.IsValid() {
		return "", false, nil
	}
	return clientAddress.String(), true, nil
}

func (p *AuthProtector) allowAuthRequest(endpoint authEndpoint, clientIP string) (bool, time.Duration) {
	now := p.now()
	p.mu.Lock()
	p.cleanupLocked(now)

	endpointState, exists := p.endpointStates[endpoint]
	if !exists || endpointState.limitConfig.requestsPerMinute <= 0 {
		p.mu.Unlock()
		return true, 0
	}

	entry, exists := endpointState.entries[clientIP]
	if exists {
		entry.lastSeen = now
		tokenBucket := entry.limiter
		p.mu.Unlock()
		return reserveAuthRequest(tokenBucket, now)
	}

	shouldCheckForExpiredEntries := endpointState.nextExpiryCheck.IsZero() || !now.Before(endpointState.nextExpiryCheck)
	if len(endpointState.entries) >= endpointState.maximumEntries && shouldCheckForExpiredEntries {
		p.cleanupEndpointLimitersLocked(endpointState, now)
	}
	if len(endpointState.entries) < endpointState.maximumEntries {
		entry = &authRateLimitEntry{
			limiter:  newAuthRateLimiter(endpointState.limitConfig),
			lastSeen: now,
		}
		endpointState.entries[clientIP] = entry
		entryExpiry := now.Add(authRateLimiterIdleTTL)
		if endpointState.nextExpiryCheck.IsZero() || entryExpiry.Before(endpointState.nextExpiryCheck) {
			endpointState.nextExpiryCheck = entryExpiry
		}
		endpointState.currentEntries.Add(1)
		endpointState.dedicatedAdmissions.Add(1)
		tokenBucket := entry.limiter
		p.mu.Unlock()
		return reserveAuthRequest(tokenBucket, now)
	}

	fallbackIndex := p.fallbackBucketIndexFor(clientIP, len(endpointState.fallbackLimiters))
	tokenBucket := endpointState.fallbackLimiters[fallbackIndex]
	endpointState.fallbackRequests.Add(1)
	p.mu.Unlock()

	allowed, retryAfter := reserveAuthRequest(tokenBucket, now)
	if !allowed {
		endpointState.fallbackRejections.Add(1)
	}
	return allowed, retryAfter
}

func (p *AuthProtector) fallbackBucketIndexFor(clientIP string, fallbackBucketCount int) int {
	return int(maphash.String(p.fallbackHashSeed, clientIP) % uint64(fallbackBucketCount))
}

func reserveAuthRequest(tokenBucket *rate.Limiter, now time.Time) (bool, time.Duration) {
	reservation := tokenBucket.ReserveN(now, 1)
	if !reservation.OK() {
		return false, time.Minute
	}
	delay := reservation.DelayFrom(now)
	if delay > 0 {
		reservation.CancelAt(now)
		return false, delay
	}
	return true, 0
}

func newAuthRateLimiter(limitConfig authEndpointLimit) *rate.Limiter {
	burst := limitConfig.burst
	if burst < 1 {
		burst = 1
	}
	requestsPerSecond := rate.Limit(limitConfig.requestsPerMinute) / rate.Limit(time.Minute/time.Second)
	return rate.NewLimiter(requestsPerSecond, burst)
}

type loginAttemptOutcome uint8

const (
	loginAttemptAbandoned loginAttemptOutcome = iota
	loginAttemptFailed
	loginAttemptSucceeded
)

type loginAttempt struct {
	protector    *AuthProtector
	failureKey   string
	entry        *loginFailureEntry
	completeOnce sync.Once
}

// beginLoginAttempt reserves bounded failure-tracking state before user lookup
// and bcrypt. A nil attempt means the pair is locked or the registry is full.
func (p *AuthProtector) beginLoginAttempt(username, clientIP string) (*loginAttempt, time.Duration) {
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cleanupLocked(now)

	failureKey := loginFailureKey(username, clientIP)
	entry := p.failures[failureKey]
	if entry != nil {
		if entry.lockedUntil.After(now) {
			return nil, entry.lockedUntil.Sub(now)
		}
		if entry.inFlightAttempts > 0 && entry.failureCount+entry.inFlightAttempts >= p.loginFailureThreshold {
			return nil, time.Minute
		}
		entry.inFlightAttempts++
		return &loginAttempt{protector: p, failureKey: failureKey, entry: entry}, 0
	}

	shouldCheckForExpiredEntries := p.loginFailureExpiryCheckKnown && !now.Before(p.loginFailureNextExpiryCheck)
	if len(p.failures) >= p.loginFailureMaximumEntries && shouldCheckForExpiredEntries {
		p.cleanupLoginFailuresLocked(now)
	}
	if len(p.failures) >= p.loginFailureMaximumEntries {
		p.loginFailureCapacityRejections.Add(1)
		return nil, time.Minute
	}

	entry = &loginFailureEntry{inFlightAttempts: 1}
	p.failures[failureKey] = entry
	p.loginFailureCurrentEntries.Add(1)
	p.loginFailureAdmissions.Add(1)
	return &loginAttempt{protector: p, failureKey: failureKey, entry: entry}, 0
}

func (attempt *loginAttempt) recordFailure() {
	if attempt != nil {
		attempt.complete(loginAttemptFailed)
	}
}

func (attempt *loginAttempt) recordSuccess() {
	if attempt != nil {
		attempt.complete(loginAttemptSucceeded)
	}
}

func (attempt *loginAttempt) abandon() {
	if attempt != nil {
		attempt.complete(loginAttemptAbandoned)
	}
}

func (attempt *loginAttempt) complete(outcome loginAttemptOutcome) {
	attempt.completeOnce.Do(func() {
		protector := attempt.protector
		now := protector.now()
		protector.mu.Lock()
		defer protector.mu.Unlock()

		entry := protector.failures[attempt.failureKey]
		if entry != attempt.entry || entry.inFlightAttempts <= 0 {
			return
		}
		entry.inFlightAttempts--

		switch outcome {
		case loginAttemptFailed:
			failureWindowExpired := !entry.lastFailure.IsZero() && !entry.lastFailure.Add(protector.loginFailureWindow).After(now)
			if failureWindowExpired {
				entry.failureCount = 0
				entry.lastFailure = time.Time{}
				entry.lockedUntil = time.Time{}
			}
			entry.failureCount++
			entry.lastFailure = now
			if entry.failureCount >= protector.loginFailureThreshold {
				entry.lockedUntil = now.Add(protector.lockoutDurationForFailures(entry.failureCount))
			}
		case loginAttemptSucceeded:
			entry.failureCount = 0
			entry.lastFailure = time.Time{}
			entry.lockedUntil = time.Time{}
		case loginAttemptAbandoned:
		}

		if entry.inFlightAttempts == 0 && entry.failureCount == 0 && !entry.lockedUntil.After(now) {
			delete(protector.failures, attempt.failureKey)
			protector.loginFailureCurrentEntries.Add(-1)
			return
		}
		if entry.inFlightAttempts == 0 {
			protector.recordLoginFailureExpiryLocked(entry)
		}
	})
}

func (p *AuthProtector) lockoutDurationForFailures(failureCount int) time.Duration {
	excessFailures := failureCount - p.loginFailureThreshold
	if excessFailures < 0 {
		excessFailures = 0
	}
	if excessFailures > 8 {
		excessFailures = 8
	}
	lockoutMultiplier := time.Duration(1 << excessFailures)
	if p.loginBaseLockout > p.loginMaxLockout/lockoutMultiplier {
		return p.loginMaxLockout
	}
	duration := p.loginBaseLockout * lockoutMultiplier
	return duration
}

func (p *AuthProtector) cleanupLocked(now time.Time) {
	if now.Sub(p.lastCleanup) < authProtectorCleanupInterval {
		return
	}
	p.lastCleanup = now

	for _, endpointState := range p.endpointStates {
		p.cleanupEndpointLimitersLocked(endpointState, now)
	}
	p.cleanupLoginFailuresLocked(now)
}

func (p *AuthProtector) cleanupEndpointLimitersLocked(endpointState *authEndpointState, now time.Time) {
	removedEntryCount := 0
	nextExpiryCheck := time.Time{}
	for clientIP, entry := range endpointState.entries {
		entryExpiry := entry.lastSeen.Add(authRateLimiterIdleTTL)
		if !entryExpiry.After(now) {
			delete(endpointState.entries, clientIP)
			removedEntryCount++
			continue
		}
		if nextExpiryCheck.IsZero() || entryExpiry.Before(nextExpiryCheck) {
			nextExpiryCheck = entryExpiry
		}
	}
	endpointState.nextExpiryCheck = nextExpiryCheck
	if removedEntryCount > 0 {
		endpointState.currentEntries.Add(-int64(removedEntryCount))
		endpointState.expiredEntriesRemoved.Add(uint64(removedEntryCount))
	}
}

func (p *AuthProtector) cleanupLoginFailuresLocked(now time.Time) {
	p.loginFailureCleanupScans.Add(1)
	removedEntryCount := 0
	nextExpiryCheck := time.Time{}
	expiryCheckKnown := false
	for key, entry := range p.failures {
		failureWindowExpired := entry.lastFailure.IsZero() || !entry.lastFailure.Add(p.loginFailureWindow).After(now)
		if entry.inFlightAttempts == 0 && failureWindowExpired && !entry.lockedUntil.After(now) {
			delete(p.failures, key)
			removedEntryCount++
			continue
		}
		if entry.inFlightAttempts == 0 {
			entryExpiry := loginFailureEntryExpiry(entry, p.loginFailureWindow)
			if !entryExpiry.IsZero() && (!expiryCheckKnown || entryExpiry.Before(nextExpiryCheck)) {
				nextExpiryCheck = entryExpiry
				expiryCheckKnown = true
			}
		}
	}
	p.loginFailureNextExpiryCheck = nextExpiryCheck
	p.loginFailureExpiryCheckKnown = expiryCheckKnown
	if removedEntryCount > 0 {
		p.loginFailureCurrentEntries.Add(-int64(removedEntryCount))
		p.loginFailureExpiredEntriesRemoved.Add(uint64(removedEntryCount))
	}
}

func (p *AuthProtector) recordLoginFailureExpiryLocked(entry *loginFailureEntry) {
	entryExpiry := loginFailureEntryExpiry(entry, p.loginFailureWindow)
	if entryExpiry.IsZero() {
		return
	}
	if !p.loginFailureExpiryCheckKnown || entryExpiry.Before(p.loginFailureNextExpiryCheck) {
		p.loginFailureNextExpiryCheck = entryExpiry
		p.loginFailureExpiryCheckKnown = true
	}
}

func loginFailureEntryExpiry(entry *loginFailureEntry, failureWindow time.Duration) time.Time {
	entryExpiry := time.Time{}
	if !entry.lastFailure.IsZero() {
		entryExpiry = entry.lastFailure.Add(failureWindow)
	}
	if entry.lockedUntil.After(entryExpiry) {
		entryExpiry = entry.lockedUntil
	}
	return entryExpiry
}

// Metrics returns a lock-free snapshot of bounded authentication state.
func (p *AuthProtector) Metrics() AuthProtectorMetricsSnapshot {
	if p == nil {
		return AuthProtectorMetricsSnapshot{}
	}
	return AuthProtectorMetricsSnapshot{
		Login:                 authEndpointMetricsSnapshot(p.endpointStates[authEndpointLogin]),
		Register:              authEndpointMetricsSnapshot(p.endpointStates[authEndpointRegister]),
		RegistrationChallenge: authEndpointMetricsSnapshot(p.endpointStates[authEndpointRegistrationChallenge]),
		LoginFailures: LoginFailureMetricsSnapshot{
			CurrentEntries:        p.loginFailureCurrentEntries.Load(),
			MaximumEntries:        int64(p.loginFailureMaximumEntries),
			Admissions:            p.loginFailureAdmissions.Load(),
			ExpiredEntriesRemoved: p.loginFailureExpiredEntriesRemoved.Load(),
			CapacityRejections:    p.loginFailureCapacityRejections.Load(),
		},
	}
}

func authEndpointMetricsSnapshot(endpointState *authEndpointState) AuthEndpointMetricsSnapshot {
	if endpointState == nil {
		return AuthEndpointMetricsSnapshot{}
	}
	return AuthEndpointMetricsSnapshot{
		CurrentEntries:        endpointState.currentEntries.Load(),
		MaximumEntries:        int64(endpointState.maximumEntries),
		FallbackBucketCount:   int64(len(endpointState.fallbackLimiters)),
		DedicatedAdmissions:   endpointState.dedicatedAdmissions.Load(),
		ExpiredEntriesRemoved: endpointState.expiredEntriesRemoved.Load(),
		FallbackRequests:      endpointState.fallbackRequests.Load(),
		FallbackRejections:    endpointState.fallbackRejections.Load(),
	}
}

func validatePanelAuthCredentials(rawUsername, password string) (string, error) {
	username := strings.TrimSpace(rawUsername)
	if len(username) < minPanelUsernameBytes || len(username) > maxPanelUsernameBytes {
		return "", errInvalidPanelAuthCredentials
	}
	if len(password) < minPanelPasswordBytes || len(password) > maxPanelPasswordBytes {
		return "", errInvalidPanelAuthCredentials
	}
	return username, nil
}

func loginFailureKey(username, clientIP string) string {
	return strings.ToLower(strings.TrimSpace(username)) + "\x00" + clientIP
}

func writeRetryAfter(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
}
