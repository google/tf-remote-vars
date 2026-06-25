package provider

import (
	"context"
	"fmt"
	"math/big"

	pb "github.com/google/varlet/proto/v1"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"google.golang.org/protobuf/types/known/structpb"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &OutputResource{}
var _ resource.ResourceWithConfigure = &OutputResource{}

func NewOutputResource() resource.Resource {
	return &OutputResource{}
}

// OutputResource defines the resource implementation.
type OutputResource struct {
	client           pb.VarletServiceClient
	defaultNamespace string
}

// OutputResourceModel describes the resource data model.
type OutputResourceModel struct {
	ID             types.String  `tfsdk:"id"`
	Namespace      types.String  `tfsdk:"namespace"`
	Name           types.String  `tfsdk:"name"`
	Value          types.Dynamic `tfsdk:"value"`
	ForceActuation types.Bool    `tfsdk:"force_actuation"`
}

func (r *OutputResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_output"
}

func (r *OutputResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Exports a variable to the Varlet backend.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique ID of the output (namespace/name).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"namespace": schema.StringAttribute{
				MarkdownDescription: "The namespace to export the variable to. Defaults to provider default namespace.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the variable.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"value": schema.DynamicAttribute{
				MarkdownDescription: "The value of the variable (can be any Terraform type).",
				Required:            true,
			},
			"force_actuation": schema.BoolAttribute{
				MarkdownDescription: "If true, force consumers to re-apply even if value is unchanged.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
		},
	}
}

func (r *OutputResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *OutputResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data OutputResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve namespace
	ns := r.defaultNamespace
	if !data.Namespace.IsNull() && !data.Namespace.IsUnknown() {
		ns = data.Namespace.ValueString()
	}
	if ns == "" {
		resp.Diagnostics.AddError(
			"Missing Namespace",
			"Namespace must be configured in the provider or the resource.",
		)
		return
	}
	data.Namespace = types.StringValue(ns)

	if err := r.putVariable(ctx, ns, data.Name.ValueString(), data.Value, data.ForceActuation.ValueBool()); err != nil {
		resp.Diagnostics.AddError("Failed to store variable", err.Error())
		return
	}

	// Set ID
	data.ID = types.StringValue(fmt.Sprintf("%s/%s", ns, data.Name.ValueString()))

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *OutputResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data OutputResourceModel

	// Read Terraform state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// ponytail: Read is a noop in Slice 2 because GetVariableValue is in Slice 3.
	// We just keep the existing state.
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *OutputResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data OutputResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.putVariable(ctx, data.Namespace.ValueString(), data.Name.ValueString(), data.Value, data.ForceActuation.ValueBool()); err != nil {
		resp.Diagnostics.AddError("Failed to update variable", err.Error())
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *OutputResource) putVariable(ctx context.Context, ns, name string, value types.Dynamic, force bool) error {
	pbVal, err := r.convDynamicToProto(ctx, value)
	if err != nil {
		return fmt.Errorf("failed to convert value: %w", err)
	}

	_, err = r.client.PutVariable(ctx, &pb.PutVariableRequest{
		Namespace:      ns,
		Name:           name,
		Value:          pbVal,
		ForceActuation: force,
	})
	if err != nil {
		return fmt.Errorf("failed to store variable: %w", err)
	}
	return nil
}

func (r *OutputResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data OutputResourceModel

	// Read Terraform state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Call backend
	_, err := r.client.DeleteVariable(ctx, &pb.DeleteVariableRequest{
		Namespace: data.Namespace.ValueString(),
		Name:      data.Name.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to delete variable",
			fmt.Sprintf("Could not delete variable: %s", err.Error()),
		)
		return
	}
}

func (r *OutputResource) convDynamicToProto(ctx context.Context, d types.Dynamic) (*structpb.Value, error) {
	if d.IsNull() || d.IsUnknown() {
		return structpb.NewNullValue(), nil
	}

	attrVal := d.UnderlyingValue()
	tfVal, err := attrVal.ToTerraformValue(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get terraform value: %w", err)
	}

	return r.convValue(tfVal)
}

func (r *OutputResource) convValue(tv tftypes.Value) (*structpb.Value, error) {
	if !tv.IsKnown() || tv.IsNull() {
		return structpb.NewNullValue(), nil
	}

	typ := tv.Type()

	if typ.Is(tftypes.String) {
		var v string
		if err := tv.As(&v); err != nil {
			return nil, err
		}
		return structpb.NewStringValue(v), nil
	}

	if typ.Is(tftypes.Bool) {
		var v bool
		if err := tv.As(&v); err != nil {
			return nil, err
		}
		return structpb.NewBoolValue(v), nil
	}

	if typ.Is(tftypes.Number) {
		var v *big.Float
		if err := tv.As(&v); err != nil {
			return nil, err
		}
		f64, _ := v.Float64()
		return structpb.NewNumberValue(f64), nil
	}

	switch typ.(type) {
	case tftypes.List, tftypes.Tuple:
		var vals []tftypes.Value
		if err := tv.As(&vals); err != nil {
			return nil, fmt.Errorf("failed to decode collection: %w", err)
		}
		pbVals := make([]*structpb.Value, len(vals))
		for i, val := range vals {
			pv, err := r.convValue(val)
			if err != nil {
				return nil, err
			}
			pbVals[i] = pv
		}
		return structpb.NewListValue(&structpb.ListValue{Values: pbVals}), nil
	case tftypes.Map, tftypes.Object:
		var vals map[string]tftypes.Value
		if err := tv.As(&vals); err != nil {
			return nil, fmt.Errorf("failed to decode collection: %w", err)
		}
		fields := make(map[string]*structpb.Value)
		for k, val := range vals {
			pv, err := r.convValue(val)
			if err != nil {
				return nil, err
			}
			fields[k] = pv
		}
		return structpb.NewStructValue(&structpb.Struct{Fields: fields}), nil
	default:
		return nil, fmt.Errorf("unsupported type: %s", typ.String())
	}
}
