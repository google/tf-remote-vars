package provider

import (
	"context"
	"fmt"
	"math/big"

	pb "github.com/google/varlet/proto/v1"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/types/known/structpb"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &InputResource{}
var _ resource.ResourceWithConfigure = &InputResource{}

func NewInputResource() resource.Resource {
	return &InputResource{}
}

// InputResource defines the resource implementation.
type InputResource struct {
	client           pb.VarletServiceClient
	defaultNamespace string
}

// InputResourceModel describes the resource data model.
type InputResourceModel struct {
	ID              types.String  `tfsdk:"id"`
	Namespace       types.String  `tfsdk:"namespace"`
	SourceNamespace types.String  `tfsdk:"source_namespace"`
	Name            types.String  `tfsdk:"name"`
	Value           types.Dynamic `tfsdk:"value"`
	Trigger         types.Int64   `tfsdk:"trigger"`
}

func (r *InputResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_input"
}

func (r *InputResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Consumes a variable from the Varlet backend and tracks dependency.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique ID of the input (consumer_namespace/source_namespace/name).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"namespace": schema.StringAttribute{
				MarkdownDescription: "The consumer namespace. Defaults to provider default namespace.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"source_namespace": schema.StringAttribute{
				MarkdownDescription: "The namespace of the source variable.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
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
				MarkdownDescription: "The value of the variable.",
				Computed:            true,
			},
			"trigger": schema.Int64Attribute{
				MarkdownDescription: "The actuation nonce (version) of the variable. Changes to this will trigger downstream updates.",
				Computed:            true,
			},
		},
	}
}

func (r *InputResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *InputResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data InputResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve consumer namespace
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

	// Call RegisterConsumer
	res, err := r.client.RegisterConsumer(ctx, &pb.RegisterConsumerRequest{
		ConsumerNamespace: ns,
		SourceNamespace:   data.SourceNamespace.ValueString(),
		VariableName:     data.Name.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to register consumer",
			fmt.Sprintf("Could not register consumer: %s", err.Error()),
		)
		return
	}

	// Convert value
	attrVal, err := r.convProtoToAttr(ctx, res.GetValue())
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to convert value",
			fmt.Sprintf("Could not convert value from proto: %s", err.Error()),
		)
		return
	}

	data.Value = types.DynamicValue(attrVal)
	data.Trigger = types.Int64Value(res.GetActuationNonce())
	data.ID = types.StringValue(fmt.Sprintf("%s/%s/%s", ns, data.SourceNamespace.ValueString(), data.Name.ValueString()))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *InputResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data InputResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Call GetVariableValue (requires consumerNS, sourceNS, varName)
	res, err := r.client.GetVariableValue(ctx, &pb.GetVariableValueRequest{
		ConsumerNamespace: data.Namespace.ValueString(),
		SourceNamespace:   data.SourceNamespace.ValueString(),
		VariableName:     data.Name.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to read variable value",
			fmt.Sprintf("Could not read variable value: %s", err.Error()),
		)
		return
	}

	attrVal, err := r.convProtoToAttr(ctx, res.GetValue())
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to convert value",
			fmt.Sprintf("Could not convert value from proto: %s", err.Error()),
		)
		return
	}

	data.Value = types.DynamicValue(attrVal)
	data.Trigger = types.Int64Value(res.GetActuationNonce())

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *InputResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Update is a no-op because changing source_namespace, name, or namespace requires replacement.
	// value and trigger are computed, so they are updated during Read.
	// We just copy plan to state.
	var data InputResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *InputResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data InputResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Call DeregisterConsumer
	_, err := r.client.DeregisterConsumer(ctx, &pb.DeregisterConsumerRequest{
		ConsumerNamespace: data.Namespace.ValueString(),
		SourceNamespace:   data.SourceNamespace.ValueString(),
		VariableName:     data.Name.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to deregister consumer",
			fmt.Sprintf("Could not deregister consumer: %s", err.Error()),
		)
		return
	}
}

func (r *InputResource) convProtoToAttr(ctx context.Context, pv *structpb.Value) (attr.Value, error) {
	if pv == nil {
		return types.DynamicNull(), nil
	}

	switch kind := pv.GetKind().(type) {
	case *structpb.Value_NullValue:
		return types.DynamicNull(), nil

	case *structpb.Value_StringValue:
		return types.StringValue(kind.StringValue), nil

	case *structpb.Value_BoolValue:
		return types.BoolValue(kind.BoolValue), nil

	case *structpb.Value_NumberValue:
		return types.NumberValue(big.NewFloat(kind.NumberValue)), nil

	case *structpb.Value_ListValue:
		vals := kind.ListValue.GetValues()
		elemTypes := make([]attr.Type, len(vals))
		elemValues := make([]attr.Value, len(vals))
		for i, val := range vals {
			av, err := r.convProtoToAttr(ctx, val)
			if err != nil {
				return nil, err
			}
			elemValues[i] = av
			elemTypes[i] = av.Type(ctx)
		}
		tupVal, diags := types.TupleValue(elemTypes, elemValues)
		if diags.HasError() {
			return nil, fmt.Errorf("failed to create tuple: %v", diags)
		}
		return tupVal, nil

	case *structpb.Value_StructValue:
		fields := kind.StructValue.GetFields()
		attrTypes := make(map[string]attr.Type)
		attrValues := make(map[string]attr.Value)
		for k, val := range fields {
			av, err := r.convProtoToAttr(ctx, val)
			if err != nil {
				return nil, err
			}
			attrValues[k] = av
			attrTypes[k] = av.Type(ctx)
		}
		objVal, diags := types.ObjectValue(attrTypes, attrValues)
		if diags.HasError() {
			return nil, fmt.Errorf("failed to create object: %v", diags)
		}
		return objVal, nil

	default:
		return nil, fmt.Errorf("unsupported proto value kind: %T", kind)
	}
}
