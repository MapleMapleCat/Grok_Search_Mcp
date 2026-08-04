package store

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestWriterCreatesReturnModelsWhileReadPoolIsOccupied(t *testing.T) {
	sqliteStore := openTestDB(t)
	setupContext := context.Background()
	keyOwner, err := sqliteStore.CreateUser(setupContext, "writer-return-owner", "password-hash", RoleUser)
	if err != nil {
		t.Fatal(err)
	}

	occupiedReadConnections := make([]*sql.Conn, 0, sqliteReadPoolSize)
	defer func() {
		for _, readConnection := range occupiedReadConnections {
			_ = readConnection.Close()
		}
	}()
	for connectionIndex := 0; connectionIndex < sqliteReadPoolSize; connectionIndex++ {
		readConnection, connectionErr := sqliteStore.readDB.Conn(setupContext)
		if connectionErr != nil {
			t.Fatalf("occupy read connection %d: %v", connectionIndex, connectionErr)
		}
		occupiedReadConnections = append(occupiedReadConnections, readConnection)
	}

	createKeyContext, cancelCreateKey := context.WithTimeout(setupContext, 2*time.Second)
	createdAPIKey, rawAPIKey, err := sqliteStore.CreateKey(createKeyContext, keyOwner.ID, "writer-return-key", 20)
	cancelCreateKey()
	if err != nil {
		t.Fatalf("CreateKey depended on the occupied read pool: %v", err)
	}
	if createdAPIKey == nil || createdAPIKey.UserID != keyOwner.ID || createdAPIKey.Name != "writer-return-key" {
		t.Fatalf("CreateKey returned unexpected model: %+v", createdAPIKey)
	}
	if rawAPIKey == "" {
		t.Fatal("CreateKey returned an empty raw key")
	}

	registrationContext, cancelRegistration := context.WithTimeout(setupContext, 2*time.Second)
	registeredUser, err := sqliteStore.RegisterUserWithCurrentMode(
		registrationContext,
		"writer-return-registration",
		"password-hash",
		"",
		RegistrationModeFree,
	)
	cancelRegistration()
	if err != nil {
		t.Fatalf("RegisterUserWithCurrentMode depended on the occupied read pool: %v", err)
	}
	if registeredUser == nil || registeredUser.Username != "writer-return-registration" {
		t.Fatalf("RegisterUserWithCurrentMode returned unexpected model: %+v", registeredUser)
	}

	for _, readConnection := range occupiedReadConnections {
		if err := readConnection.Close(); err != nil {
			t.Fatalf("release occupied read connection: %v", err)
		}
	}
	occupiedReadConnections = nil

	storedAPIKey, err := sqliteStore.GetKeyByID(setupContext, createdAPIKey.ID)
	if err != nil {
		t.Fatalf("read committed API key: %v", err)
	}
	if storedAPIKey.Name != createdAPIKey.Name {
		t.Fatalf("stored API key name = %q, want %q", storedAPIKey.Name, createdAPIKey.Name)
	}
	storedUser, err := sqliteStore.GetUserByID(setupContext, registeredUser.ID)
	if err != nil {
		t.Fatalf("read committed registered user: %v", err)
	}
	if storedUser.Username != registeredUser.Username {
		t.Fatalf("stored username = %q, want %q", storedUser.Username, registeredUser.Username)
	}
}
