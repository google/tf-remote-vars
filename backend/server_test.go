package backend

import (
	"testing"

	pb "github.com/google/varlet/proto/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
