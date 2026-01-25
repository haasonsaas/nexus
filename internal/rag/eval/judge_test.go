package eval

import "testing"

func TestParseScore(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{name: "plain", input: "0.85", want: 0.85},
		{name: "with_label", input: "Score: 0.4", want: 0.4},
		{name: "percent", input: "85%", want: 0.85},
		{name: "out_of_range", input: "1.5", wantErr: true},
		{name: "missing", input: "no score", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseScore(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %.3f", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %.3f want %.3f", got, tt.want)
			}
		})
	}
}
