package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strconv"

	"github.com/google/varlet/backend"
	pb "github.com/google/varlet/proto/v1"
	"github.com/jonboulle/clockwork"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
)

//go:embed web/*
var webFiles embed.FS

func main() {
	grpcPort := flag.Int("grpc-port", 8080, "Port for gRPC server")
	httpPort := flag.Int("http-port", 8081, "Port for HTTP server/UI")
	dbPath := flag.String("db-path", "varlet.db", "Path to SQLite database")
	flag.Parse()

	// Initialize store
	store, err := backend.NewSQLiteStore(*dbPath)
	if err != nil {
		log.Fatalf("failed to initialize store: %v", err)
	}
	defer store.Close()

	// Initialize server logic
	serverLogic := backend.NewServer(store)

	// Start gRPC server
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *grpcPort))
	if err != nil {
		log.Fatalf("failed to listen for gRPC: %v", err)
	}

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(backend.AuditInterceptor(store, clockwork.NewRealClock())),
	)
	pb.RegisterVarletServiceServer(grpcServer, serverLogic)

	go func() {
		log.Printf("Starting gRPC server on :%d", *grpcPort)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("gRPC server failed: %v", err)
		}
	}()

	// Start HTTP server
	mux := http.NewServeMux()

	// JSON API for graph
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		root := r.URL.Query().Get("root")
		depthStr := r.URL.Query().Get("upstream_depth")
		var depth int32
		if depthStr != "" {
			d, err := strconv.ParseInt(depthStr, 10, 32)
			if err != nil {
				http.Error(w, "Invalid upstream_depth", http.StatusBadRequest)
				return
			}
			depth = int32(d)
		}

		resp, err := serverLogic.GetDependencyGraph(r.Context(), &pb.GetDependencyGraphRequest{
			Namespace:     root,
			UpstreamDepth: depth,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get graph: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		m := protojson.MarshalOptions{EmitUnpopulated: true}
		jsonBytes, err := m.Marshal(resp)
		if err != nil {
			http.Error(w, "Failed to marshal response", http.StatusInternalServerError)
			return
		}
		w.Write(jsonBytes)
	})

	// Serve UI files
	subFS, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatalf("failed to create sub FS: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(subFS)))

	log.Printf("Starting HTTP server on :%d", *httpPort)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *httpPort), mux); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}
