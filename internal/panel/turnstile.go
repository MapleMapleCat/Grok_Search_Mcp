package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	cloudflareTurnstileSiteverifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	turnstileLoginAction             = "login"
	maximumTurnstileTokenBytes       = 2048
	maximumTurnstileResponseBytes    = 64 << 10
	maximumConcurrentTurnstileChecks = 32
	turnstileRequiredErrorCode       = "turnstile_required"
	turnstileRejectedErrorCode       = "turnstile_rejected"
	turnstileUnavailableErrorCode    = "turnstile_unavailable"
	turnstileOverloadedErrorCode     = "turnstile_overloaded"
)

var (
	errTurnstileRejected   = errors.New("Turnstile verification rejected")
	errTurnstileOverloaded = errors.New("Turnstile verification capacity exhausted")
)

// TurnstileVerification contains the one-time token and request metadata sent
// to Cloudflare. The secret is supplied per request so panel setting changes
// take effect without recreating the verifier.
type TurnstileVerification struct {
	SecretKey      string
	Token          string
	RemoteIP       string
	ExpectedAction string
}

// TurnstileVerifier verifies a browser-issued Turnstile token.
type TurnstileVerifier interface {
	Verify(context.Context, TurnstileVerification) error
}

type cloudflareTurnstileVerifier struct {
	httpClient     *http.Client
	endpointURL    string
	admissionSlots chan struct{}
}

type cloudflareTurnstileResponse struct {
	Success    bool     `json:"success"`
	Action     string   `json:"action"`
	ErrorCodes []string `json:"error-codes"`
}

// NewCloudflareTurnstileVerifier creates a verifier with a bounded network
// timeout. Request cancellation from the login handler is also honored.
func NewCloudflareTurnstileVerifier() TurnstileVerifier {
	turnstileTransport := http.DefaultTransport.(*http.Transport).Clone()
	turnstileTransport.MaxConnsPerHost = maximumConcurrentTurnstileChecks
	turnstileTransport.MaxIdleConnsPerHost = maximumConcurrentTurnstileChecks
	turnstileTransport.ResponseHeaderTimeout = 8 * time.Second
	return newCloudflareTurnstileVerifier(
		&http.Client{
			Transport: turnstileTransport,
			Timeout:   8 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		cloudflareTurnstileSiteverifyURL,
		maximumConcurrentTurnstileChecks,
	)
}

func newCloudflareTurnstileVerifier(
	httpClient *http.Client,
	endpointURL string,
	maximumConcurrentChecks int,
) *cloudflareTurnstileVerifier {
	if maximumConcurrentChecks < 1 {
		maximumConcurrentChecks = 1
	}
	return &cloudflareTurnstileVerifier{
		httpClient:     httpClient,
		endpointURL:    endpointURL,
		admissionSlots: make(chan struct{}, maximumConcurrentChecks),
	}
}

func (verifier *cloudflareTurnstileVerifier) Verify(
	requestContext context.Context,
	verification TurnstileVerification,
) error {
	if err := requestContext.Err(); err != nil {
		return fmt.Errorf("Turnstile verification context: %w", err)
	}
	if verifier.admissionSlots != nil {
		select {
		case verifier.admissionSlots <- struct{}{}:
			defer func() { <-verifier.admissionSlots }()
		default:
			return errTurnstileOverloaded
		}
	}

	formValues := url.Values{
		"secret":   {verification.SecretKey},
		"response": {verification.Token},
	}
	if strings.TrimSpace(verification.RemoteIP) != "" {
		formValues.Set("remoteip", verification.RemoteIP)
	}

	request, err := http.NewRequestWithContext(
		requestContext,
		http.MethodPost,
		verifier.endpointURL,
		strings.NewReader(formValues.Encode()),
	)
	if err != nil {
		return fmt.Errorf("create Turnstile verification request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := verifier.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send Turnstile verification request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumTurnstileResponseBytes))
		return fmt.Errorf("Turnstile verification returned HTTP %d", response.StatusCode)
	}

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maximumTurnstileResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read Turnstile verification response: %w", err)
	}
	if len(responseBody) > maximumTurnstileResponseBytes {
		return fmt.Errorf("Turnstile verification response exceeded size limit")
	}
	var verificationResponse cloudflareTurnstileResponse
	if err := json.Unmarshal(responseBody, &verificationResponse); err != nil {
		return fmt.Errorf("decode Turnstile verification response: %w", err)
	}
	if !verificationResponse.Success {
		return classifyTurnstileFailure(verificationResponse.ErrorCodes)
	}
	if verification.ExpectedAction != "" && verificationResponse.Action != verification.ExpectedAction {
		return errTurnstileRejected
	}
	return nil
}

func classifyTurnstileFailure(errorCodes []string) error {
	if len(errorCodes) == 0 {
		return fmt.Errorf("Turnstile verification failed without an error code")
	}

	normalizedErrorCodes := make([]string, 0, len(errorCodes))
	onlyClientTokenErrors := true
	for _, errorCode := range errorCodes {
		normalizedErrorCode := strings.ToLower(strings.TrimSpace(errorCode))
		if normalizedErrorCode == "" {
			continue
		}
		normalizedErrorCodes = append(normalizedErrorCodes, normalizedErrorCode)
		switch normalizedErrorCode {
		case "missing-input-response", "invalid-input-response", "timeout-or-duplicate":
		default:
			onlyClientTokenErrors = false
		}
	}
	if len(normalizedErrorCodes) == 0 {
		return fmt.Errorf("Turnstile verification failed without a usable error code")
	}

	joinedErrorCodes := strings.Join(normalizedErrorCodes, ",")
	if onlyClientTokenErrors {
		return fmt.Errorf("%w: %s", errTurnstileRejected, joinedErrorCodes)
	}
	return fmt.Errorf("Turnstile verification service error: %s", joinedErrorCodes)
}
