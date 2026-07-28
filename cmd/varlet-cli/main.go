package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	pb "github.com/google/varlet/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	serverAddr := flag.String("server", "localhost:8080", "Varlet server address (host:port)")
	rootNS := flag.String("root", "", "Root namespace for filtering")
	upstreamDepth := flag.Int("upstream-depth", 0, "Upstream depth (parents) to include")
	flag.Parse()

	// Connect to server
	conn, err := grpc.Dial(*serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to connect to server: %v", err)
	}
	defer conn.Close()

	client := pb.NewVarletServiceClient(conn)

	// Fetch graph
	resp, err := client.GetDependencyGraph(context.Background(), &pb.GetDependencyGraphRequest{
		Namespace:     *rootNS,
		UpstreamDepth: int32(*upstreamDepth),
	})
	if err != nil {
		log.Fatalf("failed to get dependency graph: %v", err)
	}

	// Output DOT
	fmt.Println("digraph G {")
	fmt.Println("  rankdir=TB;")
	fmt.Println("  node [shape=box, style=rounded];")
	fmt.Println()

	// Nodes
	for _, ns := range resp.GetNamespaces() {
		escaped := escape(ns)
		fmt.Printf("  \"%s\" [label=\"%s\"];\n", escaped, escaped)
	}
	fmt.Println()

	// Edges (data flow: source -> consumer)
	for _, edge := range resp.GetEdges() {
		src := escape(edge.GetSourceNamespace())
		cons := escape(edge.GetConsumerNamespace())
		label := escape(edge.GetVariableName())
		fmt.Printf("  \"%s\" -> \"%s\" [label=\"%s\"];\n", src, cons, label)
	}

	fmt.Println("}")
}

func escape(s string) string {
	return strings.ReplaceAll(s, "\"", "\\\"")
}

