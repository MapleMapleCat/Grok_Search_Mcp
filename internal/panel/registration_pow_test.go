package panel

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestRegistrationProofVerifiesOnceAndBindsRegistrationData(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	proofState := newRegistrationProofState(4, time.Minute, now)
	challenge, err := proofState.issue(now)
	if err != nil {
		t.Fatal(err)
	}

	proof := solveBoundRegistrationProofForStateTest(challenge)
	if err := proofState.verifyAndConsume(now, proof, "alice", "different-invite-code"); !errors.Is(err, errRegistrationProofInvalid) {
		t.Fatalf("proof with changed invite code error = %v, want %v", err, errRegistrationProofInvalid)
	}
	if err := proofState.verifyAndConsume(now, proof, "different-user", "invite-code"); !errors.Is(err, errRegistrationProofInvalid) {
		t.Fatalf("proof with changed username error = %v, want %v", err, errRegistrationProofInvalid)
	}
	if err := proofState.verifyAndConsume(now, proof, "alice", "invite-code"); err != nil {
		t.Fatalf("valid proof error = %v", err)
	}
	if err := proofState.verifyAndConsume(now, proof, "alice", "invite-code"); !errors.Is(err, errRegistrationProofReplayed) {
		t.Fatalf("replayed proof error = %v, want %v", err, errRegistrationProofReplayed)
	}
}

func solveBoundRegistrationProofForStateTest(challenge RegistrationChallengeResponse) RegistrationProof {
	for nonce := uint64(0); ; nonce++ {
		validDigest := calculateRegistrationProofDigest(challenge.Challenge, "alice", "invite-code", nonce)
		changedInviteDigest := calculateRegistrationProofDigest(challenge.Challenge, "alice", "different-invite-code", nonce)
		changedUsernameDigest := calculateRegistrationProofDigest(challenge.Challenge, "different-user", "invite-code", nonce)
		if hasLeadingZeroBits(validDigest, challenge.Difficulty) &&
			!hasLeadingZeroBits(changedInviteDigest, challenge.Difficulty) &&
			!hasLeadingZeroBits(changedUsernameDigest, challenge.Difficulty) {
			return RegistrationProof{
				Challenge: challenge.Challenge,
				Nonce:     strconv.FormatUint(nonce, 10),
			}
		}
	}
}

func TestRegistrationProofRejectsMissingTamperedAndExpiredChallenges(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	proofState := newRegistrationProofState(4, time.Minute, now)

	if err := proofState.verifyAndConsume(now, RegistrationProof{}, "alice", ""); !errors.Is(err, errRegistrationProofRequired) {
		t.Fatalf("missing proof error = %v, want %v", err, errRegistrationProofRequired)
	}

	challenge, err := proofState.issue(now)
	if err != nil {
		t.Fatal(err)
	}
	proof := solveRegistrationProofForStateTest(challenge, "alice", "")
	proof.Challenge += "tampered"
	if err := proofState.verifyAndConsume(now, proof, "alice", ""); !errors.Is(err, errRegistrationProofInvalid) {
		t.Fatalf("tampered challenge error = %v, want %v", err, errRegistrationProofInvalid)
	}

	expiringState := newRegistrationProofState(4, time.Second, now)
	expiringChallenge, err := expiringState.issue(now)
	if err != nil {
		t.Fatal(err)
	}
	expiredProof := solveRegistrationProofForStateTest(expiringChallenge, "alice", "")
	if err := expiringState.verifyAndConsume(now.Add(2*time.Second), expiredProof, "alice", ""); !errors.Is(err, errRegistrationProofExpired) {
		t.Fatalf("expired proof error = %v, want %v", err, errRegistrationProofExpired)
	}
}

func solveRegistrationProofForStateTest(challenge RegistrationChallengeResponse, username, inviteCode string) RegistrationProof {
	for nonce := uint64(0); ; nonce++ {
		digest := calculateRegistrationProofDigest(challenge.Challenge, username, inviteCode, nonce)
		if hasLeadingZeroBits(digest, challenge.Difficulty) {
			return RegistrationProof{
				Challenge: challenge.Challenge,
				Nonce:     strconv.FormatUint(nonce, 10),
			}
		}
	}
}
