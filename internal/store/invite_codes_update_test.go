package store

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestUpdateInviteCodeRejectsLimitBelowCurrentRegistrationCount(t *testing.T) {
	sqliteStore := openTestDB(t)
	requestContext := context.Background()

	administrator, err := sqliteStore.CreateUser(requestContext, "invite-limit-admin", "hash", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	inviteCode, rawInviteCode, err := sqliteStore.CreateInviteCode(requestContext, administrator.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, username := range []string{"invite-limit-user-one", "invite-limit-user-two"} {
		if _, err := sqliteStore.RegisterUserWithCurrentMode(
			requestContext,
			username,
			"password-hash",
			rawInviteCode,
			RegistrationModeInvite,
		); err != nil {
			t.Fatalf("register %q: %v", username, err)
		}
	}

	lowerRegistrationLimit := 1
	if _, err := sqliteStore.UpdateInviteCode(requestContext, inviteCode.ID, InviteCodeUpdates{
		RegistrationLimit: &lowerRegistrationLimit,
	}); !errors.Is(err, ErrInviteCodeLimitTooLow) {
		t.Fatalf("UpdateInviteCode error = %v, want %v", err, ErrInviteCodeLimitTooLow)
	}

	persistedInviteCode, err := sqliteStore.getInviteCodeByID(requestContext, inviteCode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedInviteCode.RegistrationCount != 2 || persistedInviteCode.RegistrationLimit != 3 {
		t.Fatalf(
			"persisted invite usage = %d/%d, want 2/3",
			persistedInviteCode.RegistrationCount,
			persistedInviteCode.RegistrationLimit,
		)
	}
}

func TestUpdateInviteCodeEnabledOnlyMapsMissingInviteCode(t *testing.T) {
	sqliteStore := openTestDB(t)
	enabled := false

	_, err := sqliteStore.UpdateInviteCode(context.Background(), "missing-invite-code", InviteCodeUpdates{
		Enabled: &enabled,
	})
	if !errors.Is(err, ErrInviteCodeNotFound) {
		t.Fatalf("UpdateInviteCode error = %v, want %v", err, ErrInviteCodeNotFound)
	}
}

func TestUpdateInviteCodeClassifiesConcurrentRegistrationLimitRace(t *testing.T) {
	const encryptionSecret = "invite-update-race-encryption-secret-at-least-32-bytes"
	databasePath := filepath.Join(t.TempDir(), "invite-update-race.db")
	requestContext := context.Background()

	updateStore, err := OpenSQLite(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = updateStore.Close() })
	if err := updateStore.ConfigureAPIKeyEncryption(encryptionSecret); err != nil {
		t.Fatal(err)
	}

	registrationStore, err := OpenSQLite(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registrationStore.Close() })
	if err := registrationStore.ConfigureAPIKeyEncryption(encryptionSecret); err != nil {
		t.Fatal(err)
	}

	administrator, err := updateStore.CreateUser(requestContext, "invite-race-admin", "hash", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	inviteCode, rawInviteCode, err := updateStore.CreateInviteCode(requestContext, administrator.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updateStore.RegisterUserWithCurrentMode(
		requestContext,
		"invite-race-first-user",
		"password-hash",
		rawInviteCode,
		RegistrationModeInvite,
	); err != nil {
		t.Fatal(err)
	}

	blockedWriterConnection, err := updateStore.db.Conn(requestContext)
	if err != nil {
		t.Fatal(err)
	}
	writerConnectionReleased := false
	t.Cleanup(func() {
		if !writerConnectionReleased {
			_ = blockedWriterConnection.Close()
		}
	})

	initialWaitCount := updateStore.db.Stats().WaitCount
	lowerRegistrationLimit := 1
	updateErrorChannel := make(chan error, 1)
	go func() {
		_, updateErr := updateStore.UpdateInviteCode(requestContext, inviteCode.ID, InviteCodeUpdates{
			RegistrationLimit: &lowerRegistrationLimit,
		})
		updateErrorChannel <- updateErr
	}()

	waitDeadline := time.Now().Add(2 * time.Second)
	for updateStore.db.Stats().WaitCount == initialWaitCount {
		select {
		case updateErr := <-updateErrorChannel:
			t.Fatalf("UpdateInviteCode returned before waiting for the held writer connection: %v", updateErr)
		default:
		}
		if time.Now().After(waitDeadline) {
			t.Fatal("UpdateInviteCode did not wait for the held writer connection")
		}
		runtime.Gosched()
	}

	if _, err := registrationStore.RegisterUserWithCurrentMode(
		requestContext,
		"invite-race-second-user",
		"password-hash",
		rawInviteCode,
		RegistrationModeInvite,
	); err != nil {
		t.Fatalf("concurrent invite registration: %v", err)
	}
	if err := blockedWriterConnection.Close(); err != nil {
		t.Fatal(err)
	}
	writerConnectionReleased = true

	var updateErr error
	select {
	case updateErr = <-updateErrorChannel:
	case <-time.After(2 * time.Second):
		t.Fatal("UpdateInviteCode did not finish after releasing the writer connection")
	}
	if !errors.Is(updateErr, ErrInviteCodeLimitTooLow) {
		t.Fatalf("concurrent UpdateInviteCode error = %v, want %v", updateErr, ErrInviteCodeLimitTooLow)
	}
	if strings.Contains(strings.ToUpper(updateErr.Error()), "CHECK") {
		t.Fatalf("concurrent UpdateInviteCode leaked SQLite CHECK error: %v", updateErr)
	}

	persistedInviteCode, err := updateStore.getInviteCodeByID(requestContext, inviteCode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedInviteCode.RegistrationCount != 2 || persistedInviteCode.RegistrationLimit != 3 {
		t.Fatalf(
			"persisted invite usage = %d/%d, want 2/3",
			persistedInviteCode.RegistrationCount,
			persistedInviteCode.RegistrationLimit,
		)
	}
}
