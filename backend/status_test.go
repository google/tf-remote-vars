package backend

import (
	"testing"
	"time"

	pb "github.com/google/varlet/proto/v1"
	"github.com/jonboulle/clockwork"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestStatusTransitions(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	fakeClock := clockwork.NewFakeClock()
	server := NewServerWithClock(store, fakeClock)

	// Setup: A -> B
	err := store.RegisterNamespace(ctx, &Namespace{Name: "A"})
	if err != nil {
		t.Fatalf("failed to register A: %v", err)
	}
	err = store.RegisterNamespace(ctx, &Namespace{Name: "B"})
	if err != nil {
		t.Fatalf("failed to register B: %v", err)
	}

	val, _ := structpb.NewValue("val")
	valBytes, _ := protojson.Marshal(val)
	v := &Variable{
		Namespace: "A",
		Name:      "var",
		Version:   1,
		Value:     valBytes,
	}
	if err := store.PutVariable(ctx, v); err != nil {
		t.Fatalf("failed to put variable: %v", err)
	}

	_, err = server.SetNamespacePolicy(ctx, &pb.SetNamespacePolicyRequest{
		Namespace:        "A",
		AllowedConsumers: []string{"B"},
	})
	if err != nil {
		t.Fatalf("failed to set policy: %v", err)
	}

	_, err = server.RegisterConsumer(ctx, &pb.RegisterConsumerRequest{
		ConsumerNamespace: "B",
		SourceNamespace:   "A",
		VariableName:     "var",
	})
	if err != nil {
		t.Fatalf("failed to register consumer: %v", err)
	}

	// 1. Initial status check
	graph, err := server.GetDependencyGraph(ctx, &pb.GetDependencyGraphRequest{})
	if err != nil {
		t.Fatalf("GetDependencyGraph failed: %v", err)
	}
	if status := graph.Statuses["A"]; status != "idle" {
		t.Errorf("expected A to be idle, got %s", status)
	}
	if status := graph.Statuses["B"]; status != "idle" {
		t.Errorf("expected B to be idle, got %s", status)
	}

	// 2. A starts actuating
	_, err = server.RegisterNamespace(ctx, &pb.RegisterNamespaceRequest{Name: "A"})
	if err != nil {
		t.Fatalf("RegisterNamespace A failed: %v", err)
	}

	graph, err = server.GetDependencyGraph(ctx, &pb.GetDependencyGraphRequest{})
	if err != nil {
		t.Fatalf("GetDependencyGraph failed: %v", err)
	}
	if status := graph.Statuses["A"]; status != "actuating" {
		t.Errorf("expected A to be actuating, got %s", status)
	}
	if status := graph.Statuses["B"]; status != "potentially-affected" {
		t.Errorf("expected B to be potentially-affected, got %s", status)
	}

	// 3. A finishes with changes
	val2, _ := structpb.NewValue("val2")
	_, err = server.PutVariable(ctx, &pb.PutVariableRequest{
		Namespace: "A",
		Name:      "var",
		Value:     val2,
	})
	if err != nil {
		t.Fatalf("PutVariable A failed: %v", err)
	}

	graph, err = server.GetDependencyGraph(ctx, &pb.GetDependencyGraphRequest{})
	if err != nil {
		t.Fatalf("GetDependencyGraph failed: %v", err)
	}
	if status := graph.Statuses["A"]; status != "succeeded" {
		t.Errorf("expected A to be succeeded, got %s", status)
	}
	if status := graph.Statuses["B"]; status != "affected" {
		t.Errorf("expected B to be affected, got %s", status)
	}

	// 4. B starts actuating
	_, err = server.RegisterNamespace(ctx, &pb.RegisterNamespaceRequest{Name: "B"})
	if err != nil {
		t.Fatalf("RegisterNamespace B failed: %v", err)
	}

	graph, err = server.GetDependencyGraph(ctx, &pb.GetDependencyGraphRequest{})
	if err != nil {
		t.Fatalf("GetDependencyGraph failed: %v", err)
	}
	if status := graph.Statuses["B"]; status != "actuating" {
		t.Errorf("expected B to be actuating, got %s", status)
	}

	// 5. B times out
	fakeClock.Advance(50 * time.Second)

	graph, err = server.GetDependencyGraph(ctx, &pb.GetDependencyGraphRequest{})
	if err != nil {
		t.Fatalf("GetDependencyGraph failed: %v", err)
	}
	if status := graph.Statuses["B"]; status != "succeeded" {
		t.Errorf("expected B to be succeeded after timeout, got %s", status)
	}
}
