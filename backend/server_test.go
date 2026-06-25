package backend

import (
	"errors"
	"testing"

	pb "github.com/google/varlet/proto/v1"
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

	t.Run("DuplicateName", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := newTestStore(t)
		server := NewServer(store)

		req := &pb.RegisterNamespaceRequest{
			Name: "duplicate",
		}
		_, err := server.RegisterNamespace(ctx, req)
		if err != nil {
			t.Fatalf("first registration failed: %v", err)
		}

		_, err = server.RegisterNamespace(ctx, req)
		if err == nil {
			t.Fatal("expected error on duplicate registration, got nil")
		}
		st, ok := status.FromError(err)
		if !ok {
			t.Fatalf("expected gRPC status error, got %v", err)
		}
		if st.Code() != codes.Internal {
			t.Errorf("expected code %v, got %v", codes.Internal, st.Code())
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

