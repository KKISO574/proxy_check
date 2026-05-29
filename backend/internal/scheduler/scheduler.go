package scheduler

import (
	"context"
	"log"
	"time"

	"proxycheck/backend/internal/storage"
)

type TaskRepository interface {
	Tasks() ([]storage.Task, error)
}

type TaskRunner interface {
	RunTask(taskID int) (storage.RunSummary, error)
}

type Scheduler struct {
	repo     TaskRepository
	runner   TaskRunner
	interval time.Duration
}

func New(repo TaskRepository, runner TaskRunner, interval time.Duration) *Scheduler {
	if interval <= 0 {
		interval = time.Minute
	}
	return &Scheduler{repo: repo, runner: runner, interval: interval}
}

func (s *Scheduler) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			if err := s.RunDueTasks(); err != nil {
				log.Printf("probe scheduler failed: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Scheduler) RunDueTasks() error {
	tasks, err := s.repo.Tasks()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, task := range tasks {
		if !task.Enabled || !due(task, now) {
			continue
		}
		if _, err := s.runner.RunTask(task.ID); err != nil {
			return err
		}
	}
	return nil
}

func due(task storage.Task, now time.Time) bool {
	if task.NextRunAt == nil || *task.NextRunAt == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339Nano, *task.NextRunAt)
	if err != nil {
		return true
	}
	return !parsed.After(now)
}
