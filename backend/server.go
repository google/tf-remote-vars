package backend

import (
	"context"
	"errors"

	pb "github.com/google/varlet/proto/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// Server implements the VarletService gRPC server.
type Server struct {
	pb.UnimplementedVarletServiceServer
	store Store
}

// NewServer creates a new Server.
func NewServer(store Store) *Server {
	return &Server{store: store}
}

// RegisterNamespace registers a new namespace.
func (s *Server) RegisterNamespace(ctx context.Context, req *pb.RegisterNamespaceRequest) (*pb.RegisterNamespaceResponse, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace name cannot be empty")
	}

	ns := &Namespace{
		Name: req.GetName(),
	}

	if err := s.store.RegisterNamespace(ctx, ns); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to register namespace: %v", err)
	}

	return &pb.RegisterNamespaceResponse{
		Name: ns.Name,
	}, nil
}

// GetNamespace retrieves a namespace.
func (s *Server) GetNamespace(ctx context.Context, req *pb.GetNamespaceRequest) (*pb.GetNamespaceResponse, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace name cannot be empty")
	}

	ns, err := s.store.GetNamespace(ctx, req.GetName())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "namespace %q not found", req.GetName())
		}
		return nil, status.Errorf(codes.Internal, "failed to get namespace: %v", err)
	}

	return &pb.GetNamespaceResponse{
		Name: ns.Name,
		// ponytail: only name is returned for now, other fields will be added in Slice 4.
	}, nil
}

// PutVariable stores a new variable version.
func (s *Server) PutVariable(ctx context.Context, req *pb.PutVariableRequest) (*pb.PutVariableResponse, error) {
	if req.GetNamespace() == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace cannot be empty")
	}
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name cannot be empty")
	}
	if req.GetValue() == nil {
		return nil, status.Error(codes.InvalidArgument, "value cannot be nil")
	}

	latest, err := s.store.GetLatestVariable(ctx, req.GetNamespace(), req.GetName())
	isNotFound := errors.Is(err, ErrNotFound)
	if err != nil && !isNotFound {
		return nil, status.Errorf(codes.Internal, "failed to get latest variable: %v", err)
	}

	newValueBytes, err := protojson.Marshal(req.GetValue())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal value: %v", err)
	}

	var version int64 = 1
	shouldWrite := false

	if isNotFound {
		shouldWrite = true
	} else {
		var oldVal structpb.Value
		if err := protojson.Unmarshal(latest.Value, &oldVal); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to unmarshal old value: %v", err)
		}

		if !proto.Equal(req.GetValue(), &oldVal) || req.GetForceActuation() {
			version = latest.Version + 1
			shouldWrite = true
		}
	}

	if shouldWrite {
		v := &Variable{
			Namespace: req.GetNamespace(),
			Name:      req.GetName(),
			Version:   version,
			Value:     newValueBytes,
		}
		if err := s.store.PutVariable(ctx, v); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to store variable: %v", err)
		}
	}

	return &pb.PutVariableResponse{}, nil
}

// DeleteVariable deletes a variable.
func (s *Server) DeleteVariable(ctx context.Context, req *pb.DeleteVariableRequest) (*pb.DeleteVariableResponse, error) {
	if req.GetNamespace() == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace cannot be empty")
	}
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name cannot be empty")
	}

	if err := s.store.DeleteVariable(ctx, req.GetNamespace(), req.GetName()); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete variable: %v", err)
	}

	return &pb.DeleteVariableResponse{}, nil
}

