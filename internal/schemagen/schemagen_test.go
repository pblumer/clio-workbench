package schemagen

import (
	"encoding/json"
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
