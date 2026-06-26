package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestRegisterUserOnlyOneAdminUnderConcurrency(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()
	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("user%d", i)
			_, err := s.RegisterUser(ctx, name, "hash", 0, 0, 0)
			if err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != n {
		t.Fatalf("want %d users got %d", n, len(users))
	}
	var admins int
	for _, u := range users {
		if u.Role == RoleAdmin {
			admins++
		}
	}
	if admins != 1 {
		t.Fatalf("want exactly 1 admin got %d", admins)
	}
}

func TestFirstUserAdminAndQuotaReserve(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	u1, err := s.CreateUser(ctx, "admin1", "hash", RoleAdmin, 60, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if u1.Role != RoleAdmin {
		t.Fatalf("role %s", u1.Role)
	}

	if err := s.ReserveTotalCall(ctx, u1.ID, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveTotalCall(ctx, u1.ID, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveTotalCall(ctx, u1.ID, 2); !errors.Is(err, ErrQuotaTotal) {
		t.Fatalf("expected total quota, got %v", err)
	}

	if err := s.TryIncrementUserSuccessCalls(ctx, u1.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.TryIncrementUserSuccessCalls(ctx, u1.ID, 1); !errors.Is(err, ErrQuotaSuccess) {
		t.Fatalf("expected success quota, got %v", err)
	}
}

func TestReserveAndReleaseSuccessCall(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "u2", "h", RoleUser, 0, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveSuccessCall(ctx, u.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveSuccessCall(ctx, u.ID, 1); !errors.Is(err, ErrQuotaSuccess) {
		t.Fatalf("expected success quota on reserve, got %v", err)
	}
	if err := s.ReleaseSuccessCall(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	uAfter, _ := s.GetUserByID(ctx, u.ID)
	if uAfter.SuccessCalls != 0 {
		t.Fatalf("success_calls after release want 0 got %d", uAfter.SuccessCalls)
	}
}

func TestUpdateUserChangesTierID(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "u", "h", RoleUser, 10, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	tier, err := s.CreateTier(ctx, "t", 0, 1, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateUser(ctx, u.ID, UserUpdates{TierID: &tier.ID})
	if err != nil {
		t.Fatal(err)
	}
	if updated.TierID != tier.ID {
		t.Fatalf("tier_id want %s got %s", tier.ID, updated.TierID)
	}
}
