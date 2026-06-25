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
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
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

func TestAccOutputResource(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	addr, store := startTestServer(t)

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
  endpoint  = %q
  namespace = "test-ns"
}

resource "varlet_output" "str" {
  name  = "my_str"
  value = "hello"
}
`, addr),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("varlet_output.str", "id", "test-ns/my_str"),
					resource.TestCheckResourceAttr("varlet_output.str", "namespace", "test-ns"),
					resource.TestCheckResourceAttr("varlet_output.str", "name", "my_str"),
					resource.TestCheckResourceAttr("varlet_output.str", "value", "hello"),
					resource.TestCheckResourceAttr("varlet_output.str", "force_actuation", "false"),
					func(s *terraform.State) error {
						v, err := store.GetLatestVariable(ctx, "test-ns", "my_str")
						if err != nil {
							return fmt.Errorf("failed to get variable from DB: %w", err)
						}
						if v.Version != 1 {
							return fmt.Errorf("expected version 1, got %d", v.Version)
						}
						var gotVal structpb.Value
						if err := protojson.Unmarshal(v.Value, &gotVal); err != nil {
							return fmt.Errorf("failed to unmarshal value: %w", err)
						}
						if gotVal.GetStringValue() != "hello" {
							return fmt.Errorf("expected hello, got %s", gotVal.GetStringValue())
						}
						return nil
					},
				),
			},
			{
				Config: fmt.Sprintf(`
provider "varlet" {
  endpoint  = %q
  namespace = "test-ns"
}

resource "varlet_output" "str" {
  name  = "my_str"
  value = "world"
}
`, addr),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("varlet_output.str", "value", "world"),
					func(s *terraform.State) error {
						v, err := store.GetLatestVariable(ctx, "test-ns", "my_str")
						if err != nil {
							return fmt.Errorf("failed to get variable from DB: %w", err)
						}
						if v.Version != 2 {
							return fmt.Errorf("expected version 2, got %d", v.Version)
						}
						var gotVal structpb.Value
						if err := protojson.Unmarshal(v.Value, &gotVal); err != nil {
							return fmt.Errorf("failed to unmarshal value: %w", err)
						}
						if gotVal.GetStringValue() != "world" {
							return fmt.Errorf("expected world, got %s", gotVal.GetStringValue())
						}
						return nil
					},
				),
			},
			{
				Config: fmt.Sprintf(`
provider "varlet" {
  endpoint  = %q
  namespace = "test-ns"
}

resource "varlet_output" "list" {
  name  = "my_list"
  value = ["a", "b"]
}

resource "varlet_output" "map" {
  name  = "my_map"
  value = {
    key1 = "val1"
  }
}
`, addr),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("varlet_output.list", "value.0", "a"),
					resource.TestCheckResourceAttr("varlet_output.list", "value.1", "b"),
					resource.TestCheckResourceAttr("varlet_output.map", "value.key1", "val1"),
					func(s *terraform.State) error {
						v, err := store.GetLatestVariable(ctx, "test-ns", "my_list")
						if err != nil {
							return fmt.Errorf("failed to get list from DB: %w", err)
						}
						var gotList structpb.Value
						if err := protojson.Unmarshal(v.Value, &gotList); err != nil {
							return fmt.Errorf("failed to unmarshal list: %w", err)
						}
						list := gotList.GetListValue()
						if list == nil || len(list.Values) != 2 || list.Values[0].GetStringValue() != "a" || list.Values[1].GetStringValue() != "b" {
							return fmt.Errorf("unexpected list value: %v", gotList.String())
						}

						v, err = store.GetLatestVariable(ctx, "test-ns", "my_map")
						if err != nil {
							return fmt.Errorf("failed to get map from DB: %w", err)
						}
						var gotMap structpb.Value
						if err := protojson.Unmarshal(v.Value, &gotMap); err != nil {
							return fmt.Errorf("failed to unmarshal map: %w", err)
						}
						strct := gotMap.GetStructValue()
						if strct == nil || strct.Fields["key1"].GetStringValue() != "val1" {
							return fmt.Errorf("unexpected map value: %v", gotMap.String())
						}
						return nil
					},
				),
			},
		},
	})
}
