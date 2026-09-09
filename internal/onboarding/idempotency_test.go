package onboarding

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"sync"
	"testing"
)

func TestConcurrentCreationRecoversWithoutCredentialRotation(t *testing.T) {
	s, repo, q := fixture(t)
	q.ProductID = "new-product"
	q.ProductName = "new product"
	q = tested(t, s, q)
	const count = 12
	var wg sync.WaitGroup
	results := make(chan Result, count)
	failures := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.Create(context.Background(), "tenant", q)
			results <- r
			failures <- e
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	secrets := 0
	for r := range results {
		if r.Credential.Secret != "" {
			secrets++
			if r.Reused {
				t.Fatal("replay disclosed secret")
			}
		}
	}
	if secrets != 1 {
		t.Fatalf("generated %d secrets", secrets)
	}
	devices, _ := repo.ListManagedDevices(context.Background(), "tenant")
	if len(devices) != 1 {
		t.Fatal("duplicate devices")
	}
	q.TestToken = "expired"
	if r, e := s.Create(context.Background(), "tenant", q); e != nil || !r.Reused {
		t.Fatal("lost response could not recover", e)
	}
	q.Name = "different"
	if _, e := s.Create(context.Background(), "tenant", q); e == nil {
		t.Fatal("different request did not conflict")
	}
}

type failingOnboardingRepository struct{ ports.Repository }

func (r failingOnboardingRepository) SaveOnboarding(context.Context, model.OnboardingBundle) error {
	return errors.New("injected storage failure")
}
func TestFailedSaveLeavesNoResourcesAndCanRetry(t *testing.T) {
	s, repo, q := fixture(t)
	q.ProductID = "new-product"
	q.ProductName = "new product"
	q = tested(t, s, q)
	s.Repo = failingOnboardingRepository{repo}
	if _, e := s.Create(context.Background(), "tenant", q); e == nil {
		t.Fatal("failed storage reported success")
	}
	if _, e := repo.GetProduct(context.Background(), "tenant", q.ProductID); e == nil {
		t.Fatal("failed request leaked product")
	}
	if _, e := repo.GetManagedDevice(context.Background(), "tenant", q.DeviceID); e == nil {
		t.Fatal("failed request leaked device")
	}
	s.Repo = repo
	if r, e := s.Create(context.Background(), "tenant", q); e != nil || r.Reused || r.Credential.Secret == "" {
		t.Fatal("retry failed", e)
	}
}
