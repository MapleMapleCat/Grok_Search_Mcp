package panel

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultRegistrationProofDifficultyBits = 20
	defaultRegistrationProofValidity       = 5 * time.Minute
	registrationProofVersion               = "v1"
	registrationProofNonceBytes            = 24
	maxRegistrationProofNonceDigits        = 20
	registrationProofPrefix                = "grok-registration-pow-v1\x00"
)

var (
	errRegistrationProofRequired = errors.New("registration proof is required")
	errRegistrationProofInvalid  = errors.New("registration proof is invalid")
	errRegistrationProofExpired  = errors.New("registration proof expired")
	errRegistrationProofReplayed = errors.New("registration proof already used")
)

// RegistrationProof contains the server-issued challenge and the client-found
// counter. A challenge is single-use after a valid counter is submitted.
type RegistrationProof struct {
	Challenge string `json:"challenge"`
	Nonce     string `json:"nonce"`
}

type registrationProofChallenge struct {
	ExpiresAt      time.Time
	DifficultyBits int
}

// registrationProofState is deliberately kept in AuthProtector so the proof
// uses the same process-local lifecycle as the unauthenticated auth limits.
// The signed challenge itself is stateless; only successful proofs are kept
// here to prevent replay and to avoid memory growth from challenge requests.
type registrationProofState struct {
	mu                 sync.Mutex
	secret             []byte
	difficultyBits     int
	validity           time.Duration
	usedChallenges     map[[sha256.Size]byte]time.Time
	lastProofCleanupAt time.Time
}

func newRegistrationProofState(difficultyBits int, validity time.Duration, now time.Time) *registrationProofState {
	if difficultyBits <= 0 {
		difficultyBits = defaultRegistrationProofDifficultyBits
	}
	if difficultyBits > 32 {
		difficultyBits = 32
	}
	if validity <= 0 {
		validity = defaultRegistrationProofValidity
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		panic(fmt.Sprintf("generate registration proof secret: %v", err))
	}
	return &registrationProofState{
		secret:             secret,
		difficultyBits:     difficultyBits,
		validity:           validity,
		usedChallenges:     make(map[[sha256.Size]byte]time.Time),
		lastProofCleanupAt: now,
	}
}

func (p *registrationProofState) issue(now time.Time) (RegistrationChallengeResponse, error) {
	randomChallenge := make([]byte, registrationProofNonceBytes)
	if _, err := rand.Read(randomChallenge); err != nil {
		return RegistrationChallengeResponse{}, fmt.Errorf("generate registration challenge: %w", err)
	}

	expiresAt := now.Add(p.validity).UTC()
	payload := strings.Join([]string{
		registrationProofVersion,
		strconv.FormatInt(expiresAt.Unix(), 10),
		strconv.Itoa(p.difficultyBits),
		base64.RawURLEncoding.EncodeToString(randomChallenge),
	}, ".")
	signature := p.sign([]byte(payload))
	return RegistrationChallengeResponse{
		Challenge:  payload + "." + base64.RawURLEncoding.EncodeToString(signature),
		Difficulty: p.difficultyBits,
		ExpiresAt:  expiresAt,
	}, nil
}

func (p *registrationProofState) verifyAndConsume(now time.Time, proof RegistrationProof, username, inviteCode string) error {
	if strings.TrimSpace(proof.Challenge) == "" || strings.TrimSpace(proof.Nonce) == "" {
		return errRegistrationProofRequired
	}

	challenge, payload, err := p.parseAndVerifyChallenge(proof.Challenge)
	if err != nil {
		return errRegistrationProofInvalid
	}
	if !challenge.ExpiresAt.After(now) {
		return errRegistrationProofExpired
	}
	nonceValue, err := parseRegistrationProofNonce(proof.Nonce)
	if err != nil {
		return errRegistrationProofInvalid
	}

	digest := calculateRegistrationProofDigest(proof.Challenge, username, inviteCode, nonceValue)
	if !hasLeadingZeroBits(digest, challenge.DifficultyBits) {
		return errRegistrationProofInvalid
	}

	challengeKey := sha256.Sum256([]byte(payload))
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cleanupUsedChallengesLocked(now)
	if _, alreadyUsed := p.usedChallenges[challengeKey]; alreadyUsed {
		return errRegistrationProofReplayed
	}
	p.usedChallenges[challengeKey] = challenge.ExpiresAt
	return nil
}

func (p *registrationProofState) parseAndVerifyChallenge(token string) (registrationProofChallenge, string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 5 || parts[0] != registrationProofVersion {
		return registrationProofChallenge{}, "", errRegistrationProofInvalid
	}
	expiresUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return registrationProofChallenge{}, "", errRegistrationProofInvalid
	}
	difficultyBits, err := strconv.Atoi(parts[2])
	if err != nil || difficultyBits < 1 || difficultyBits > 32 {
		return registrationProofChallenge{}, "", errRegistrationProofInvalid
	}
	if _, err := base64.RawURLEncoding.DecodeString(parts[3]); err != nil {
		return registrationProofChallenge{}, "", errRegistrationProofInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil {
		return registrationProofChallenge{}, "", errRegistrationProofInvalid
	}
	payload := strings.Join(parts[:4], ".")
	if !hmac.Equal(signature, p.sign([]byte(payload))) {
		return registrationProofChallenge{}, "", errRegistrationProofInvalid
	}
	return registrationProofChallenge{
		ExpiresAt:      time.Unix(expiresUnix, 0).UTC(),
		DifficultyBits: difficultyBits,
	}, payload, nil
}

func (p *registrationProofState) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, p.secret)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func (p *registrationProofState) cleanupUsedChallengesLocked(now time.Time) {
	if now.Sub(p.lastProofCleanupAt) < time.Minute {
		return
	}
	p.lastProofCleanupAt = now
	for challengeKey, expiresAt := range p.usedChallenges {
		if !expiresAt.After(now) {
			delete(p.usedChallenges, challengeKey)
		}
	}
}

func parseRegistrationProofNonce(rawNonce string) (uint64, error) {
	nonce := strings.TrimSpace(rawNonce)
	if len(nonce) == 0 || len(nonce) > maxRegistrationProofNonceDigits {
		return 0, errRegistrationProofInvalid
	}
	for _, character := range nonce {
		if character < '0' || character > '9' {
			return 0, errRegistrationProofInvalid
		}
	}
	return strconv.ParseUint(nonce, 10, 64)
}

func calculateRegistrationProofDigest(challenge, username, inviteCode string, nonce uint64) [sha256.Size]byte {
	message := bytes.NewBuffer(make([]byte, 0, len(registrationProofPrefix)+len(challenge)+len(username)+len(inviteCode)+11))
	message.WriteString(registrationProofPrefix)
	message.WriteString(challenge)
	message.WriteByte(0)
	message.WriteString(username)
	message.WriteByte(0)
	message.WriteString(inviteCode)
	message.WriteByte(0)
	var nonceBytes [8]byte
	binary.BigEndian.PutUint64(nonceBytes[:], nonce)
	message.Write(nonceBytes[:])
	return sha256.Sum256(message.Bytes())
}

func hasLeadingZeroBits(digest [sha256.Size]byte, difficultyBits int) bool {
	fullZeroBytes := difficultyBits / 8
	for byteIndex := 0; byteIndex < fullZeroBytes; byteIndex++ {
		if digest[byteIndex] != 0 {
			return false
		}
	}
	remainingBits := difficultyBits % 8
	if remainingBits == 0 {
		return true
	}
	return digest[fullZeroBytes]>>(8-remainingBits) == 0
}
