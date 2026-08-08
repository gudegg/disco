package handlers

import (
	"testing"

	"github.com/gudegg/disco/models"
)

func TestConfigValueFromJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     interface{}
		wantValue string
		wantType  string
	}{
		{name: "string", input: "hello", wantValue: "hello", wantType: models.ConfigTypeString},
		{name: "object", input: map[string]interface{}{"a": 1}, wantValue: `{"a":1}`, wantType: models.ConfigTypeJSON},
		{name: "array", input: []interface{}{1, 2}, wantValue: `[1,2]`, wantType: models.ConfigTypeJSON},
		{name: "number", input: float64(3306), wantValue: "3306", wantType: models.ConfigTypeString},
		{name: "bool", input: true, wantValue: "true", wantType: models.ConfigTypeString},
		{name: "null", input: nil, wantValue: "null", wantType: models.ConfigTypeString},
	}

	for _, tt := range tests {
		gotValue, gotType := configValueFromJSON(tt.input)
		if gotValue != tt.wantValue || gotType != tt.wantType {
			t.Fatalf("%s: got (%q, %q), want (%q, %q)", tt.name, gotValue, gotType, tt.wantValue, tt.wantType)
		}
	}
}
