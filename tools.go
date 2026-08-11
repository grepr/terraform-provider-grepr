//go:build tools

// Package tools tracks tool dependencies for this module.
//
// This file ensures that tool dependencies are recorded in go.mod without being
// imported in regular code. The tools build tag ensures this file is excluded
// from normal builds but included when running `go mod tidy`.
//
// To regenerate OpenAPI code, run: make generate
package tools

import (
	// oapi-codegen generates Go client code from OpenAPI specifications.
	// It's used to generate internal/client/generated/models.gen.go from the Grepr OpenAPI spec.
	_ "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen"
	_ "github.com/oapi-codegen/runtime"
	_ "github.com/oapi-codegen/runtime/types"
)
