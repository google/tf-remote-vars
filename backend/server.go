package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"path"
	"sync"
	"time"

	pb "github.com/google/varlet/proto/v1"
	"github.com/jonboulle/clockwork"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

var webhookClient = &http.Client{
	Timeout: 5 * time.Second,
}

type NamespaceStatus string

const (
	StatusIdle                NamespaceStatus = "idle"
	StatusActuating           NamespaceStatus = "actuating"
	StatusSucceeded           NamespaceStatus = "succeeded"
	StatusAffected            NamespaceStatus = "affected"
	StatusPotentiallyAffected NamespaceStatus = "potentially-affected"
)

type debounceState struct {
	timer            interface{ Stop() bool }
	changedVariables map[string]bool
}

// Server implements the VarletService gRPC server.
type Server struct {
	pb.UnimplementedVarletServiceServer
	store Store
	clock clockwork.Clock

	mu             sync.Mutex
	actuating      map[string]time.Time
	affected       map[string]bool
	succeeded      map[string]bool
	debounceTimers map[string]*debounceState
}

func newServer(store Store, clock clockwork.Clock) *Server {
	return &Server{
		store:          store,
		clock:          clock,
		actuating:      make(map[string]time.Time),
		affected:       make(map[string]bool),
		succeeded:      make(map[string]bool),
		debounceTimers: make(map[string]*debounceState),
	}
}

// NewServer creates a new Server with a real clock.
func NewServer(store Store) *Server {
	return newServer(store, clockwork.NewRealClock())
}

// NewServerWithClock creates a new Server with a custom clock.
func NewServerWithClock(store Store, clock clockwork.Clock) *Server {
	return newServer(store, clock)
}

// RegisterNamespace registers a new namespace.
func (s *Server) RegisterNamespace(ctx context.Context, req *pb.RegisterNamespaceRequest) (*pb.RegisterNamespaceResponse, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace name cannot be empty")
	}

	if req.GetWebhookDelayMinutes() < 0 {
		return nil, status.Error(codes.InvalidArgument, "webhook delay minutes cannot be negative")
	}

	ns := &Namespace{
		Name:                req.GetName(),
		RunWebhookURL:       req.GetRunWebhookUrl(),
		WebhookDelayMinutes: req.GetWebhookDelayMinutes(),
	}
	if req.GetRetentionPolicy() != nil {
		ns.RetentionPolicyMinVersions = req.GetRetentionPolicy().GetMinVersions()
		ns.RetentionPolicyMaxAgeDays = req.GetRetentionPolicy().GetMaxAgeDays()
	}

	if err := s.store.RegisterNamespace(ctx, ns); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to register namespace: %v", err)
	}

	s.markActuating(ns.Name)

	return &pb.RegisterNamespaceResponse{
		Name: ns.Name,
	}, nil
}

func (s *Server) StartActuation(ctx context.Context, req *pb.StartActuationRequest) (*pb.StartActuationResponse, error) {
	ns := req.GetNamespace()
	if ns == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace cannot be empty")
	}

	s.markActuating(ns)

	return &pb.StartActuationResponse{}, nil
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
		Name:                ns.Name,
		AllowedConsumers:    ns.AllowedConsumers,
		RunWebhookUrl:       ns.RunWebhookURL,
		WebhookDelayMinutes: ns.WebhookDelayMinutes,
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

		s.debounceSucceeded(v.Namespace, v.Name, true)

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
	} else {
		s.debounceSucceeded(req.GetNamespace(), "", false)
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



	for _, pattern := range ns.AllowedConsumers {
		if pattern == "*" {
			return nil
		}
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
		statuses, err := s.calculateStatuses(ctx, allNS)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to calculate statuses: %v", err)
		}
		return &pb.GetDependencyGraphResponse{
			Namespaces: allNS,
			Edges:      respEdges,
			Statuses:   statuses,
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

	// Build adjacency maps
	downstream := make(map[string][]string)
	upstream := make(map[string][]string)
	for _, d := range allDeps {
		downstream[d.Source] = append(downstream[d.Source], d.Consumer)
		upstream[d.Consumer] = append(upstream[d.Consumer], d.Source)
	}

	targetNodes := make(map[string]bool)
	targetNodes[rootNS] = true

	// 1. Find downstream descendants (unlimited depth)
	queue := []string{rootNS}
	visitedDownstream := make(map[string]bool)
	visitedDownstream[rootNS] = true

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		for _, consumer := range downstream[curr] {
			if !visitedDownstream[consumer] {
				visitedDownstream[consumer] = true
				targetNodes[consumer] = true
				queue = append(queue, consumer)
			}
		}
	}

	// 2. Find upstream ancestors (limited to req.GetUpstreamDepth() unless < 0)
	upstreamDepth := req.GetUpstreamDepth()
	if upstreamDepth != 0 {
		type queueItem struct {
			node  string
			depth int32
		}
		uQueue := []queueItem{{node: rootNS, depth: 0}}
		visitedUpstream := make(map[string]bool)
		visitedUpstream[rootNS] = true

		for len(uQueue) > 0 {
			curr := uQueue[0]
			uQueue = uQueue[1:]

			if upstreamDepth > 0 && curr.depth >= upstreamDepth {
				continue
			}

			for _, source := range upstream[curr.node] {
				if !visitedUpstream[source] {
					visitedUpstream[source] = true
					targetNodes[source] = true
					uQueue = append(uQueue, queueItem{node: source, depth: curr.depth + 1})
				}
			}
		}
	}

	// Filter namespaces and edges
	var respNS []string
	for _, ns := range allNS {
		if targetNodes[ns] {
			respNS = append(respNS, ns)
		}
	}

	var respEdges []*pb.DependencyEdge
	for _, d := range allDeps {
		if targetNodes[d.Consumer] && targetNodes[d.Source] {
			respEdges = append(respEdges, &pb.DependencyEdge{
				ConsumerNamespace: d.Consumer,
				SourceNamespace:   d.Source,
				VariableName:     d.Variable,
			})
		}
	}

	statuses, err := s.calculateStatuses(ctx, respNS)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to calculate statuses: %v", err)
	}

	return &pb.GetDependencyGraphResponse{
		Namespaces: respNS,
		Edges:      respEdges,
		Statuses:   statuses,
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

		go s.callWebhook(ctx, ns.RunWebhookURL, consumerNS, sourceNS, varName, ns.WebhookDelayMinutes)
	}
}

func (s *Server) callWebhook(ctx context.Context, url, consumerNS, sourceNS, varName string, delay int32) {
	if delay > 0 {
		s.clock.Sleep(time.Duration(delay) * time.Minute)
	}
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

// AuditInterceptor returns a gRPC unary server interceptor that logs state-changing operations to the store.
func AuditInterceptor(store Store, clock clockwork.Clock) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			return resp, err
		}

		action := ""
		target := ""

		switch info.FullMethod {
		case "/varlet.v1.VarletService/RegisterNamespace":
			action = "RegisterNamespace"
			if r, ok := req.(*pb.RegisterNamespaceRequest); ok {
				target = r.GetName()
			}
		case "/varlet.v1.VarletService/SetNamespacePolicy":
			action = "SetNamespacePolicy"
			if r, ok := req.(*pb.SetNamespacePolicyRequest); ok {
				target = r.GetNamespace()
			}
		case "/varlet.v1.VarletService/PutVariable":
			action = "PutVariable"
			if r, ok := req.(*pb.PutVariableRequest); ok {
				target = r.GetNamespace() + "/" + r.GetName()
			}
		case "/varlet.v1.VarletService/DeleteVariable":
			action = "DeleteVariable"
			if r, ok := req.(*pb.DeleteVariableRequest); ok {
				target = r.GetNamespace() + "/" + r.GetName()
			}
		case "/varlet.v1.VarletService/RegisterConsumer":
			action = "RegisterConsumer"
			if r, ok := req.(*pb.RegisterConsumerRequest); ok {
				target = r.GetConsumerNamespace() + " -> " + r.GetSourceNamespace() + "/" + r.GetVariableName()
			}
		case "/varlet.v1.VarletService/DeregisterConsumer":
			action = "DeregisterConsumer"
			if r, ok := req.(*pb.DeregisterConsumerRequest); ok {
				target = r.GetConsumerNamespace() + " -> " + r.GetSourceNamespace() + "/" + r.GetVariableName()
			}
		}

		if action != "" {
			actor := "anonymous"
			if md, ok := metadata.FromIncomingContext(ctx); ok {
				if actors := md.Get("x-actor"); len(actors) > 0 {
					actor = actors[0]
				}
			}

			var details string
			if protoReq, ok := req.(proto.Message); ok {
				if b, err := protojson.Marshal(protoReq); err == nil {
					details = string(b)
				}
			}

			auditLog := &AuditLog{
				Timestamp: clock.Now(),
				Actor:     actor,
				Action:    action,
				Target:    target,
				Details:   details,
			}
			if err := store.WriteAuditLog(ctx, auditLog); err != nil {
				log.Printf("[ERROR] failed to write audit log: %v", err)
			}
		}

		return resp, err
	}
}

func (s *Server) markActuating(ns string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actuating[ns] = s.clock.Now()
	delete(s.affected, ns)
	delete(s.succeeded, ns)
}

func (s *Server) debounceSucceeded(ns string, varName string, changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var state *debounceState
	if existing, ok := s.debounceTimers[ns]; ok {
		existing.timer.Stop()
		state = existing
	} else {
		state = &debounceState{
			changedVariables: make(map[string]bool),
		}
	}

	if changed && varName != "" {
		state.changedVariables[varName] = true
	}

	timer := s.clock.AfterFunc(2*time.Second, func() {
		s.mu.Lock()
		delete(s.actuating, ns)
		s.succeeded[ns] = true
		
		varsToPropagate := make([]string, 0, len(state.changedVariables))
		for v := range state.changedVariables {
			varsToPropagate = append(varsToPropagate, v)
		}
		
		delete(s.debounceTimers, ns)
		s.mu.Unlock()

		if len(varsToPropagate) > 0 {
			if err := s.propagateAffected(context.Background(), ns); err != nil {
				log.Printf("[WARNING] failed to propagate affected state from %s: %v", ns, err)
			}
			
			for _, vName := range varsToPropagate {
				hasCons, err := s.store.HasConsumers(context.Background(), ns, vName)
				if err == nil && hasCons {
					go s.propagateChange(context.Background(), ns, vName)
				}
			}
		}
	})
	state.timer = timer
	s.debounceTimers[ns] = state
}

func (s *Server) propagateAffected(ctx context.Context, sourceNS string) error {
	allDeps, err := s.store.GetAllDependencies(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, d := range allDeps {
		if d.Source == sourceNS {
			s.affected[d.Consumer] = true
			delete(s.succeeded, d.Consumer)
		}
	}

	return nil
}

func (s *Server) hasActuatingAncestor(ns string, actuating map[string]time.Time, upstream map[string][]string) bool {
	queue := []string{ns}
	visited := make(map[string]bool)
	visited[ns] = true

	first := true

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if !first {
			if _, ok := actuating[curr]; ok {
				return true
			}
		}
		first = false

		for _, parent := range upstream[curr] {
			if !visited[parent] {
				visited[parent] = true
				queue = append(queue, parent)
			}
		}
	}

	return false
}

func (s *Server) handleTimeouts(ctx context.Context) {
	s.mu.Lock()
	candidates := make(map[string]time.Time)
	for k, v := range s.actuating {
		candidates[k] = v
	}
	s.mu.Unlock()

	now := s.clock.Now()
	shortTimeout := 45 * time.Second
	longTimeout := 5 * time.Minute

	var toSucceed []string
	for ns, startTime := range candidates {
		elapsed := now.Sub(startTime)
		if elapsed > shortTimeout {
			hasVars, err := s.store.HasVariables(ctx, ns)
			if err != nil {
				log.Printf("[WARNING] failed to check if namespace %s has variables: %v", ns, err)
				continue
			}
			if !hasVars {
				toSucceed = append(toSucceed, ns)
			} else if elapsed > longTimeout {
				log.Printf("[INFO] Actuation for namespace %s (with outputs) timed out after long duration, marking as succeeded", ns)
				toSucceed = append(toSucceed, ns)
			}
		}
	}

	if len(toSucceed) > 0 {
		s.mu.Lock()
		for _, ns := range toSucceed {
			if _, ok := s.actuating[ns]; ok {
				log.Printf("[INFO] Marking namespace %s as succeeded due to timeout", ns)
				delete(s.actuating, ns)
				s.succeeded[ns] = true
			}
		}
		s.mu.Unlock()
	}
}

func (s *Server) calculateStatuses(ctx context.Context, namespaces []string) (map[string]string, error) {
	s.handleTimeouts(ctx)

	allDeps, err := s.store.GetAllDependencies(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get dependencies for status calculation: %v", err)
	}

	upstream := make(map[string][]string)
	for _, d := range allDeps {
		upstream[d.Consumer] = append(upstream[d.Consumer], d.Source)
	}

	s.mu.Lock()
	actuatingCopy := make(map[string]time.Time)
	for k, v := range s.actuating {
		actuatingCopy[k] = v
	}
	affectedCopy := make(map[string]bool)
	for k, v := range s.affected {
		affectedCopy[k] = v
	}
	succeededCopy := make(map[string]bool)
	for k, v := range s.succeeded {
		succeededCopy[k] = v
	}
	s.mu.Unlock()

	statuses := make(map[string]string)
	for _, ns := range namespaces {
		if _, ok := actuatingCopy[ns]; ok {
			statuses[ns] = string(StatusActuating)
		} else if affectedCopy[ns] {
			statuses[ns] = string(StatusAffected)
		} else {
			if s.hasActuatingAncestor(ns, actuatingCopy, upstream) {
				statuses[ns] = string(StatusPotentiallyAffected)
			} else if succeededCopy[ns] {
				statuses[ns] = string(StatusSucceeded)
			} else {
				statuses[ns] = string(StatusIdle)
			}
		}
	}
	return statuses, nil
}

