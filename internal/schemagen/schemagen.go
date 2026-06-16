// Package schemagen turns authored fields into a Clio-compatible JSON Schema
// (Draft 2020-12) for an event's data payload.
package schemagen

import (
	"encoding/json"
	"strings"

	"github.com/pblumer/clio-workbench/internal/model"
)

// EventSchema renders a pretty-printed JSON Schema for the given fields,
// preserving field order. Fields without a name are skipped.
func EventSchema(fields []model.Field) string {
	var named []model.Field
	for _, f := range fields {
		if strings.TrimSpace(f.Name) != "" {
			named = append(named, f)
		}
	}

	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString("  \"$schema\": \"https://json-schema.org/draft/2020-12/schema\",\n")
	b.WriteString("  \"type\": \"object\",\n")
	b.WriteString("  \"properties\": {")
	for i, f := range named {
		prop, _ := json.Marshal(propSchema(f))
		name, _ := json.Marshal(f.Name)
		b.WriteString("\n    ")
		b.Write(name)
		b.WriteString(": ")
		b.Write(prop)
		if i < len(named)-1 {
			b.WriteString(",")
		}
	}
	if len(named) > 0 {
		b.WriteString("\n  }")
	} else {
		b.WriteString("}")
	}

	var required []string
	for _, f := range named {
		if f.Required {
			required = append(required, f.Name)
		}
	}
	if len(required) > 0 {
		rb, _ := json.Marshal(required)
		b.WriteString(",\n  \"required\": ")
		b.Write(rb)
	}
	b.WriteString("\n}")
	return b.String()
}

// propSchema builds the JSON Schema fragment for one field.
func propSchema(f model.Field) map[string]any {
	m := map[string]any{}
	switch f.Type {
	case "integer":
		m["type"] = "integer"
	case "number":
		m["type"] = "number"
	case "boolean":
		m["type"] = "boolean"
	case "datetime":
		m["type"] = "string"
		m["format"] = "date-time"
	case "enum":
		m["type"] = "string"
		if len(f.Enum) > 0 {
			m["enum"] = f.Enum
		}
	case "reference":
		m["type"] = "string"
		if f.Format != "" {
			m["format"] = f.Format
		}
		if f.Ref != "" {
			m["description"] = "reference to " + f.Ref
		}
	default: // string
		m["type"] = "string"
		if f.Format != "" {
			m["format"] = f.Format
		}
	}
	if f.Description != "" {
		if _, ok := m["description"]; !ok {
			m["description"] = f.Description
		}
	}
	return m
}
