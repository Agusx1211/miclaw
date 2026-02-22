package tools

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const (
	cronSource      = "cron"
	defaultCronTick = time.Minute
	cronTableSQL    = `CREATE TABLE IF NOT EXISTS cron_jobs (
		id TEXT PRIMARY KEY,
		expression TEXT NOT NULL,
		prompt TEXT NOT NULL,
		created_at DATETIME
	)`
	cronInsertSQL = `INSERT INTO cron_jobs (id, expression, prompt, created_at) VALUES (?, ?, ?, ?)`
)

// Scheduler runs cron jobs and injects prompts through an inject callback.
type Scheduler struct {
	db   *sql.DB
	mu   sync.Mutex
	jobs map[string]scheduledJob
	stop context.CancelFunc
	now  func() time.Time
	tick time.Duration
}

type scheduledJob struct {
	id         string
	expression string
	prompt     string
	expr       CronExpr
	nextRun    time.Time
}

// CronJob is a persisted cron job entry used by tool responses.
type CronJob struct {
	ID         string    `json:"id"`
	Expression string    `json:"expression"`
	Prompt     string    `json:"prompt"`
	NextRun    time.Time `json:"next_run"`
}

func NewScheduler(dbPath string) (*Scheduler, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(cronTableSQL); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Scheduler{db: db, jobs: map[string]scheduledJob{}, now: time.Now, tick: defaultCronTick}
	if err := s.refreshJobs(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Scheduler) Close() error {
	return s.db.Close()
}

func (s *Scheduler) Start(ctx context.Context, inject func(source, content string)) {
	s.mu.Lock()
	runCtx, cancel := context.WithCancel(ctx)
	s.stop = cancel
	tick := s.tick
	if tick <= 0 {
		tick = defaultCronTick
	}
	s.mu.Unlock()

	ticker := time.NewTicker(tick)
	go func() {
		defer ticker.Stop()
		for {
			s.enqueueDue(inject)
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	stop := s.stop
	s.stop = nil
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func (s *Scheduler) AddJob(expression, prompt string) (string, error) {
	expr, err := ParseCronExpr(expression)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	nextRun := expr.NextAfter(s.now().UTC())
	if _, err := s.db.Exec(cronInsertSQL, id, expression, prompt, s.now().UTC()); err != nil {
		return "", err
	}
	s.mu.Lock()
	s.jobs[id] = scheduledJob{id: id, expression: expression, prompt: prompt, expr: expr, nextRun: nextRun}
	s.mu.Unlock()
	return id, nil
}

func (s *Scheduler) RemoveJob(id string) error {
	if _, err := s.db.Exec(`DELETE FROM cron_jobs WHERE id = ?`, id); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.jobs, id)
	s.mu.Unlock()
	return nil
}

func (s *Scheduler) ListJobs() ([]CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs := make([]CronJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, CronJob{ID: job.id, Expression: job.expression, Prompt: job.prompt, NextRun: job.nextRun})
	}
	return jobs, nil
}

func (s *Scheduler) NextRun(expression string) (time.Time, error) {
	expr, err := ParseCronExpr(expression)
	if err != nil {
		return time.Time{}, err
	}
	return expr.NextAfter(s.now()), nil
}

func (s *Scheduler) enqueueDue(inject func(source, content string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	for id, job := range s.jobs {
		if now.Before(job.nextRun) {
			continue
		}
		inject(cronSource, job.prompt)
		job.nextRun = job.expr.NextAfter(now)
		s.jobs[id] = job
	}
}

func (s *Scheduler) refreshJobs() error {
	rows, err := s.db.Query(`SELECT id, expression, prompt FROM cron_jobs ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id, expression, prompt string
		if err := rows.Scan(&id, &expression, &prompt); err != nil {
			return err
		}
		expr, err := ParseCronExpr(expression)
		if err != nil {
			return fmt.Errorf("invalid cron expression %q: %w", expression, err)
		}
		s.jobs[id] = scheduledJob{id: id, expression: expression, prompt: prompt, expr: expr, nextRun: expr.NextAfter(s.now().UTC())}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}
