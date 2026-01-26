package multiagent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuildDependencyGraph(t *testing.T) {
	agents := []AgentDefinition{
		{ID: "a"},
		{ID: "b"},
		{ID: "c", DependsOn: []string{"a", "b"}},
	}

	graph, err := BuildDependencyGraph(agents)
	if err != nil {
		t.Fatalf("BuildDependencyGraph: %v", err)
	}

	stages := graph.Stages()
	if len(stages) != 2 {
		t.Fatalf("Stages=%v, want 2 stages", stages)
	}
	if len(stages[0]) != 2 || stages[0][0] != "a" || stages[0][1] != "b" {
		t.Fatalf("Stage0=%v, want [a b]", stages[0])
	}
	if len(stages[1]) != 1 || stages[1][0] != "c" {
		t.Fatalf("Stage1=%v, want [c]", stages[1])
	}
}

func TestBuildDependencyGraph_Cycle(t *testing.T) {
	_, err := BuildDependencyGraph([]AgentDefinition{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	})
	if err == nil {
		t.Fatalf("expected cycle error")
	}
}

func TestSwarmExecute_DependencyAndParallelism(t *testing.T) {
	agents := []AgentDefinition{
		{ID: "a"},
		{ID: "b"},
		{ID: "c", DependsOn: []string{"a", "b"}},
	}

	swarm, err := NewSwarm(SwarmConfig{Enabled: true, MaxParallelAgents: 2}, agents)
	if err != nil {
		t.Fatalf("NewSwarm: %v", err)
	}

	started := make(chan string, 2)
	release := make(chan struct{})

	var current int32
	var maxSeen int32

	exec := func(ctx context.Context, agentID string, shared SwarmSharedContext) (any, error) {
		n := atomic.AddInt32(&current, 1)
		for {
			m := atomic.LoadInt32(&maxSeen)
			if n <= m {
				break
			}
			if atomic.CompareAndSwapInt32(&maxSeen, m, n) {
				break
			}
		}
		defer atomic.AddInt32(&current, -1)

		switch agentID {
		case "a", "b":
			started <- agentID
			<-release
			time.Sleep(10 * time.Millisecond)
			return agentID + "-out", nil
		case "c":
			if _, ok := shared.GetFromAgent("a"); !ok {
				t.Errorf("expected shared output for a")
			}
			if _, ok := shared.GetFromAgent("b"); !ok {
				t.Errorf("expected shared output for b")
			}
			return "c-out", nil
		default:
			t.Fatalf("unexpected agent id: %s", agentID)
			return nil, nil
		}
	}

	done := make(chan struct{})
	var (
		result *SwarmResult
		runErr error
	)
	go func() {
		defer close(done)
		result, runErr = swarm.Execute(context.Background(), exec)
	}()

	// Ensure a and b are running concurrently before letting them finish.
	<-started
	<-started
	close(release)
	<-done

	if runErr != nil {
		t.Fatalf("Execute: %v", runErr)
	}
	if result == nil || len(result.Results) != 3 {
		t.Fatalf("result=%v, want 3 results", result)
	}
	if atomic.LoadInt32(&maxSeen) > 2 {
		t.Fatalf("maxSeen=%d, want <= 2", maxSeen)
	}
	if atomic.LoadInt32(&maxSeen) < 2 {
		t.Fatalf("maxSeen=%d, want >= 2 (expected concurrency)", maxSeen)
	}
}
