package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"path"
	"time"

	pb "github.com/google/varlet/proto/v1"
	"github.com/jonboulle/clockwork"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

var webhookClient = &http.Client{
	Timeout: 5 * time.Second,
}

// Server implements the VarletService gRPC server.
type Server struct {
	pb.UnimplementedVarletServiceServer
	store Store
	clock clockwork.Clock
}

// NewServer creates a new Server with a real clock.
func NewServer(store Store) *Server {
	return &Server{
		store: store,
		clock: clockwork.NewRealClock(),
	}
}

// NewServerWithClock creates a new Server with a custom clock.
func NewServerWithClock(store Store, clock clockwork.Clock) *Server {
	return &Server{
		store: store,
		clock: clock,
	}
}

// RegisterNamespace registers a new namespace.
func (s *Server) RegisterNamespace(ctx context.Context, req *pb.RegisterNamespaceRequest) (*pb.RegisterNamespaceResponse, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace name cannot be empty")
	}

	ns := &Namespace{
		Name:          req.GetName(),
		RunWebhookURL: req.GetRunWebhookUrl(),
	}
	if req.GetRetentionPolicy() != nil {
		ns.RetentionPolicyMinVersions = req.GetRetentionPolicy().GetMinVersions()
		ns.RetentionPolicyMaxAgeDays = req.GetRetentionPolicy().GetMaxAgeDays()
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

	resp := &pb.GetNamespaceResponse{
		Name:             ns.Name,
		AllowedConsumers: ns.AllowedConsumers,
		RunWebhookUrl:    ns.RunWebhookURL,
	}
	if ns.RetentionPolicyMinVersions > 0 || ns.RetentionPolicyMaxAgeDays > 0 {
		resp.RetentionPolicy = &pb.RetentionPolicy{
			MinVersions: ns.RetentionPolicyMinVersions,
			MaxAgeDays:  ns.RetentionPolicyMaxAgeDays,
		}
	}

	return resp, nil
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
			CreatedAt: s.clock.Now(),
		}
		if err := s.store.PutVariable(ctx, v); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to store variable: %v", err)
		}

		// Enforce retention policy
		ns, err := s.store.GetNamespace(ctx, req.GetNamespace())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get namespace for retention check: %v", err)
		}

		if ns.RetentionPolicyMaxAgeDays > 0 {
			cutoff := s.clock.Now().AddDate(0, 0, -int(ns.RetentionPolicyMaxAgeDays))
			if err := s.store.PruneVariables(ctx, v.Namespace, v.Name, ns.RetentionPolicyMinVersions, cutoff); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to enforce retention policy: %v", err)
			}
		}
		// Trigger downstream webhooks if there are consumers
		hasCons, err := s.store.HasConsumers(ctx, v.Namespace, v.Name)
		if err == nil && hasCons {
			go s.propagateChange(context.Background(), v.Namespace, v.Name)
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

	// Check access policy
	if err := s.checkAccess(ctx, cNS, sNS); err != nil {
		return nil, err
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

	// Check access policy
	if err := s.checkAccess(ctx, cNS, sNS); err != nil {
		return nil, err
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

func (s *Server) SetNamespacePolicy(ctx context.Context, req *pb.SetNamespacePolicyRequest) (*pb.SetNamespacePolicyResponse, error) {
	ns := req.GetNamespace()
	allowed := req.GetAllowedConsumers()

	if ns == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace cannot be empty")
	}

	// Verify namespace exists
	_, err := s.store.GetNamespace(ctx, ns)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "namespace %q not found", ns)
		}
		return nil, status.Errorf(codes.Internal, "failed to check namespace: %v", err)
	}

	if err := s.store.SetNamespacePolicy(ctx, ns, allowed); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to set namespace policy: %v", err)
	}

	return &pb.SetNamespacePolicyResponse{}, nil
}

func (s *Server) checkAccess(ctx context.Context, consumerNS, sourceNS string) error {
	if consumerNS == sourceNS {
		return nil
	}

	ns, err := s.store.GetNamespace(ctx, sourceNS)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return status.Errorf(codes.NotFound, "source namespace %q not found", sourceNS)
		}
		return status.Errorf(codes.Internal, "failed to get source namespace: %v", err)
	}

	if len(ns.AllowedConsumers) == 0 {
		return nil // Open by default
	}

	for _, pattern := range ns.AllowedConsumers {
		matched, err := path.Match(pattern, consumerNS)
		if err != nil {
			continue
		}
		if matched {
			return nil
		}
	}

	return status.Errorf(codes.PermissionDenied, "namespace %q is not allowed to consume from %q", consumerNS, sourceNS)
}

// GetDependencyGraph returns the dependency graph, optionally filtered by a root namespace.
func (s *Server) GetDependencyGraph(ctx context.Context, req *pb.GetDependencyGraphRequest) (*pb.GetDependencyGraphResponse, error) {
	rootNS := req.GetNamespace()

	allNS, err := s.store.GetNamespaces(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get namespaces: %v", err)
	}

	allDeps, err := s.store.GetAllDependencies(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get dependencies: %v", err)
	}

	if rootNS == "" {
		respEdges := make([]*pb.DependencyEdge, len(allDeps))
		for i, d := range allDeps {
			respEdges[i] = &pb.DependencyEdge{
				ConsumerNamespace: d.Consumer,
				SourceNamespace:   d.Source,
				VariableName:     d.Variable,
			}
		}
		return &pb.GetDependencyGraphResponse{
			Namespaces: allNS,
			Edges:      respEdges,
		}, nil
	}

	foundRoot := false
	for _, ns := range allNS {
		if ns == rootNS {
			foundRoot = true
			break
		}
	}
	if !foundRoot {
		return nil, status.Errorf(codes.NotFound, "root namespace %q not found", rootNS)
	}

	// Upstream traversal (ancestors)
	adj := make(map[string][]string)
	for _, d := range allDeps {
		adj[d.Consumer] = append(adj[d.Consumer], d.Source)
	}

	visited := make(map[string]bool)
	queue := []string{rootNS}
	visited[rootNS] = true

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		for _, source := range adj[curr] {
			if !visited[source] {
				visited[source] = true
				queue = append(queue, source)
			}
		}
	}

	var respNS []string
	for _, ns := range allNS {
		if visited[ns] {
			respNS = append(respNS, ns)
		}
	}

	var respEdges []*pb.DependencyEdge
	for _, d := range allDeps {
		if visited[d.Consumer] {
			respEdges = append(respEdges, &pb.DependencyEdge{
				ConsumerNamespace: d.Consumer,
				SourceNamespace:   d.Source,
				VariableName:     d.Variable,
			})
		}
	}

	return &pb.GetDependencyGraphResponse{
		Namespaces: respNS,
		Edges:      respEdges,
	}, nil
}

func (s *Server) propagateChange(ctx context.Context, sourceNS, varName string) {
	consumers, err := s.store.GetConsumers(ctx, sourceNS, varName)
	if err != nil {
		log.Printf("[ERROR] failed to get consumers for %s/%s: %v", sourceNS, varName, err)
		return
	}

	for _, consumerNS := range consumers {
		ns, err := s.store.GetNamespace(ctx, consumerNS)
		if err != nil {
			log.Printf("[ERROR] failed to get namespace %s for webhook: %v", consumerNS, err)
			continue
		}
		if ns.RunWebhookURL == "" {
			continue
		}

		go s.callWebhook(ctx, ns.RunWebhookURL, consumerNS, sourceNS, varName)
	}
}

func (s *Server) callWebhook(ctx context.Context, url, consumerNS, sourceNS, varName string) {
	payload := struct {
		ConsumerNamespace string `json:"consumer_namespace"`
		SourceNamespace   string `json:"source_namespace"`
		VariableName     string `json:"variable_name"`
	}{
		ConsumerNamespace: consumerNS,
		SourceNamespace:   sourceNS,
		VariableName:     varName,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[ERROR] failed to marshal webhook payload: %v", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[ERROR] failed to create webhook request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := webhookClient.Do(req)
	if err != nil {
		log.Printf("[ERROR] webhook call to %s failed: %v", url, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[WARNING] webhook call to %s returned status %d", url, resp.StatusCode)
	}
}

