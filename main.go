// Package main is the entry point for the Grepr Terraform provider.
//
// This provider allows users to manage Grepr pipelines (async streaming jobs) through
// Terraform configuration. It communicates with the Grepr API using OAuth2 authentication
// via Auth0.
//
// Usage:
//
//	provider "grepr" {
//	  host          = "https://myorg.app.grepr.ai/api"
//	  client_id     = var.grepr_client_id
//	  client_secret = var.grepr_client_secret
//	}
//
//	resource "grepr_pipeline" "example" {
//	  name           = "my_pipeline"
//	  job_graph_json = jsonencode({...})
//	}
//
// For more information, see the provider documentation.
package main

// Generates the registry documentation under docs/ from the provider schema and
// the examples/ directory. tfplugindocs is invoked with an explicit version so it
// is not tracked in this module's go.mod (it is only needed to regenerate docs).
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0 generate --provider-name grepr

import (
	"context"
	"flag"
	"log"

	"github.com/grepr/terraform-provider-grepr/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// version is set at build time via -ldflags "-X main.version=X.Y.Z"
var (
	version string = "dev"
)

func main() {
	var debug bool

	// The debug flag enables running the provider in debug mode, which allows
	// attaching a debugger like Delve. This is useful for development.
	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		// Address is the full registry address for this provider.
		// Users reference this as "grepr/grepr" in their Terraform/OpenTofu configuration.
		Address: "registry.terraform.io/grepr/grepr",
		Debug:   debug,
	}

	err := providerserver.Serve(context.Background(), provider.New(version), opts)
	if err != nil {
		log.Fatal(err.Error())
	}
}
