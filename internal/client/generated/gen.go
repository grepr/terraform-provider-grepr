// Package generated contains auto-generated Go types from the Grepr OpenAPI specification.
//
// To regenerate the models, run:
//
//	make generate
//
// or directly:
//
//	go generate ./internal/client/generated
//
// The spec it reads is docs/public/openapi.json, committed in this repository, so the
// generated types are a function of the commit rather than of what happens to be deployed at
// docs.grepr.ai at the moment the generator runs. That matters beyond reproducibility: the
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
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --old-config-style -generate types -package generated -o models.gen.go ../../../../../docs/public/openapi.json
