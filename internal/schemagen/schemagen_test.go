package schemagen

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/model"
)

func TestEventSchema(t *testing.T) {
	out := EventSchema([]model.Field{
		{Name: "employeeId", Type: "string", Required: true, Format: "uuid"},
		{Name: "amount", Type: "number"},
		{Name: "department", Type: "enum", Enum: []string{"eng", "sales"}},
		{Name: "managerId", Type: "reference", Ref: "manager"},
		{Name: "", Type: "string"}, // unnamed → skipped
	})

	// valid JSON
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if v["type"] != "object" {
		t.Errorf("type = %v, want object", v["type"])
	}
	props, _ := v["properties"].(map[string]any)
	if _, ok := props["employeeId"]; !ok {
		t.Error("employeeId property missing")
	}
	if _, ok := props[""]; ok {
		t.Error("unnamed field should be skipped")
	}
	// required contains only employeeId
	req, _ := v["required"].([]any)
	if len(req) != 1 || req[0] != "employeeId" {
		t.Errorf("required = %v, want [employeeId]", req)
	}
	// reference carries a description
	mgr, _ := props["managerId"].(map[string]any)
	if !strings.Contains(mgr["description"].(string), "manager") {
		t.Errorf("reference description = %v", mgr["description"])
	}
	// order preserved: employeeId before amount
	if strings.Index(out, "employeeId") > strings.Index(out, "amount") {
		t.Error("field order not preserved")
	}
}

func TestEventSchemaEmpty(t *testing.T) {
	out := EventSchema(nil)
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("empty schema not valid JSON: %v\n%s", err, out)
	}
}

func TestPropSchema(t *testing.T) {
	cases := []struct {
		name  string
		field model.Field
		want  map[string]any
	}{
		{
			name:  "integer",
			field: model.Field{Type: "integer"},
			want:  map[string]any{"type": "integer"},
		},
		{
			name:  "number",
			field: model.Field{Type: "number"},
			want:  map[string]any{"type": "number"},
		},
		{
			name:  "boolean",
			field: model.Field{Type: "boolean"},
			want:  map[string]any{"type": "boolean"},
		},
		{
			name:  "datetime",
			field: model.Field{Type: "datetime"},
			want:  map[string]any{"type": "string", "format": "date-time"},
		},
		{
			name:  "enum with values",
			field: model.Field{Type: "enum", Enum: []string{"a", "b"}},
			want:  map[string]any{"type": "string", "enum": []string{"a", "b"}},
		},
		{
			name:  "enum without values",
			field: model.Field{Type: "enum"},
			want:  map[string]any{"type": "string"},
		},
		{
			name:  "reference with format and ref",
			field: model.Field{Type: "reference", Format: "uuid", Ref: "manager"},
			want:  map[string]any{"type": "string", "format": "uuid", "description": "reference to manager"},
		},
		{
			name:  "reference without format or ref",
			field: model.Field{Type: "reference"},
			want:  map[string]any{"type": "string"},
		},
		{
			name:  "string default with format",
			field: model.Field{Type: "string", Format: "email"},
			want:  map[string]any{"type": "string", "format": "email"},
		},
		{
			name:  "unknown type falls back to string",
			field: model.Field{Type: "weird"},
			want:  map[string]any{"type": "string"},
		},
		{
			name:  "description added when absent",
			field: model.Field{Type: "string", Description: "hi"},
			want:  map[string]any{"type": "string", "description": "hi"},
		},
		{
			name:  "description not overwritten for reference",
			field: model.Field{Type: "reference", Ref: "manager", Description: "ignored"},
			want:  map[string]any{"type": "string", "description": "reference to manager"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := propSchema(tc.field)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("propSchema(%+v) = %v, want %v", tc.field, got, tc.want)
			}
		})
	}
}

func TestSchemaCollectionEmpty(t *testing.T) {
	if out := SchemaCollection(model.Draft{}); out != "[]" {
		t.Errorf("empty draft = %q, want []", out)
	}

	// Steps present but none are named events → still empty.
	d := model.Draft{Steps: []model.Step{
		{Kind: model.StepTask, Name: "doThing"},
		{Kind: model.StepEvent, Name: ""},
		{Kind: model.StepEvent, Name: "   "},
	}}
	if out := SchemaCollection(d); out != "[]" {
		t.Errorf("no named events = %q, want []", out)
	}
}

func TestSchemaCollection(t *testing.T) {
	d := model.Draft{Steps: []model.Step{
		{Kind: model.StepEvent, Name: "order.placed", Fields: []model.Field{
			{Name: "id", Type: "string", Required: true, Format: "uuid"},
		}},
		{Kind: model.StepTask, Name: "ship"}, // skipped: not an event
		{Kind: model.StepEvent, Name: ""},    // skipped: unnamed
		{Kind: model.StepEvent, Name: "order.shipped", Fields: []model.Field{
			{Name: "carrier", Type: "string"},
		}},
	}}

	out := SchemaCollection(d)

	var arr []struct {
		Type   string         `json:"type"`
		Schema map[string]any `json:"schema"`
	}
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if len(arr) != 2 {
		t.Fatalf("got %d payloads, want 2\n%s", len(arr), out)
	}
	if arr[0].Type != "order.placed" || arr[1].Type != "order.shipped" {
		t.Errorf("types = %q, %q", arr[0].Type, arr[1].Type)
	}
	if arr[0].Schema["type"] != "object" {
		t.Errorf("schema embedded incorrectly: %v", arr[0].Schema)
	}
	props, _ := arr[0].Schema["properties"].(map[string]any)
	if _, ok := props["id"]; !ok {
		t.Errorf("expected id property in embedded schema, got %v", props)
	}
}
