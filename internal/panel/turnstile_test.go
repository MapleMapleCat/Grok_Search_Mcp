package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/config"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/store"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/testsupport"
)

type recordingTurnstileVerifier struct {
	verification      TurnstileVerification
	verificationError error
	callCount         int
}

func (verifier *recordingTurnstileVerifier) Verify(
	_ context.Context,
	verification TurnstileVerification,
) error {
	verifier.callCount += 1
	verifier.verification = verification
	return verifier.verificationError
}

type turnstileLoginStore struct {
	testsupport.Store
	serverSettings  config.ServerSettings
	userLookupCount int
}

type staticLoginVerificationSettingsProvider struct {
	settings LoginVerificationSettings
}

func (provider staticLoginVerificationSettingsProvider) LiveLoginVerificationSettings() LoginVerificationSettings {
	return provider.settings
}

func (testStore *turnstileLoginStore) GetServerSettings(context.Context) (*store.ServerSettings, error) {
	return &store.ServerSettings{Runtime: testStore.serverSettings}, nil
}

func (testStore *turnstileLoginStore) GetUserByUsername(context.Context, string) (*store.User, error) {
	testStore.userLookupCount += 1
	return nil, nil
}

func TestCloudflareTurnstileVerifierSendsLoginVerification(t *testing.T) {
	receivedSecret := ""
	receivedToken := ""
	receivedRemoteIP := ""
	responseAction := turnstileLoginAction
	responseSuccess := true
	var responseErrorCodes []string
	verificationServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("siteverify method = %s, want POST", request.Method)
		}
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse siteverify form: %v", err)
		}
		receivedSecret = request.Form.Get("secret")
		receivedToken = request.Form.Get("response")
		receivedRemoteIP = request.Form.Get("remoteip")
		writeJSON(writer, http.StatusOK, map[string]any{
			"success":     responseSuccess,
			"action":      responseAction,
			"error-codes": responseErrorCodes,
		})
	}))
	defer verificationServer.Close()

	verifier := &cloudflareTurnstileVerifier{
		httpClient:  verificationServer.Client(),
		endpointURL: verificationServer.URL,
	}
	verification := TurnstileVerification{
		SecretKey:      "private-secret-key",
		Token:          "one-time-token",
		RemoteIP:       "192.0.2.25",
		ExpectedAction: turnstileLoginAction,
	}
	if err := verifier.Verify(context.Background(), verification); err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if receivedSecret != verification.SecretKey || receivedToken != verification.Token || receivedRemoteIP != verification.RemoteIP {
		t.Fatalf(
			"siteverify form = secret %q, token %q, remote IP %q",
			receivedSecret,
			receivedToken,
			receivedRemoteIP,
		)
	}

	responseAction = "register"
	if err := verifier.Verify(context.Background(), verification); !errors.Is(err, errTurnstileRejected) {
		t.Fatalf("action mismatch error = %v, want rejected", err)
	}

	responseAction = turnstileLoginAction
	responseSuccess = false
	responseErrorCodes = []string{"invalid-input-response"}
	if err := verifier.Verify(context.Background(), verification); !errors.Is(err, errTurnstileRejected) {
		t.Fatalf("invalid token error = %v, want rejected", err)
	}
	responseErrorCodes = []string{"invalid-input-secret"}
	if err := verifier.Verify(context.Background(), verification); err == nil || errors.Is(err, errTurnstileRejected) {
		t.Fatalf("invalid secret error = %v, want service failure", err)
	}
}

func TestCloudflareTurnstileVerifierRejectsWhenConcurrencyIsSaturated(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	requestReleased := false
	verificationServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
		writeJSON(writer, http.StatusOK, map[string]any{"success": true, "action": turnstileLoginAction})
	}))
	defer verificationServer.Close()
	defer func() {
		if !requestReleased {
			close(releaseRequest)
		}
	}()

	verifier := newCloudflareTurnstileVerifier(verificationServer.Client(), verificationServer.URL, 1)
	verification := TurnstileVerification{
		SecretKey:      "private-secret-key",
		Token:          "one-time-token",
		ExpectedAction: turnstileLoginAction,
	}
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- verifier.Verify(context.Background(), verification)
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("first Turnstile verification did not reach the server")
	}
	if err := verifier.Verify(context.Background(), verification); !errors.Is(err, errTurnstileOverloaded) {
		t.Fatalf("saturated verification error = %v, want overloaded", err)
	}
	close(releaseRequest)
	requestReleased = true
	select {
	case err := <-firstResult:
		if err != nil {
			t.Fatalf("admitted verification failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("admitted Turnstile verification did not complete")
	}
}

func TestLoginTurnstileRejectsBeforeUserLookup(t *testing.T) {
	testCases := []struct {
		name                  string
		token                 string
		verificationError     error
		expectedStatus        int
		expectedVerifierCalls int
		expectsRetryAfter     bool
		expectedErrorCode     string
	}{
		{
			name:              "missing token",
			expectedStatus:    http.StatusBadRequest,
			expectedErrorCode: turnstileRequiredErrorCode,
		},
		{
			name:                  "rejected token",
			token:                 "rejected-token",
			verificationError:     errTurnstileRejected,
			expectedStatus:        http.StatusBadRequest,
			expectedVerifierCalls: 1,
			expectedErrorCode:     turnstileRejectedErrorCode,
		},
		{
			name:                  "verification unavailable",
			token:                 "unavailable-token",
			verificationError:     errors.New("network unavailable"),
			expectedStatus:        http.StatusServiceUnavailable,
			expectedVerifierCalls: 1,
			expectsRetryAfter:     true,
			expectedErrorCode:     turnstileUnavailableErrorCode,
		},
		{
			name:                  "verification overloaded",
			token:                 "overloaded-token",
			verificationError:     errTurnstileOverloaded,
			expectedStatus:        http.StatusServiceUnavailable,
			expectedVerifierCalls: 1,
			expectsRetryAfter:     true,
			expectedErrorCode:     turnstileOverloadedErrorCode,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testStore := &turnstileLoginStore{serverSettings: config.ServerSettings{
				RegistrationMode:   store.RegistrationModeDisabled,
				TurnstileEnabled:   true,
				TurnstileSiteKey:   "public-site-key",
				TurnstileSecretKey: "private-secret-key",
			}}
			verifier := &recordingTurnstileVerifier{verificationError: testCase.verificationError}
			handler := &Handler{
				Store:             testStore,
				AuthProtector:     newTestAuthProtector(),
				TurnstileVerifier: verifier,
			}
			requestBody, err := json.Marshal(LoginRequest{
				Username:       "turnstile-user",
				Password:       "password123",
				TurnstileToken: testCase.token,
			})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/panel/v1/auth/login", bytes.NewReader(requestBody))
			request.RemoteAddr = "192.0.2.25:43120"
			responseRecorder := httptest.NewRecorder()

			handler.login(responseRecorder, request)

			if responseRecorder.Code != testCase.expectedStatus {
				t.Fatalf("login status = %d, body = %s", responseRecorder.Code, responseRecorder.Body.String())
			}
			var responseError errorResponse
			if err := json.NewDecoder(responseRecorder.Body).Decode(&responseError); err != nil {
				t.Fatalf("decode login error response: %v", err)
			}
			if responseError.Code != testCase.expectedErrorCode {
				t.Fatalf("login error code = %q, want %q", responseError.Code, testCase.expectedErrorCode)
			}
			if verifier.callCount != testCase.expectedVerifierCalls {
				t.Fatalf("verifier calls = %d, want %d", verifier.callCount, testCase.expectedVerifierCalls)
			}
			if testStore.userLookupCount != 0 {
				t.Fatalf("user lookup count = %d, want 0 before Turnstile passes", testStore.userLookupCount)
			}
			if testCase.expectedVerifierCalls > 0 {
				if verifier.verification.SecretKey != "private-secret-key" || verifier.verification.Token != testCase.token {
					t.Fatalf("verifier received unexpected credentials: %+v", verifier.verification)
				}
				if verifier.verification.RemoteIP != "192.0.2.25" || verifier.verification.ExpectedAction != turnstileLoginAction {
					t.Fatalf("verifier received unexpected metadata: %+v", verifier.verification)
				}
			}
			if testCase.expectsRetryAfter && responseRecorder.Header().Get("Retry-After") == "" {
				t.Fatal("verification availability failure omitted Retry-After")
			}
		})
	}
}

func TestPublicAuthenticationSettingsNeverExposeTurnstileSecret(t *testing.T) {
	const secretKey = "public-endpoint-must-not-return-this-secret"
	testStore := &turnstileLoginStore{serverSettings: config.ServerSettings{
		RegistrationMode:   store.RegistrationModeFree,
		TurnstileEnabled:   true,
		TurnstileSiteKey:   "public-site-key",
		TurnstileSecretKey: secretKey,
	}}
	handler := &Handler{Store: testStore}
	request := httptest.NewRequest(http.MethodGet, "/panel/v1/auth/registration-settings", nil)
	responseRecorder := httptest.NewRecorder()

	handler.registrationSettings(responseRecorder, request)

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("registration settings status = %d", responseRecorder.Code)
	}
	responseBody := responseRecorder.Body.String()
	if strings.Contains(responseBody, secretKey) || strings.Contains(responseBody, "turnstile_secret_key") {
		t.Fatalf("public authentication settings exposed Turnstile secret: %s", responseBody)
	}
	if !strings.Contains(responseBody, `"turnstile_site_key":"public-site-key"`) {
		t.Fatalf("public authentication settings omitted site key: %s", responseBody)
	}
}

func TestPublicAuthenticationSettingsUseLastAppliedTurnstileRevision(t *testing.T) {
	testStore := &turnstileLoginStore{serverSettings: config.ServerSettings{
		RegistrationMode:   store.RegistrationModeFree,
		TurnstileEnabled:   true,
		TurnstileSiteKey:   "persisted-unapplied-site-key",
		TurnstileSecretKey: "persisted-unapplied-secret-key",
	}}
	handler := &Handler{
		Store: testStore,
		LoginVerificationSettingsProvider: staticLoginVerificationSettingsProvider{settings: LoginVerificationSettings{
			TurnstileEnabled:   true,
			TurnstileSiteKey:   "confirmed-live-site-key",
			TurnstileSecretKey: "confirmed-live-secret-key",
		}},
	}
	request := httptest.NewRequest(http.MethodGet, "/panel/v1/auth/registration-settings", nil)
	responseRecorder := httptest.NewRecorder()

	handler.registrationSettings(responseRecorder, request)

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("registration settings status = %d", responseRecorder.Code)
	}
	var response RegistrationSettingsResponse
	if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.RegistrationMode != store.RegistrationModeFree {
		t.Fatalf("registration mode = %q, want persisted free mode", response.RegistrationMode)
	}
	if !response.TurnstileEnabled || response.TurnstileSiteKey != "confirmed-live-site-key" {
		t.Fatalf("public Turnstile settings = %+v, want confirmed live settings", response)
	}
}
