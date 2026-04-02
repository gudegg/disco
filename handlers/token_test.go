package handlers

import "testing"

func TestParseServiceID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    uint
		wantErr bool
	}{
		{name: "valid", input: "42", want: 42},
		{name: "invalid", input: "4x", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tt := range tests {
		got, err := parseServiceID(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error", tt.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
		if got != tt.want {
			t.Fatalf("%s: got %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestGenerateRandomString(t *testing.T) {
	value, err := generateRandomString(32)
	if err != nil {
		t.Fatalf("generateRandomString() error = %v", err)
	}
	if len(value) != 64 {
		t.Fatalf("generateRandomString() len = %d, want %d", len(value), 64)
	}
}
