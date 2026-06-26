package provider

import (
	"context"

	pb "github.com/google/varlet/proto/v1"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Ensure VarletProvider satisfies various provider interfaces.
var _ provider.Provider = &VarletProvider{}

// VarletProvider defines the provider implementation.
type VarletProvider struct {
	version string
}

// VarletProviderModel describes the provider data model.
type VarletProviderModel struct {
	Endpoint  types.String `tfsdk:"endpoint"`
	Namespace types.String `tfsdk:"namespace"`
}

// VarletProviderData holds the configured client and metadata.
type VarletProviderData struct {
	Client    pb.VarletServiceClient
	Namespace string
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &VarletProvider{
			version: version,
		}
	}
}

func (p *VarletProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "varlet"
	resp.Version = p.version
}

func (p *VarletProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "The gRPC endpoint of the Varlet backend.",
				Optional:            true,
			},
			"namespace": schema.StringAttribute{
				MarkdownDescription: "The default namespace for this provider instance.",
				Optional:            true,
			},
		},
	}
}

func (p *VarletProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data VarletProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := "localhost:8080" // default
	if !data.Endpoint.IsNull() {
		endpoint = data.Endpoint.ValueString()
	}

	namespace := ""
	if !data.Namespace.IsNull() {
		namespace = data.Namespace.ValueString()
	}

	// Establish gRPC connection
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to create gRPC client",
			"An unexpected error occurred while creating the gRPC client: "+err.Error(),
		)
		return
	}

	client := pb.NewVarletServiceClient(conn)

	providerData := &VarletProviderData{
		Client:    client,
		Namespace: namespace,
	}

	resp.DataSourceData = providerData
	resp.ResourceData = providerData
}

func (p *VarletProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewNamespaceDataSource,
	}
}

func (p *VarletProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewOutputResource,
		NewInputResource,
	}
}
