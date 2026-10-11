package integration

import (
	"bytes"
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Helpers that translate between Terraform values and the generated payload
// types. A null value clears the field; an unknown one leaves it as it is, so
// on create the server derives it.

func setString[S ~string](dst **S, v types.String) {
	if v.IsUnknown() {
		return
	}
	if v.IsNull() {
		*dst = nil
		return
	}
	s := S(v.ValueString())
	*dst = &s
}

func setBool(dst **bool, v types.Bool) {
	if v.IsUnknown() {
		return
	}
	if v.IsNull() {
		*dst = nil
		return
	}
	b := v.ValueBool()
	*dst = &b
}

func stringValue[S ~string](s *S) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(string(*s))
}

func boolValue(b *bool) types.Bool {
	if b == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*b)
}

// typeNameSuffix maps an API type to its resource name: "data-warehouse"
// becomes "data_warehouse_integration".
func typeNameSuffix(typ string) string {
	return strings.ReplaceAll(typ, "-", "_") + "_integration"
}

func sortedKeys[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }

func isSecret(secrets []string, name string) bool { return slices.Contains(secrets, name) }

// decodePayload reads an integration payload into its generated type.
func decodePayload[P any](raw map[string]any) (P, error) {
	var p P
	b, err := json.Marshal(raw)
	if err == nil {
		err = decodeJSON(b, &p)
	}
	return p, err
}

// decodeJSON decodes keeping numbers exact, as the client does, so large
// integers in fields this provider does not model are sent back unchanged.
func decodeJSON(b []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	return decoder.Decode(v)
}

// encodePayload writes p over the remote payload: every field the generated
// type knows comes from p, and fields newer than the provider's API spec keep
// their remote values, so an older provider does not erase them.
func encodePayload[P any](p P, remote map[string]any) (map[string]any, error) {
	known := map[string]bool{}
	if t := reflect.TypeOf(p); t != nil && t.Kind() == reflect.Struct {
		for i := range t.NumField() {
			known[strings.Split(t.Field(i).Tag.Get("json"), ",")[0]] = true
		}
	}
	result := map[string]any{}
	for key, value := range remote {
		if !known[key] {
			result[key] = value
		}
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var encoded map[string]any
	if err := decodeJSON(b, &encoded); err != nil {
		return nil, err
	}
	// Absent and null mean the same to the server, which leaves nulls out of
	// its own responses.
	for key, value := range encoded {
		if value != nil {
			result[key] = value
		}
	}
	return result, nil
}
