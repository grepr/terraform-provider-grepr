// Package generated contains Go types, and a client for the operations the provider calls
// (client.cfg.yaml), generated from the Grepr OpenAPI specification.
//
// To regenerate them, run:
//
//	make generate
//
// or directly:
//
//	go generate ./internal/client/generated
//
// The spec it reads is openapi.json at the provider root: in grepr-server, a link to the
// committed docs/public/openapi.json; in the public provider repository, which releases the
// provider, a copy the sync workflow makes. So the generated types are a function of the
// commit rather than of what happens to be deployed at docs.grepr.ai at the moment the
// generator runs. That matters beyond reproducibility: the
// provider's compiled package graph is what the third-party attribution bundle is built from,
// so a spec that moved on its own could change the bundle -- and so flip the licences check
// from green to red -- on a commit that touched nothing. It is also the spec
// .github/workflows/terraform-provider-openapi-check.yml already generates against.
package generated

// Generate Go types from the Grepr OpenAPI specification.
// This pulls the committed OpenAPI spec and generates type definitions for use with the
// openapi-fetch client. The path is relative to this directory, which is where `go generate`
// runs the directive from.
//
// Note: We use --old-config-style for compatibility with the openapi-fetch approach.
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --old-config-style -generate types -package generated -o models.gen.go ../../../openapi.json
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config client.cfg.yaml ../../../openapi.json
