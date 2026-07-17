package http_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/panel"
)

func lowDifficultyAuthProtector() *panel.AuthProtector {
	return panel.NewAuthProtector(panel.AuthProtectorConfig{
		RegistrationProofDifficultyBits: 4,
	})
}

func buildRegistrationRequestBody(t *testing.T, serverURL, username, password, inviteCode string) []byte {
	t.Helper()
	challengeRequest, err := http.NewRequest(http.MethodPost, serverURL+"/panel/v1/auth/registration-challenge", nil)
	if err != nil {
		t.Fatal(err)
	}
	challengeResponse, err := http.DefaultClient.Do(challengeRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer challengeResponse.Body.Close()
	if challengeResponse.StatusCode != http.StatusOK {
		t.Fatalf("registration challenge status = %d, want %d", challengeResponse.StatusCode, http.StatusOK)
	}

	var challenge panel.RegistrationChallengeResponse
	if err := json.NewDecoder(challengeResponse.Body).Decode(&challenge); err != nil {
		t.Fatal(err)
	}

	var proof panel.RegistrationProof
	for nonce := uint64(0); ; nonce++ {
		digest := calculateRegistrationDigest(challenge.Challenge, username, inviteCode, nonce)
		if digestHasLeadingZeroBits(digest, challenge.Difficulty) {
			proof = panel.RegistrationProof{
				Challenge: challenge.Challenge,
				Nonce:     strconv.FormatUint(nonce, 10),
			}
			break
		}
	}

	requestBody, err := json.Marshal(panel.RegisterRequest{
		Username:   username,
		Password:   password,
		InviteCode: inviteCode,
		Proof:      proof,
	})
	if err != nil {
		t.Fatal(err)
	}
	return requestBody
}

func calculateRegistrationDigest(challenge, username, inviteCode string, nonce uint64) [sha256.Size]byte {
	message := bytes.NewBuffer(make([]byte, 0, len(challenge)+len(username)+len(inviteCode)+64))
	message.WriteString("grok-registration-pow-v1\x00")
	message.WriteString(challenge)
	message.WriteByte(0)
	message.WriteString(username)
	message.WriteByte(0)
	message.WriteString(inviteCode)
	message.WriteByte(0)
	_ = binary.Write(message, binary.BigEndian, nonce)
	return sha256.Sum256(message.Bytes())
}

func digestHasLeadingZeroBits(digest [sha256.Size]byte, difficultyBits int) bool {
	fullZeroBytes := difficultyBits / 8
	for byteIndex := 0; byteIndex < fullZeroBytes; byteIndex++ {
		if digest[byteIndex] != 0 {
			return false
		}
	}
	remainingBits := difficultyBits % 8
	return remainingBits == 0 || digest[fullZeroBytes]>>(8-remainingBits) == 0
}
