package scheduler

import (
	"testing"
	"time"

	"proxycheck/backend/internal/storage"
)

func TestRunDueTasksRunsEnabledDueTasksOnly(t *testing.T) {
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	repo := fakeTaskRepo{
		tasks: []storage.Task{
			{ID: 1, Enabled: true},
			{ID: 2, Enabled: false},
			{ID: 3, Enabled: true, NextRunAt: &future},
			{ID: 4, Enabled: true},
		},
	}
	runner := &fakeTaskRunner{}
	scheduler := New(repo, runner, time.Minute)

	if err := scheduler.RunDueTasks(); err != nil {
		t.Fatalf("run due tasks: %v", err)
	}
	if got := runner.calls; len(got) != 2 || got[0] != 1 || got[1] != 4 {
		t.Fatalf("unexpected run calls: %#v", got)
	}
}

type fakeTaskRepo struct {
	tasks []storage.Task
}

func (r fakeTaskRepo) Tasks() ([]storage.Task, error) {
	return r.tasks, nil
}

type fakeTaskRunner struct {
	calls []int
}

func (r *fakeTaskRunner) RunTask(taskID int) (storage.RunSummary, error) {
	r.calls = append(r.calls, taskID)
	return storage.RunSummary{Nodes: 1, Results: 1}, nil
}
