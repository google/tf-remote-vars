package backend

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pb "github.com/google/varlet/proto/v1"
	"github.com/jonboulle/clockwork"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() {
		store.Close()
	})
	return store
}

func TestRegisterNamespaceSuccess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	server := NewServer(store)

	req := &pb.RegisterNamespaceRequest{
		Name: "test-namespace",
	}
	resp, err := server.RegisterNamespace(ctx, req)
	if err != nil {
		t.Fatalf("RegisterNamespace failed: %v", err)
	}
	if resp.GetName() != "test-namespace" {
		t.Errorf("expected name %q, got %q", "test-namespace", resp.GetName())
	}

	// Verify it was actually stored
	ns, err := store.GetNamespace(ctx, "test-namespace")
	if err != nil {
		t.Fatalf("failed to get namespace from store: %v", err)
	}
	if ns.Name != "test-namespace" {
		t.Errorf("expected stored name %q, got %q", "test-namespace", ns.Name)
	}
}

func TestRegisterNamespaceError(t *testing.T) {
	t.Parallel()

	t.Run("EmptyName", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := newTestStore(t)
		server := NewServer(store)

		req := &pb.RegisterNamespaceRequest{
			Name: "",
		}
		_, err := server.RegisterNamespace(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		st, ok := status.FromError(err)
		if !ok {
			t.Fatalf("expected gRPC status error, got %v", err)
		}
		if st.Code() != codes.InvalidArgument {
			t.Errorf("expected code %v, got %v", codes.InvalidArgument, st.Code())
		}
	})

	t.Run("UpsertBehavior", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := newTestStore(t)
		server := NewServer(store)

		req := &pb.RegisterNamespaceRequest{
			Name:          "upsert-ns",
			RunWebhookUrl: "http://first",
		}
		_, err := server.RegisterNamespace(ctx, req)
		if err != nil {
			t.Fatalf("first registration failed: %v", err)
		}

		req.RunWebhookUrl = "http://second"
		_, err = server.RegisterNamespace(ctx, req)
		if err != nil {
			t.Fatalf("second registration failed: %v", err)
		}

		ns, err := store.GetNamespace(ctx, "upsert-ns")
		if err != nil {
			t.Fatalf("failed to get namespace: %v", err)
		}
		if ns.RunWebhookURL != "http://second" {
			t.Errorf("expected updated webhook URL 'http://second', got %q", ns.RunWebhookURL)
		}
	})
}

func TestGetNamespaceSuccess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	server := NewServer(store)

	// Pre-register
	err := store.RegisterNamespace(ctx, &Namespace{Name: "existing"})
	if err != nil {
		t.Fatalf("failed to pre-register namespace: %v", err)
	}

	req := &pb.GetNamespaceRequest{
		Name: "existing",
	}
	resp, err := server.GetNamespace(ctx, req)
	if err != nil {
		t.Fatalf("GetNamespace failed: %v", err)
	}
	if resp.GetName() != "existing" {
		t.Errorf("expected name %q, got %q", "existing", resp.GetName())
	}
}

func TestGetNamespaceError(t *testing.T) {
	t.Parallel()

	t.Run("EmptyName", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := newTestStore(t)
		server := NewServer(store)

		req := &pb.GetNamespaceRequest{
			Name: "",
		}
		_, err := server.GetNamespace(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		st, ok := status.FromError(err)
		if !ok {
			t.Fatalf("expected gRPC status error, got %v", err)
		}
		if st.Code() != codes.InvalidArgument {
			t.Errorf("expected code %v, got %v", codes.InvalidArgument, st.Code())
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := newTestStore(t)
		server := NewServer(store)

		req := &pb.GetNamespaceRequest{
			Name: "non-existent",
		}
		_, err := server.GetNamespace(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		st, ok := status.FromError(err)
		if !ok {
			t.Fatalf("expected gRPC status error, got %v", err)
		}
		if st.Code() != codes.NotFound {
			t.Errorf("expected code %v, got %v", codes.NotFound, st.Code())
		}
	})
}

func TestPutVariableSuccess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	server := NewServer(store)

	err := store.RegisterNamespace(ctx, &Namespace{Name: "ns1"})
	if err != nil {
		t.Fatalf("failed to register namespace: %v", err)
	}

	val1, err := structpb.NewValue("value1")
	if err != nil {
		t.Fatalf("failed to create value: %v", err)
	}

	req := &pb.PutVariableRequest{
		Namespace: "ns1",
		Name:      "var1",
		Value:     val1,
	}
	_, err = server.PutVariable(ctx, req)
	if err != nil {
		t.Fatalf("PutVariable failed: %v", err)
	}

	v, err := store.GetLatestVariable(ctx, "ns1", "var1")
	if err != nil {
		t.Fatalf("failed to get variable: %v", err)
	}
	if v.Version != 1 {
		t.Errorf("expected version 1, got %d", v.Version)
	}
	var gotVal structpb.Value
	err = protojson.Unmarshal(v.Value, &gotVal)
	if err != nil {
		t.Fatalf("failed to unmarshal value: %v", err)
	}
	if gotVal.GetStringValue() != "value1" {
		t.Errorf("expected value1, got %v", gotVal.GetStringValue())
	}

	val2, err := structpb.NewValue("value2")
	if err != nil {
		t.Fatalf("failed to create value: %v", err)
	}

	req.Value = val2
	_, err = server.PutVariable(ctx, req)
	if err != nil {
		t.Fatalf("PutVariable failed: %v", err)
	}

	v, err = store.GetLatestVariable(ctx, "ns1", "var1")
	if err != nil {
		t.Fatalf("failed to get variable: %v", err)
	}
	if v.Version != 2 {
		t.Errorf("expected version 2, got %d", v.Version)
	}
	err = protojson.Unmarshal(v.Value, &gotVal)
	if err != nil {
		t.Fatalf("failed to unmarshal value: %v", err)
	}
	if gotVal.GetStringValue() != "value2" {
		t.Errorf("expected value2, got %v", gotVal.GetStringValue())
	}

	req.Value = val2
	req.ForceActuation = false
	_, err = server.PutVariable(ctx, req)
	if err != nil {
		t.Fatalf("PutVariable failed: %v", err)
	}

	v, err = store.GetLatestVariable(ctx, "ns1", "var1")
	if err != nil {
		t.Fatalf("failed to get variable: %v", err)
	}
	if v.Version != 2 {
		t.Errorf("expected version 2, got %d", v.Version)
	}

	req.ForceActuation = true
	_, err = server.PutVariable(ctx, req)
	if err != nil {
		t.Fatalf("PutVariable failed: %v", err)
	}

	v, err = store.GetLatestVariable(ctx, "ns1", "var1")
	if err != nil {
		t.Fatalf("failed to get variable: %v", err)
	}
	if v.Version != 3 {
		t.Errorf("expected version 3, got %d", v.Version)
	}
}

func TestPutVariableError(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	server := NewServer(store)
	val, err := structpb.NewValue("val")
	if err != nil {
		t.Fatalf("failed to create value: %v", err)
	}

	t.Run("EmptyNamespace", func(t *testing.T) {
		t.Parallel()
		req := &pb.PutVariableRequest{
			Namespace: "",
			Name:      "var",
			Value:     val,
		}
		_, err := server.PutVariable(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.InvalidArgument {
			t.Errorf("expected InvalidArgument, got %v", err)
		}
	})

	t.Run("EmptyName", func(t *testing.T) {
		t.Parallel()
		req := &pb.PutVariableRequest{
			Namespace: "ns",
			Name:      "",
			Value:     val,
		}
		_, err := server.PutVariable(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.InvalidArgument {
			t.Errorf("expected InvalidArgument, got %v", err)
		}
	})

	t.Run("NilValue", func(t *testing.T) {
		t.Parallel()
		req := &pb.PutVariableRequest{
			Namespace: "ns",
			Name:      "var",
			Value:     nil,
		}
		_, err := server.PutVariable(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.InvalidArgument {
			t.Errorf("expected InvalidArgument, got %v", err)
		}
	})

	t.Run("NonExistentNamespace", func(t *testing.T) {
		t.Parallel()
		req := &pb.PutVariableRequest{
			Namespace: "non-existent",
			Name:      "var",
			Value:     val,
		}
		_, err := server.PutVariable(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.Internal {
			t.Errorf("expected Internal error, got %v", err)
		}
	})
}

func TestDeleteVariableSuccess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	server := NewServer(store)

	err := store.RegisterNamespace(ctx, &Namespace{Name: "ns1"})
	if err != nil {
		t.Fatalf("failed to register namespace: %v", err)
	}

	val, err := structpb.NewValue("val")
	if err != nil {
		t.Fatalf("failed to create value: %v", err)
	}
	valBytes, err := protojson.Marshal(val)
	if err != nil {
		t.Fatalf("failed to marshal value: %v", err)
	}

	v := &Variable{
		Namespace: "ns1",
		Name:      "var1",
		Version:   1,
		Value:     valBytes,
	}
	if err := store.PutVariable(ctx, v); err != nil {
		t.Fatalf("failed to put variable: %v", err)
	}
	v.Version = 2
	if err := store.PutVariable(ctx, v); err != nil {
		t.Fatalf("failed to put variable v2: %v", err)
	}

	req := &pb.DeleteVariableRequest{
		Namespace: "ns1",
		Name:      "var1",
	}
	_, err = server.DeleteVariable(ctx, req)
	if err != nil {
		t.Fatalf("DeleteVariable failed: %v", err)
	}

	_, err = store.GetLatestVariable(ctx, "ns1", "var1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteVariableError(t *testing.T) {
	t.Parallel()

	t.Run("EmptyNamespace", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := newTestStore(t)
		server := NewServer(store)
		req := &pb.DeleteVariableRequest{
			Namespace: "",
			Name:      "var",
		}
		_, err := server.DeleteVariable(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.InvalidArgument {
			t.Errorf("expected InvalidArgument, got %v", err)
		}
	})

	t.Run("EmptyName", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := newTestStore(t)
		server := NewServer(store)
		req := &pb.DeleteVariableRequest{
			Namespace: "ns",
			Name:      "",
		}
		_, err := server.DeleteVariable(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.InvalidArgument {
			t.Errorf("expected InvalidArgument, got %v", err)
		}
	})
}

func TestRegisterConsumerSuccess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	server := NewServer(store)

	if err := store.RegisterNamespace(ctx, &Namespace{Name: "source-ns"}); err != nil {
		t.Fatalf("failed to register source namespace: %v", err)
	}
	if err := store.RegisterNamespace(ctx, &Namespace{Name: "consumer-ns"}); err != nil {
		t.Fatalf("failed to register consumer namespace: %v", err)
	}

	val, _ := structpb.NewValue("hello")
	valBytes, _ := protojson.Marshal(val)
	v := &Variable{
		Namespace: "source-ns",
		Name:      "my_var",
		Version:   1,
		Value:     valBytes,
	}
	if err := store.PutVariable(ctx, v); err != nil {
		t.Fatalf("failed to put variable: %v", err)
	}

	req := &pb.RegisterConsumerRequest{
		ConsumerNamespace: "consumer-ns",
		SourceNamespace:   "source-ns",
		VariableName:     "my_var",
	}

	resp, err := server.RegisterConsumer(ctx, req)
	if err != nil {
		t.Fatalf("RegisterConsumer failed: %v", err)
	}

	if resp.GetActuationNonce() != 1 {
		t.Errorf("expected nonce 1, got %d", resp.GetActuationNonce())
	}
	if resp.GetValue().GetStringValue() != "hello" {
		t.Errorf("expected value 'hello', got %v", resp.GetValue())
	}

	isCons, err := store.IsConsumer(ctx, "consumer-ns", "source-ns", "my_var")
	if err != nil {
		t.Fatalf("IsConsumer failed: %v", err)
	}
	if !isCons {
		t.Error("expected consumer-ns to be consumer of source-ns/my_var")
	}
}

func TestRegisterConsumerCycleDetection(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	server := NewServer(store)

	for _, ns := range []string{"A", "B", "C"} {
		if err := store.RegisterNamespace(ctx, &Namespace{Name: ns}); err != nil {
			t.Fatalf("failed to register namespace %s: %v", ns, err)
		}
	}

	val, _ := structpb.NewValue("dummy")
	valBytes, _ := protojson.Marshal(val)
	for _, ns := range []string{"A", "B", "C"} {
		v := &Variable{
			Namespace: ns,
			Name:      "var",
			Version:   1,
			Value:     valBytes,
		}
		if err := store.PutVariable(ctx, v); err != nil {
			t.Fatalf("failed to put variable in %s: %v", ns, err)
		}
	}

	_, err := server.RegisterConsumer(ctx, &pb.RegisterConsumerRequest{
		ConsumerNamespace: "A",
		SourceNamespace:   "B",
		VariableName:     "var",
	})
	if err != nil {
		t.Fatalf("RegisterConsumer A->B failed: %v", err)
	}

	_, err = server.RegisterConsumer(ctx, &pb.RegisterConsumerRequest{
		ConsumerNamespace: "B",
		SourceNamespace:   "C",
		VariableName:     "var",
	})
	if err != nil {
		t.Fatalf("RegisterConsumer B->C failed: %v", err)
	}

	_, err = server.RegisterConsumer(ctx, &pb.RegisterConsumerRequest{
		ConsumerNamespace: "C",
		SourceNamespace:   "A",
		VariableName:     "var",
	})

	if err == nil {
		t.Fatal("expected error due to cycle, got nil")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.FailedPrecondition {
		t.Errorf("expected FailedPrecondition, got %v", err)
	}
}

func TestGetVariableValueSuccess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	server := NewServer(store)

	if err := store.RegisterNamespace(ctx, &Namespace{Name: "ns1"}); err != nil {
		t.Fatalf("failed to register ns1: %v", err)
	}
	if err := store.RegisterNamespace(ctx, &Namespace{Name: "ns2"}); err != nil {
		t.Fatalf("failed to register ns2: %v", err)
	}

	val, _ := structpb.NewValue("hello")
	valBytes, _ := protojson.Marshal(val)
	v := &Variable{
		Namespace: "ns1",
		Name:      "var1",
		Version:   42,
		Value:     valBytes,
	}
	if err := store.PutVariable(ctx, v); err != nil {
		t.Fatalf("failed to put variable: %v", err)
	}

	_, err := server.RegisterConsumer(ctx, &pb.RegisterConsumerRequest{
		ConsumerNamespace: "ns2",
		SourceNamespace:   "ns1",
		VariableName:     "var1",
	})
	if err != nil {
		t.Fatalf("RegisterConsumer failed: %v", err)
	}

	req := &pb.GetVariableValueRequest{
		ConsumerNamespace: "ns2",
		SourceNamespace:   "ns1",
		VariableName:     "var1",
	}
	resp, err := server.GetVariableValue(ctx, req)
	if err != nil {
		t.Fatalf("GetVariableValue failed: %v", err)
	}

	if resp.GetActuationNonce() != 42 {
		t.Errorf("expected nonce 42, got %d", resp.GetActuationNonce())
	}
	if resp.GetValue().GetStringValue() != "hello" {
		t.Errorf("expected 'hello', got %v", resp.GetValue())
	}
}

func TestGetVariableValueNotRegistered(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	server := NewServer(store)

	if err := store.RegisterNamespace(ctx, &Namespace{Name: "ns1"}); err != nil {
		t.Fatalf("failed to register ns1: %v", err)
	}
	if err := store.RegisterNamespace(ctx, &Namespace{Name: "ns2"}); err != nil {
		t.Fatalf("failed to register ns2: %v", err)
	}

	val, _ := structpb.NewValue("hello")
	valBytes, _ := protojson.Marshal(val)
	v := &Variable{
		Namespace: "ns1",
		Name:      "var1",
		Version:   1,
		Value:     valBytes,
	}
	if err := store.PutVariable(ctx, v); err != nil {
		t.Fatalf("failed to put variable: %v", err)
	}

	req := &pb.GetVariableValueRequest{
		ConsumerNamespace: "ns2",
		SourceNamespace:   "ns1",
		VariableName:     "var1",
	}
	_, err := server.GetVariableValue(ctx, req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.FailedPrecondition {
		t.Errorf("expected FailedPrecondition, got %v", err)
	}
}

func TestDeregisterConsumerSuccess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	server := NewServer(store)

	if err := store.RegisterNamespace(ctx, &Namespace{Name: "ns1"}); err != nil {
		t.Fatalf("failed to register ns1: %v", err)
	}
	if err := store.RegisterNamespace(ctx, &Namespace{Name: "ns2"}); err != nil {
		t.Fatalf("failed to register ns2: %v", err)
	}
	val, _ := structpb.NewValue("hello")
	valBytes, _ := protojson.Marshal(val)
	v := &Variable{
		Namespace: "ns1",
		Name:      "var1",
		Version:   1,
		Value:     valBytes,
	}
	if err := store.PutVariable(ctx, v); err != nil {
		t.Fatalf("failed to put variable: %v", err)
	}

	_, err := server.RegisterConsumer(ctx, &pb.RegisterConsumerRequest{
		ConsumerNamespace: "ns2",
		SourceNamespace:   "ns1",
		VariableName:     "var1",
	})
	if err != nil {
		t.Fatalf("RegisterConsumer failed: %v", err)
	}

	_, err = server.DeregisterConsumer(ctx, &pb.DeregisterConsumerRequest{
		ConsumerNamespace: "ns2",
		SourceNamespace:   "ns1",
		VariableName:     "var1",
	})
	if err != nil {
		t.Fatalf("DeregisterConsumer failed: %v", err)
	}

	isCons, err := store.IsConsumer(ctx, "ns2", "ns1", "var1")
	if err != nil {
		t.Fatalf("IsConsumer failed: %v", err)
	}
	if isCons {
		t.Error("expected ns2 to NOT be consumer anymore")
	}
}

func TestDeleteVariableBlocked(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	server := NewServer(store)

	if err := store.RegisterNamespace(ctx, &Namespace{Name: "ns1"}); err != nil {
		t.Fatalf("failed to register ns1: %v", err)
	}
	if err := store.RegisterNamespace(ctx, &Namespace{Name: "ns2"}); err != nil {
		t.Fatalf("failed to register ns2: %v", err)
	}
	val, _ := structpb.NewValue("hello")
	valBytes, _ := protojson.Marshal(val)
	v := &Variable{
		Namespace: "ns1",
		Name:      "var1",
		Version:   1,
		Value:     valBytes,
	}
	if err := store.PutVariable(ctx, v); err != nil {
		t.Fatalf("failed to put variable: %v", err)
	}
	_, err := server.RegisterConsumer(ctx, &pb.RegisterConsumerRequest{
		ConsumerNamespace: "ns2",
		SourceNamespace:   "ns1",
		VariableName:     "var1",
	})
	if err != nil {
		t.Fatalf("RegisterConsumer failed: %v", err)
	}

	_, err = server.DeleteVariable(ctx, &pb.DeleteVariableRequest{
		Namespace: "ns1",
		Name:      "var1",
		Force:     false,
	})
	if err == nil {
		t.Fatal("expected error deleting variable with active consumers, got nil")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.FailedPrecondition {
		t.Errorf("expected FailedPrecondition, got %v", err)
	}

	_, err = server.DeleteVariable(ctx, &pb.DeleteVariableRequest{
		Namespace: "ns1",
		Name:      "var1",
		Force:     true,
	})
	if err != nil {
		t.Fatalf("DeleteVariable with force failed: %v", err)
	}

	_, err = store.GetLatestVariable(ctx, "ns1", "var1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestNamespacePolicyAndWebhook(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	server := NewServer(store)

	// Register with policy and webhook
	req := &pb.RegisterNamespaceRequest{
		Name: "policy-ns",
		RetentionPolicy: &pb.RetentionPolicy{
			MinVersions: 5,
			MaxAgeDays:  10,
		},
		RunWebhookUrl: "http://example.com/webhook",
	}
	_, err := server.RegisterNamespace(ctx, req)
	if err != nil {
		t.Fatalf("RegisterNamespace failed: %v", err)
	}

	// Retrieve and verify
	getResp, err := server.GetNamespace(ctx, &pb.GetNamespaceRequest{Name: "policy-ns"})
	if err != nil {
		t.Fatalf("GetNamespace failed: %v", err)
	}
	if getResp.GetRunWebhookUrl() != "http://example.com/webhook" {
		t.Errorf("expected webhook url, got %q", getResp.GetRunWebhookUrl())
	}
	if getResp.GetRetentionPolicy() == nil {
		t.Fatal("expected retention policy, got nil")
	}
	if getResp.GetRetentionPolicy().GetMinVersions() != 5 {
		t.Errorf("expected min versions 5, got %d", getResp.GetRetentionPolicy().GetMinVersions())
	}
	if getResp.GetRetentionPolicy().GetMaxAgeDays() != 10 {
		t.Errorf("expected max age days 10, got %d", getResp.GetRetentionPolicy().GetMaxAgeDays())
	}
}

func TestSetNamespacePolicyAndAccessControl(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	server := NewServer(store)

	// Register source and consumers
	for _, ns := range []string{"source", "allowed-1", "allowed-2", "blocked"} {
		if err := store.RegisterNamespace(ctx, &Namespace{Name: ns}); err != nil {
			t.Fatalf("failed to register namespace %s: %v", ns, err)
		}
	}

	// Create a variable in source
	val, _ := structpb.NewValue("secret")
	_, err := server.PutVariable(ctx, &pb.PutVariableRequest{
		Namespace: "source",
		Name:      "var",
		Value:     val,
	})
	if err != nil {
		t.Fatalf("PutVariable failed: %v", err)
	}

	// Set policy: allow "allowed-*"
	_, err = server.SetNamespacePolicy(ctx, &pb.SetNamespacePolicyRequest{
		Namespace:        "source",
		AllowedConsumers: []string{"allowed-*"},
	})
	if err != nil {
		t.Fatalf("SetNamespacePolicy failed: %v", err)
	}

	// Verify allowed consumer can register
	_, err = server.RegisterConsumer(ctx, &pb.RegisterConsumerRequest{
		ConsumerNamespace: "allowed-1",
		SourceNamespace:   "source",
		VariableName:     "var",
	})
	if err != nil {
		t.Errorf("RegisterConsumer for allowed-1 failed: %v", err)
	}

	// Verify blocked consumer cannot register
	_, err = server.RegisterConsumer(ctx, &pb.RegisterConsumerRequest{
		ConsumerNamespace: "blocked",
		SourceNamespace:   "source",
		VariableName:     "var",
	})
	if err == nil {
		t.Error("expected RegisterConsumer for blocked to fail, got nil")
	} else {
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.PermissionDenied {
			t.Errorf("expected PermissionDenied, got %v", err)
		}
	}

	// Verify blocked consumer cannot read value directly
	if err := store.RegisterConsumer(ctx, "blocked", "source", "var"); err != nil {
		t.Fatalf("failed to force register: %v", err)
	}

	_, err = server.GetVariableValue(ctx, &pb.GetVariableValueRequest{
		ConsumerNamespace: "blocked",
		SourceNamespace:   "source",
		VariableName:     "var",
	})
	if err == nil {
		t.Error("expected GetVariableValue for blocked to fail, got nil")
	} else {
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.PermissionDenied {
			t.Errorf("expected PermissionDenied, got %v", err)
		}
	}

	// Allowed consumer can read
	_, err = server.GetVariableValue(ctx, &pb.GetVariableValueRequest{
		ConsumerNamespace: "allowed-1",
		SourceNamespace:   "source",
		VariableName:     "var",
	})
	if err != nil {
		t.Errorf("GetVariableValue for allowed-1 failed: %v", err)
	}
}

func TestRetentionPolicyPruning(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	fakeClock := clockwork.NewFakeClock()
	server := NewServerWithClock(store, fakeClock)

	// Register with min=2, age=10 days
	err := store.RegisterNamespace(ctx, &Namespace{
		Name:                       "ns",
		RetentionPolicyMinVersions: 2,
		RetentionPolicyMaxAgeDays:  10,
	})
	if err != nil {
		t.Fatalf("failed to register namespace: %v", err)
	}

	val, _ := structpb.NewValue("val")
	putReq := &pb.PutVariableRequest{
		Namespace: "ns",
		Name:      "var",
		Value:     val,
	}

	// Write v1 (t=0)
	_, err = server.PutVariable(ctx, putReq)
	if err != nil {
		t.Fatalf("Put v1 failed: %v", err)
	}

	// Advance time 5 days
	fakeClock.Advance(5 * 24 * time.Hour)

	// Write v2 (t=5d)
	putReq.Value, _ = structpb.NewValue("val2")
	_, err = server.PutVariable(ctx, putReq)
	if err != nil {
		t.Fatalf("Put v2 failed: %v", err)
	}

	// Advance time 6 days (total 11 days)
	fakeClock.Advance(6 * 24 * time.Hour)

	// Write v3 (t=11d). v1 is older than 10d.
	putReq.Value, _ = structpb.NewValue("val3")
	_, err = server.PutVariable(ctx, putReq)
	if err != nil {
		t.Fatalf("Put v3 failed: %v", err)
	}

	// Verify v1 is pruned, v2 and v3 exist
	sqlStore := store.(*SQLiteStore)
	var count int
	err = sqlStore.db.QueryRow("SELECT COUNT(*) FROM variables WHERE namespace = 'ns' AND name = 'var'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count variables: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 versions remaining, got %d", count)
	}

	var versions []int64
	rows, err := sqlStore.db.Query("SELECT version FROM variables WHERE namespace = 'ns' AND name = 'var' ORDER BY version ASC")
	if err != nil {
		t.Fatalf("failed to query versions: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		versions = append(versions, v)
	}
	if len(versions) != 2 || versions[0] != 2 || versions[1] != 3 {
		t.Errorf("expected versions [2, 3], got %v", versions)
	}

	// Test "keep min versions anyway"
	fakeClock.Advance(20 * 24 * time.Hour)

	// Write v4 (t=31d). v2 and v3 are older than 10d.
	putReq.Value, _ = structpb.NewValue("val4")
	_, err = server.PutVariable(ctx, putReq)
	if err != nil {
		t.Fatalf("Put v4 failed: %v", err)
	}

	versions = nil
	rows, err = sqlStore.db.Query("SELECT version FROM variables WHERE namespace = 'ns' AND name = 'var' ORDER BY version ASC")
	if err != nil {
		t.Fatalf("failed to query versions: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan error: %v", err)
		}
		versions = append(versions, v)
	}
	if len(versions) != 2 || versions[0] != 3 || versions[1] != 4 {
		t.Errorf("expected versions [3, 4] (v2 pruned, v3 kept to satisfy min=2), got %v", versions)
	}
}

func TestGetDependencyGraph(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	server := NewServer(store)

	// Set up graph: A -> B -> C, D (isolated)
	for _, ns := range []string{"A", "B", "C", "D"} {
		err := store.RegisterNamespace(ctx, &Namespace{Name: ns})
		if err != nil {
			t.Fatalf("failed to register namespace %s: %v", ns, err)
		}
	}

	val, _ := structpb.NewValue("val")
	_, err := server.PutVariable(ctx, &pb.PutVariableRequest{Namespace: "C", Name: "var_c", Value: val})
	if err != nil {
		t.Fatalf("failed to put var_c: %v", err)
	}
	_, err = server.PutVariable(ctx, &pb.PutVariableRequest{Namespace: "B", Name: "var_b", Value: val})
	if err != nil {
		t.Fatalf("failed to put var_b: %v", err)
	}

	_, err = server.RegisterConsumer(ctx, &pb.RegisterConsumerRequest{
		ConsumerNamespace: "A",
		SourceNamespace:   "B",
		VariableName:     "var_b",
	})
	if err != nil {
		t.Fatalf("RegisterConsumer A->B failed: %v", err)
	}

	_, err = server.RegisterConsumer(ctx, &pb.RegisterConsumerRequest{
		ConsumerNamespace: "B",
		SourceNamespace:   "C",
		VariableName:     "var_c",
	})
	if err != nil {
		t.Fatalf("RegisterConsumer B->C failed: %v", err)
	}

	t.Run("EntireGraph", func(t *testing.T) {
		resp, err := server.GetDependencyGraph(ctx, &pb.GetDependencyGraphRequest{})
		if err != nil {
			t.Fatalf("GetDependencyGraph failed: %v", err)
		}

		expectedNS := map[string]bool{"A": true, "B": true, "C": true, "D": true}
		if len(resp.GetNamespaces()) != len(expectedNS) {
			t.Errorf("expected %d namespaces, got %d", len(expectedNS), len(resp.GetNamespaces()))
		}
		for _, ns := range resp.GetNamespaces() {
			if !expectedNS[ns] {
				t.Errorf("unexpected namespace in response: %s", ns)
			}
		}

		if len(resp.GetEdges()) != 2 {
			t.Errorf("expected 2 edges, got %d", len(resp.GetEdges()))
		}
		hasAB := false
		hasBC := false
		for _, edge := range resp.GetEdges() {
			if edge.GetConsumerNamespace() == "A" && edge.GetSourceNamespace() == "B" && edge.GetVariableName() == "var_b" {
				hasAB = true
			}
			if edge.GetConsumerNamespace() == "B" && edge.GetSourceNamespace() == "C" && edge.GetVariableName() == "var_c" {
				hasBC = true
			}
		}
		if !hasAB {
			t.Error("missing edge A->B")
		}
		if !hasBC {
			t.Error("missing edge B->C")
		}
	})

	t.Run("FilteredByRootA", func(t *testing.T) {
		resp, err := server.GetDependencyGraph(ctx, &pb.GetDependencyGraphRequest{Namespace: "A"})
		if err != nil {
			t.Fatalf("GetDependencyGraph failed: %v", err)
		}

		expectedNS := map[string]bool{"A": true, "B": true, "C": true}
		if len(resp.GetNamespaces()) != len(expectedNS) {
			t.Errorf("expected %d namespaces, got %d: %v", len(expectedNS), len(resp.GetNamespaces()), resp.GetNamespaces())
		}
		for _, ns := range resp.GetNamespaces() {
			if !expectedNS[ns] {
				t.Errorf("unexpected namespace in response: %s", ns)
			}
		}

		if len(resp.GetEdges()) != 2 {
			t.Errorf("expected 2 edges, got %d", len(resp.GetEdges()))
		}
	})

	t.Run("FilteredByRootB", func(t *testing.T) {
		resp, err := server.GetDependencyGraph(ctx, &pb.GetDependencyGraphRequest{Namespace: "B"})
		if err != nil {
			t.Fatalf("GetDependencyGraph failed: %v", err)
		}

		expectedNS := map[string]bool{"B": true, "C": true}
		if len(resp.GetNamespaces()) != len(expectedNS) {
			t.Errorf("expected %d namespaces, got %d: %v", len(expectedNS), len(resp.GetNamespaces()), resp.GetNamespaces())
		}
		for _, ns := range resp.GetNamespaces() {
			if !expectedNS[ns] {
				t.Errorf("unexpected namespace in response: %s", ns)
			}
		}

		if len(resp.GetEdges()) != 1 {
			t.Errorf("expected 1 edge, got %d", len(resp.GetEdges()))
		}
		edge := resp.GetEdges()[0]
		if edge.GetConsumerNamespace() != "B" || edge.GetSourceNamespace() != "C" {
			t.Errorf("expected edge B->C, got %s->%s", edge.GetConsumerNamespace(), edge.GetSourceNamespace())
		}
	})

	t.Run("FilteredByRootD", func(t *testing.T) {
		resp, err := server.GetDependencyGraph(ctx, &pb.GetDependencyGraphRequest{Namespace: "D"})
		if err != nil {
			t.Fatalf("GetDependencyGraph failed: %v", err)
		}

		if len(resp.GetNamespaces()) != 1 || resp.GetNamespaces()[0] != "D" {
			t.Errorf("expected namespaces [D], got %v", resp.GetNamespaces())
		}
		if len(resp.GetEdges()) != 0 {
			t.Errorf("expected 0 edges, got %d", len(resp.GetEdges()))
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		_, err := server.GetDependencyGraph(ctx, &pb.GetDependencyGraphRequest{Namespace: "non-existent"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.NotFound {
			t.Errorf("expected NotFound, got %v", err)
		}
	})
}

func TestWebhookPropagation(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := newTestStore(t)
	server := NewServer(store)

	received := make(chan struct {
		Consumer string
		Source   string
		Variable string
	}, 10)

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			ConsumerNamespace string `json:"consumer_namespace"`
			SourceNamespace   string `json:"source_namespace"`
			VariableName     string `json:"variable_name"`
		}
		err := json.NewDecoder(r.Body).Decode(&payload)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		t.Logf("Test server received webhook for consumer %q", payload.ConsumerNamespace)
		received <- struct {
			Consumer string
			Source   string
			Variable string
		}{
			Consumer: payload.ConsumerNamespace,
			Source:   payload.SourceNamespace,
			Variable: payload.VariableName,
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer testServer.Close()

	err := store.RegisterNamespace(ctx, &Namespace{Name: "source-ns"})
	if err != nil {
		t.Fatalf("failed to register source-ns: %v", err)
	}
	err = store.RegisterNamespace(ctx, &Namespace{
		Name:          "consumer-1",
		RunWebhookURL: testServer.URL,
	})
	if err != nil {
		t.Fatalf("failed to register consumer-1: %v", err)
	}
	err = store.RegisterNamespace(ctx, &Namespace{
		Name: "consumer-2",
	})
	if err != nil {
		t.Fatalf("failed to register consumer-2: %v", err)
	}
	err = store.RegisterNamespace(ctx, &Namespace{
		Name:          "consumer-3",
		RunWebhookURL: testServer.URL,
	})
	if err != nil {
		t.Fatalf("failed to register consumer-3: %v", err)
	}

	val, _ := structpb.NewValue("initial")
	_, err = server.PutVariable(ctx, &pb.PutVariableRequest{
		Namespace: "source-ns",
		Name:      "var1",
		Value:     val,
	})
	if err != nil {
		t.Fatalf("failed to put variable: %v", err)
	}

	for _, c := range []string{"consumer-1", "consumer-2", "consumer-3"} {
		_, err = server.RegisterConsumer(ctx, &pb.RegisterConsumerRequest{
			ConsumerNamespace: c,
			SourceNamespace:   "source-ns",
			VariableName:     "var1",
		})
		if err != nil {
			t.Fatalf("failed to register consumer %s: %v", c, err)
		}
	}

	val2, _ := structpb.NewValue("updated")
	_, err = server.PutVariable(ctx, &pb.PutVariableRequest{
		Namespace: "source-ns",
		Name:      "var1",
		Value:     val2,
	})
	if err != nil {
		t.Fatalf("failed to update variable: %v", err)
	}

	expected := map[string]bool{
		"consumer-1": true,
		"consumer-3": true,
	}

	numExpected := len(expected)
	timeout := time.After(2 * time.Second)
	for i := 0; i < numExpected; i++ {
		select {
		case event := <-received:
			if event.Source != "source-ns" || event.Variable != "var1" {
				t.Errorf("unexpected event content: %+v", event)
			}
			if !expected[event.Consumer] {
				t.Errorf("unexpected consumer triggered: %s", event.Consumer)
			}
			delete(expected, event.Consumer)
		case <-timeout:
			t.Fatalf("timeout waiting for webhooks, missing consumers: %+v", expected)
		}
	}

	// Give webhook client time to finish receiving responses before closing testServer
	time.Sleep(100 * time.Millisecond)

	select {
	case event := <-received:
		t.Errorf("received extra unexpected event: %+v", event)
	default:
		// OK
	}
}

