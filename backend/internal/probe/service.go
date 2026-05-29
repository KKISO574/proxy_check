package probe

import (
	"context"
	"sync"
	"time"

	"proxycheck/backend/internal/storage"
)

type Repository interface {
	Tasks() ([]storage.Task, error)
	GetTask(id int) (*storage.Task, error)
	Nodes(taskID *int) ([]storage.Node, error)
	SaveProbeBatch(nodeID int, results []storage.ProbeResultInput) error
	UpdateTask(id int, patch storage.TaskPatch) (*storage.Task, error)
}

type Prober interface {
	Probe(ctx context.Context, node storage.Node) []storage.ProbeResultInput
}

type AdvancedProber interface {
	AdvancedProbe() bool
}

type Options struct {
	Concurrency   int
	Probers       []Prober
	BeforeTaskRun func(task *storage.Task, nodes []storage.Node) error
}

type Service struct {
	repo    Repository
	options Options
	mu      sync.Mutex
}

func NewService(repo Repository, options Options) *Service {
	if options.Concurrency <= 0 {
		options.Concurrency = 100
	}
	return &Service{repo: repo, options: options}
}

func (s *Service) RunTask(taskID int) (storage.RunSummary, error) {
	if !s.mu.TryLock() {
		return storage.RunSummary{Errors: 1}, nil
	}
	defer s.mu.Unlock()
	return s.runTaskUnlocked(taskID)
}

func (s *Service) RunAdvancedTask(taskID int) (storage.RunSummary, error) {
	if !s.mu.TryLock() {
		return storage.RunSummary{Errors: 1}, nil
	}
	defer s.mu.Unlock()
	return s.runTaskWithPredicate(taskID, func(prober Prober) bool {
		return isAdvancedProber(prober)
	})
}

func (s *Service) RunAll() (storage.RunSummary, error) {
	tasks, err := s.repo.Tasks()
	if err != nil {
		return storage.RunSummary{}, err
	}
	total := storage.RunSummary{}
	for _, task := range tasks {
		if !task.Enabled {
			continue
		}
		summary, err := s.RunTask(task.ID)
		if err != nil {
			return total, err
		}
		total.Nodes += summary.Nodes
		total.Results += summary.Results
		total.Errors += summary.Errors
	}
	return total, nil
}

func (s *Service) runTaskUnlocked(taskID int) (storage.RunSummary, error) {
	return s.runTaskWithPredicate(taskID, func(Prober) bool { return true })
}

func (s *Service) runTaskWithPredicate(taskID int, include func(Prober) bool) (storage.RunSummary, error) {
	task, err := s.repo.GetTask(taskID)
	if err != nil {
		return storage.RunSummary{}, err
	}
	if task == nil || !task.Enabled {
		return storage.RunSummary{Errors: 1}, nil
	}
	nodes, err := s.repo.Nodes(&taskID)
	if err != nil {
		return storage.RunSummary{}, err
	}
	activeNodes := make([]storage.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Status == "removed" {
			continue
		}
		activeNodes = append(activeNodes, node)
	}
	if len(activeNodes) == 0 {
		if err := s.updateTaskAfterRun(task, "unknown"); err != nil {
			return storage.RunSummary{}, err
		}
		return storage.RunSummary{}, nil
	}
	if s.options.BeforeTaskRun != nil {
		if err := s.options.BeforeTaskRun(task, activeNodes); err != nil {
			return storage.RunSummary{Nodes: len(activeNodes), Errors: 1}, err
		}
	}

	semaphore := make(chan struct{}, s.options.Concurrency)
	var wg sync.WaitGroup
	collected := make([]nodeProbeResults, len(activeNodes))

	for index, node := range activeNodes {
		index := index
		node := node
		wg.Add(1)
		go func() {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			collected[index] = nodeProbeResults{
				nodeID:  node.ID,
				results: s.runNodeWithPredicate(context.Background(), task, node, include),
			}
		}()
	}
	wg.Wait()

	summary := storage.RunSummary{Nodes: len(activeNodes)}
	successes := 0
	for _, item := range collected {
		nodeErrors := countFailures(item.results)
		if err := s.repo.SaveProbeBatch(item.nodeID, item.results); err != nil {
			return summary, err
		}
		summary.Results += len(item.results)
		summary.Errors += nodeErrors
		successes += len(item.results) - nodeErrors
	}

	status := "down"
	if successes > 0 {
		status = "available"
	}
	if err := s.updateTaskAfterRun(task, status); err != nil {
		return summary, err
	}
	return summary, nil
}

type nodeProbeResults struct {
	nodeID  int
	results []storage.ProbeResultInput
}

func (s *Service) runNode(ctx context.Context, task *storage.Task, node storage.Node) []storage.ProbeResultInput {
	return s.runNodeWithPredicate(ctx, task, node, func(Prober) bool { return true })
}

func (s *Service) runNodeWithPredicate(ctx context.Context, task *storage.Task, node storage.Node, include func(Prober) bool) []storage.ProbeResultInput {
	results := make([]storage.ProbeResultInput, 0)
	for _, prober := range s.options.Probers {
		if !include(prober) {
			continue
		}
		if isAdvancedProber(prober) && (task == nil || !task.AdvancedProbesEnabled) {
			continue
		}
		results = append(results, prober.Probe(ctx, node)...)
	}
	return results
}

func isAdvancedProber(prober Prober) bool {
	advanced, ok := prober.(AdvancedProber)
	return ok && advanced.AdvancedProbe()
}

func (s *Service) updateTaskAfterRun(task *storage.Task, status string) error {
	now := time.Now().UTC()
	checked := now.Format(time.RFC3339Nano)
	next := now.Add(time.Duration(task.IntervalSeconds) * time.Second).Format(time.RFC3339Nano)
	_, err := s.repo.UpdateTask(task.ID, storage.TaskPatch{
		Status:        &status,
		LastCheckedAt: &checked,
		NextRunAt:     &next,
	})
	return err
}

func countFailures(results []storage.ProbeResultInput) int {
	count := 0
	for _, result := range results {
		if !result.Success {
			count++
		}
	}
	return count
}
