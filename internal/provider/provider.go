// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Ensure AzurepimProvider satisfies various provider interfaces.
var _ provider.Provider = &AzurepimProvider{}

// AzurepimProvider defines the provider implementation.
type AzurepimProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// AzurepimProviderModel describes the provider data model.
type AzurepimProviderModel struct {
}

func (p *AzurepimProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "azurepim"
	resp.Version = p.version
}

func (p *AzurepimProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The Azure PIM provider was built as PIM group eligible assignment is as of writing not supported in the official azurerm provider.",
	}
}

func (p *AzurepimProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data AzurepimProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Configuration values are now available.
	// if data.Endpoint.IsNull() { /* ... */ }

	// Example client configuration for data sources and resources
	client := http.DefaultClient
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *AzurepimProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewGroupEligibleAssignment,
	}
}

func (p *AzurepimProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &AzurepimProvider{
			version: version,
		}
	}
}
