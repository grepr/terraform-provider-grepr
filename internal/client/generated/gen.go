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
// This will fetch the OpenAPI spec from https://docs.grepr.ai/openapi.json and generate
// models.gen.go with all the type definitions.
package generated

// Generate Go types from the Grepr OpenAPI specification.
// This pulls the latest OpenAPI spec from docs.grepr.ai and generates type definitions
// for use with the openapi-fetch client.
//
// Note: We use --old-config-style for compatibility with the openapi-fetch approach.
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --old-config-style -generate types -package generated -o models.gen.go https://docs.grepr.ai/openapi.json
