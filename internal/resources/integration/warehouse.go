package integration

import (
	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type DataWarehouseModel struct {
	Common
}

var NewDataWarehouseResource = newResource(Kind[DataWarehouseModel, generated.DataWarehouse]{
	Type:         "data-warehouse",
	Description:  "Manages a Grepr-managed data warehouse integration, which stores datasets in Grepr's own storage.",
	Attributes:   map[string]schema.Attribute{},
	ToPayload:    func(DataWarehouseModel, *generated.DataWarehouse) {},
	FromPayload:  func(generated.DataWarehouse, *DataWarehouseModel) {},
	WriteSecrets: func(*secretWriter, DataWarehouseModel, *DataWarehouseModel) {},
})

type S3DataWarehouseModel struct {
	Common
	BucketName    types.String `tfsdk:"bucket_name"`
	AssumeRoleARN types.String `tfsdk:"assume_role_arn"`
	Region        types.String `tfsdk:"region"`
	Prefix        types.String `tfsdk:"prefix"`
}

var NewS3DataWarehouseResource = newResource(Kind[S3DataWarehouseModel, generated.S3DataWarehouse]{
	Type:        "s3-data-warehouse",
	Description: "Manages a data warehouse integration that stores datasets in your S3 bucket. Grepr test-writes to the bucket when the integration is created, using the role in `assume_role_arn`. The bucket cannot change, so changing it replaces the integration.",
	Attributes: map[string]schema.Attribute{
		"bucket_name": schema.StringAttribute{
			Required:            true,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			MarkdownDescription: "S3 bucket holding the data. Changing it replaces the integration.",
		},
		"assume_role_arn": schema.StringAttribute{
			Computed:            true,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			MarkdownDescription: "IAM role Grepr assumes to access the bucket, derived by Grepr for your organization. Grant it access to the bucket.",
		},
		"region": defaultedString("AWS region of the bucket, which must be the Grepr deployment's region. The server defaults it to `us-east-1`, so set it for deployments in other regions."),
		"prefix": schema.StringAttribute{
			Computed:            true,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			MarkdownDescription: "Folder within the bucket where data is stored.",
		},
	},
	ToPayload: func(m S3DataWarehouseModel, p *generated.S3DataWarehouse) {
		p.BucketName = m.BucketName.ValueString()
		setString(&p.Region, m.Region)
	},
	FromPayload: func(p generated.S3DataWarehouse, m *S3DataWarehouseModel) {
		m.BucketName = types.StringValue(p.BucketName)
		m.AssumeRoleARN = stringValue(p.AssumeRoleArn)
		m.Region = stringValue(p.Region)
		m.Prefix = stringValue(p.Prefix)
	},
	WriteSecrets: func(*secretWriter, S3DataWarehouseModel, *S3DataWarehouseModel) {},
})
