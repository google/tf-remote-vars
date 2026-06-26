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
	ns := req.GetNamespace()
	name := req.GetName()

	if ns == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace cannot be empty")
	}
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "name cannot be empty")
	}

	if !req.GetForce() {
		hasCons, err := s.store.HasConsumers(ctx, ns, name)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to check active consumers: %v", err)
		}
		if hasCons {
			return nil, status.Error(codes.FailedPrecondition, "cannot delete variable with active consumers")
		}
	}

	if err := s.store.DeleteVariable(ctx, ns, name); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete variable: %v", err)
	}

	return &pb.DeleteVariableResponse{}, nil
}

// RegisterConsumer registers a consumer for a variable.
func (s *Server) RegisterConsumer(ctx context.Context, req *pb.RegisterConsumerRequest) (*pb.RegisterConsumerResponse, error) {
	cNS := req.GetConsumerNamespace()
	sNS := req.GetSourceNamespace()
	varName := req.GetVariableName()

	if cNS == "" || sNS == "" || varName == "" {
		return nil, status.Error(codes.InvalidArgument, "consumer_namespace, source_namespace, and variable_name cannot be empty")
	}

	// Verify consumer namespace exists
	_, err := s.store.GetNamespace(ctx, cNS)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, status.Errorf(codes.InvalidArgument, "consumer namespace %q does not exist", cNS)
		}
		return nil, status.Errorf(codes.Internal, "failed to check consumer namespace: %v", err)
	}

	// Verify variable exists first
	v, err := s.store.GetLatestVariable(ctx, sNS, varName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, status.Errorf(codes.FailedPrecondition, "variable %s/%s does not exist", sNS, varName)
		}
		return nil, status.Errorf(codes.Internal, "failed to check variable existence: %v", err)
	}

	// Check for cycles
	cycle, err := s.hasCycle(ctx, cNS, sNS)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to detect cycles: %v", err)
	}
	if cycle {
		return nil, status.Error(codes.FailedPrecondition, "registering this dependency would introduce a cycle")
	}

	// Register consumer
	if err := s.store.RegisterConsumer(ctx, cNS, sNS, varName); err != nil {
		isCons, err2 := s.store.IsConsumer(ctx, cNS, sNS, varName)
		if err2 == nil && isCons {
			// Already registered, just proceed to return value (idempotent)
		} else {
			return nil, status.Errorf(codes.Internal, "failed to register consumer: %v", err)
		}
	}

	// Return value and nonce (version)
	var pbVal structpb.Value
	if err := protojson.Unmarshal(v.Value, &pbVal); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to unmarshal variable value: %v", err)
	}

	return &pb.RegisterConsumerResponse{
		Value:          &pbVal,
		ActuationNonce: v.Version,
	}, nil
}

// DeregisterConsumer deregisters a consumer for a variable.
func (s *Server) DeregisterConsumer(ctx context.Context, req *pb.DeregisterConsumerRequest) (*pb.DeregisterConsumerResponse, error) {
	cNS := req.GetConsumerNamespace()
	sNS := req.GetSourceNamespace()
	varName := req.GetVariableName()

	if cNS == "" || sNS == "" || varName == "" {
		return nil, status.Error(codes.InvalidArgument, "consumer_namespace, source_namespace, and variable_name cannot be empty")
	}

	if err := s.store.DeregisterConsumer(ctx, cNS, sNS, varName); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to deregister consumer: %v", err)
	}

	return &pb.DeregisterConsumerResponse{}, nil
}

// GetVariableValue retrieves the value of a variable for a consumer.
func (s *Server) GetVariableValue(ctx context.Context, req *pb.GetVariableValueRequest) (*pb.GetVariableValueResponse, error) {
	cNS := req.GetConsumerNamespace()
	sNS := req.GetSourceNamespace()
	varName := req.GetVariableName()

	if cNS == "" || sNS == "" || varName == "" {
		return nil, status.Error(codes.InvalidArgument, "consumer_namespace, source_namespace, and variable_name cannot be empty")
	}

	// Verify registration
	isCons, err := s.store.IsConsumer(ctx, cNS, sNS, varName)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check registration: %v", err)
	}
	if !isCons {
		return nil, status.Errorf(codes.FailedPrecondition, "consumer %s is not registered for variable %s/%s", cNS, sNS, varName)
	}

	// Get latest value
	v, err := s.store.GetLatestVariable(ctx, sNS, varName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "variable %s/%s not found", sNS, varName)
		}
		return nil, status.Errorf(codes.Internal, "failed to get variable: %v", err)
	}

	var pbVal structpb.Value
	if err := protojson.Unmarshal(v.Value, &pbVal); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to unmarshal variable value: %v", err)
	}

	return &pb.GetVariableValueResponse{
		Value:          &pbVal,
		ActuationNonce: v.Version,
	}, nil
}

func (s *Server) hasCycle(ctx context.Context, startNS, targetNS string) (bool, error) {
	visited := make(map[string]bool)
	var dfs func(ns string) (bool, error)
	dfs = func(ns string) (bool, error) {
		if ns == startNS {
			return true, nil
		}
		if visited[ns] {
			return false, nil
		}
		visited[ns] = true
		deps, err := s.store.GetDependencies(ctx, ns)
		if err != nil {
			return false, err
		}
		for _, dep := range deps {
			found, err := dfs(dep)
			if err != nil {
				return false, err
			}
			if found {
				return true, nil
			}
		}
		return false, nil
	}
	return dfs(targetNS)
}

