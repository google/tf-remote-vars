package provider

import (
	"fmt"
	"net"
	"regexp"
	"testing"

	"github.com/google/varlet/backend"
	pb "github.com/google/varlet/proto/v1"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"google.golang.org/grpc"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"varlet": providerserver.NewProtocol6WithError(New("test")()),
}

func startTestServer(t *testing.T) (string, backend.Store) {
	t.Helper()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	store, err := backend.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	grpcServer := grpc.NewServer()
	server := backend.NewServer(store)
	pb.RegisterVarletServiceServer(grpcServer, server)

	go func() {
		_ = grpcServer.Serve(lis)
	}()

	t.Cleanup(func() {
		grpcServer.GracefulStop()
		store.Close()
	})

	return lis.Addr().String(), store
}

func TestAccNamespaceDataSourceSuccess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	addr, store := startTestServer(t)

	// Pre-register namespace
	err := store.RegisterNamespace(ctx, &backend.Namespace{Name: "test-ns"})
	if err != nil {
		t.Fatalf("failed to pre-register namespace: %v", err)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "varlet" {
  endpoint = %q
}

data "varlet_namespace" "test" {
  name = "test-ns"
}
`, addr),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.varlet_namespace.test", "name", "test-ns"),
					resource.TestCheckResourceAttr("data.varlet_namespace.test", "id", "test-ns"),
				),
			},
		},
	})
}

func TestAccNamespaceDataSourceNotFound(t *testing.T) {
	t.Parallel()
	addr, _ := startTestServer(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "varlet" {
  endpoint = %q
}

data "varlet_namespace" "test" {
  name = "non-existent"
}
`, addr),
				ExpectError: regexp.MustCompile("Could not find namespace"),
			},
		},
	})
}
