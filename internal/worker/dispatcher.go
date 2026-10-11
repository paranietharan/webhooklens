package worker

import (
	"context"
	"log"
	"sync"
	"time"

	"webhooklens/internal/cache"
	"webhooklens/internal/models"
	"webhooklens/internal/store"
)

type IngestionJob struct {
	Log   *models.WebhookLog
	Event *models.WebhookEvent
}

type Pool struct {
	store       store.Store
	cache       cache.Cache
	jobQueue    chan *IngestionJob
	workerCount int
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	stopped     bool
	mu          sync.Mutex
}

func NewPool(store store.Store, cache cache.Cache, workerCount, bufferSize int) *Pool {
	ctx, cancel := context.WithCancel(context.Background())
	return &Pool{
		store:       store,
		cache:       cache,
		jobQueue:    make(chan *IngestionJob, bufferSize),
		workerCount: workerCount,
		ctx:         ctx,
		cancel:      cancel,
	}
}

func (p *Pool) Start() {
	for i := 0; i < p.workerCount; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	log.Printf("[worker] started ingestion worker pool with %d workers", p.workerCount)
}

func (p *Pool) Enqueue(job *IngestionJob) bool {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return false
	}
	p.mu.Unlock()

	select {
	case p.jobQueue <- job:
		return true
	default:
		// Queue buffer is full, cannot drop silently
		log.Printf("[worker] warning: worker queue is full, dropping or backpressuring job for endpoint %s", job.Log.EndpointID)
		return false
	}
}

func (p *Pool) worker(id int) {
	defer p.wg.Done()

	for job := range p.jobQueue {
		p.processJob(job)
	}
}

func (p *Pool) processJob(job *IngestionJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Insert webhook log into PostgreSQL
	if err := p.store.WebhookLogs().Create(ctx, job.Log); err != nil {
		log.Printf("[worker] error saving webhook log %s to postgres: %v", job.Log.ID, err)
		return
	}

	// 2. Publish to Redis Pub/Sub on channel endpoint:{endpoint_id}
	if err := p.cache.PublishWebhookEvent(ctx, job.Log.EndpointID, job.Event); err != nil {
		log.Printf("[worker] error publishing webhook event %s to redis pubsub: %v", job.Log.ID, err)
	}
}

func (p *Pool) Stop(ctx context.Context) error {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return nil
	}
	p.stopped = true
	close(p.jobQueue)
	p.mu.Unlock()

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.cancel()
		log.Println("[worker] all ingestion workers drained and stopped cleanly")
		return nil
	case <-ctx.Done():
		p.cancel()
		log.Println("[worker] shutdown timed out waiting for workers to drain")
		return ctx.Err()
	}
}
