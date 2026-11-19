package worker

import (
	"context"
	"sync"
	"testing"
	"webhooklens/internal/models"
)

func TestConcurrentEnqueueAndShutdown(t *testing.T) {
	for i := 0; i < 100; i++ {
		pool := NewPool(nil, nil, 0, 4)
		var wg sync.WaitGroup
		for j := 0; j < 4; j++ {
			wg.Add(1)
			go func() { defer wg.Done(); pool.Enqueue(&IngestionJob{Log: &models.WebhookLog{}}) }()
		}
		if err := pool.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		if pool.Enqueue(&IngestionJob{}) {
			t.Fatal("stopped queue accepted a job")
		}
	}
}
