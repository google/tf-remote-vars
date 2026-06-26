package provider

import (
	"context"
	"fmt"

	pb "github.com/google/varlet/proto/v1"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &NamespaceResource{}
var _ resource.ResourceWithConfigure = &NamespaceResource{}
var _ resource.ResourceWithImportState = &NamespaceResource{}

func NewNamespaceResource() resource.Resource {
	return &NamespaceResource{}
}

// NamespaceResource defines the resource implementation.
type NamespaceResource struct {
	client           pb.VarletServiceClient
	defaultNamespace string // Not used for namespace resource itself, but part of VarletProviderData
}

// NamespaceResourceModel describes the resource data model.
type NamespaceResourceModel struct {
	ID               types.String                 `tfsdk:"id"`
	Name             types.String                 `tfsdk:"name"`
	RunWebhookURL    types.String                 `tfsdk:"run_webhook_url"`
	AllowedConsumers types.Set                    `tfsdk:"allowed_consumers"`
	RetentionPolicy  *RetentionPolicyProductModel `tfsdk:"retention_policy"`
}

type RetentionPolicyProductModel struct {
	MinVersions types.Int64 `tfsdk:"min_versions"`
	MaxAgeDays  types.Int64 `tfsdk:"max_age_days"`
}

func (r *NamespaceResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_namespace"
}

func (r *NamespaceResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Varlet namespace, its access policies, and retention policies.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The ID of the namespace (same as name).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The name of the namespace.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"run_webhook_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The URL of the webhook to trigger on changes.",
			},
			"allowed_consumers": schema.SetAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "List of consumer namespace patterns allowed to consume from this namespace.",
			},
			"retention_policy": schema.SingleNestedAttribute{
				Optional:            true,
				MarkdownDescription: "The retention policy for variables in this namespace.",
				Attributes: map[string]schema.Attribute{
					"min_versions": schema.Int64Attribute{
						Optional:            true,
						Computed:            true,
						Default:             int64default.StaticInt64(0),
						MarkdownDescription: "Minimum number of versions to keep.",
					},
					"max_age_days": schema.Int64Attribute{
						Optional:            true,
						Computed:            true,
						Default:             int64default.StaticInt64(0),
						MarkdownDescription: "Maximum age of versions in days.",
					},
				},
			},
		},
	}
}

func (r *NamespaceResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	data, ok := req.ProviderData.(*VarletProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *VarletProviderData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = data.Client
	r.defaultNamespace = data.Namespace
}

func (r *NamespaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data NamespaceResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Register namespace
	registerReq := &pb.RegisterNamespaceRequest{
		Name:          data.Name.ValueString(),
		RunWebhookUrl: data.RunWebhookURL.ValueString(),
	}
	if data.RetentionPolicy != nil {
		registerReq.RetentionPolicy = &pb.RetentionPolicy{
			MinVersions: int32(data.RetentionPolicy.MinVersions.ValueInt64()),
			MaxAgeDays:  int32(data.RetentionPolicy.MaxAgeDays.ValueInt64()),
		}
	}

	_, err := r.client.RegisterNamespace(ctx, registerReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to register namespace", err.Error())
		return
	}

	// Set policy if allowed_consumers is configured
	if !data.AllowedConsumers.IsNull() && !data.AllowedConsumers.IsUnknown() {
		var allowed []string
		resp.Diagnostics.Append(data.AllowedConsumers.ElementsAs(ctx, &allowed, false)...)
		if resp.Diagnostics.HasError() {
			return
		}

		_, err = r.client.SetNamespacePolicy(ctx, &pb.SetNamespacePolicyRequest{
			Namespace:        data.Name.ValueString(),
			AllowedConsumers: allowed,
		})
		if err != nil {
			resp.Diagnostics.AddError("Failed to set namespace policy", err.Error())
			return
		}
	}

	data.ID = data.Name

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *NamespaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data NamespaceResourceModel

	// Read Terraform state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get namespace from backend
	ns, err := r.client.GetNamespace(ctx, &pb.GetNamespaceRequest{
		Name: data.Name.ValueString(),
	})
	if err != nil {
		if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read namespace", err.Error())
		return
	}

	if ns.GetRunWebhookUrl() == "" {
		data.RunWebhookURL = types.StringNull()
	} else {
		data.RunWebhookURL = types.StringValue(ns.GetRunWebhookUrl())
	}

	if len(ns.GetAllowedConsumers()) > 0 {
		allowedSet, diags := types.SetValueFrom(ctx, types.StringType, ns.GetAllowedConsumers())
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.AllowedConsumers = allowedSet
	} else {
		data.AllowedConsumers = types.SetNull(types.StringType)
	}

	if ns.GetRetentionPolicy() != nil {
		data.RetentionPolicy = &RetentionPolicyProductModel{
			MinVersions: types.Int64Value(int64(ns.GetRetentionPolicy().GetMinVersions())),
			MaxAgeDays:  types.Int64Value(int64(ns.GetRetentionPolicy().GetMaxAgeDays())),
		}
	} else {
		data.RetentionPolicy = nil
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *NamespaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data NamespaceResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Register namespace (acts as upsert)
	registerReq := &pb.RegisterNamespaceRequest{
		Name:          data.Name.ValueString(),
		RunWebhookUrl: data.RunWebhookURL.ValueString(),
	}
	if data.RetentionPolicy != nil {
		registerReq.RetentionPolicy = &pb.RetentionPolicy{
			MinVersions: int32(data.RetentionPolicy.MinVersions.ValueInt64()),
			MaxAgeDays:  int32(data.RetentionPolicy.MaxAgeDays.ValueInt64()),
		}
	}

	_, err := r.client.RegisterNamespace(ctx, registerReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update namespace", err.Error())
		return
	}

	// Update policy
	var allowed []string
	if !data.AllowedConsumers.IsNull() && !data.AllowedConsumers.IsUnknown() {
		resp.Diagnostics.Append(data.AllowedConsumers.ElementsAs(ctx, &allowed, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	_, err = r.client.SetNamespacePolicy(ctx, &pb.SetNamespacePolicyRequest{
		Namespace:        data.Name.ValueString(),
		AllowedConsumers: allowed,
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to update namespace policy", err.Error())
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *NamespaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// No-op. Namespace cannot be deleted from backend via API currently.
}

func (r *NamespaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
