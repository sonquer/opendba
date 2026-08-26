package ai

import (
	"encoding/json"
	"testing"
)

func TestSchemaJSON(t *testing.T) {
	schema := Schema{
		Properties: map[string]Property{
			"statement": {Type: "string", Description: "the SQL to run"},
			"mode":      {Type: "string", Enum: []string{"read", "plan"}},
			"schemas":   {Type: "array", Items: &Property{Type: "string"}},
		},
		Required: []string{"statement"},
	}
	encoded, err := json.Marshal(schema.JSON())
	if err != nil {
		t.Fatalf("marshal error = %v", err)
	}
	want := `{"properties":{"mode":{"enum":["read","plan"],"type":"string"},` +
		`"schemas":{"items":{"type":"string"},"type":"array"},` +
		`"statement":{"description":"the SQL to run","type":"string"}},` +
		`"required":["statement"],"type":"object"}`
	if string(encoded) != want {
		t.Fatalf("JSON() = %s\nwant %s", encoded, want)
	}
}

func TestSchemaJSONEmpty(t *testing.T) {
	encoded, err := json.Marshal(Schema{Type: "object"}.JSON())
	if err != nil {
		t.Fatalf("marshal error = %v", err)
	}
	if string(encoded) != `{"type":"object"}` {
		t.Fatalf("JSON() = %s, want a bare object", encoded)
	}
}
