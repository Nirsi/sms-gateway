package modem

import "testing"

func TestOptionsString(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		key  string
		def  string
		want string
	}{
		{name: "nil extra", opts: Options{}, key: "pin", def: "0000", want: "0000"},
		{name: "missing key", opts: Options{Extra: map[string]string{"apn": "internet"}}, key: "pin", def: "0000", want: "0000"},
		{name: "present key", opts: Options{Extra: map[string]string{"pin": "1234"}}, key: "pin", def: "0000", want: "1234"},
		{name: "empty value", opts: Options{Extra: map[string]string{"pin": ""}}, key: "pin", def: "0000", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.opts.String(tt.key, tt.def); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOptionsInt(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]string
		want  int
	}{
		{name: "valid", extra: map[string]string{"timeout": "30"}, want: 30},
		{name: "missing", extra: map[string]string{}, want: 10},
		{name: "empty", extra: map[string]string{"timeout": ""}, want: 10},
		{name: "malformed", extra: map[string]string{"timeout": "12x"}, want: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := Options{Extra: tt.extra}
			if got := opts.Int("timeout", 10); got != tt.want {
				t.Fatalf("Int() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestOptionsBool(t *testing.T) {
	tests := []struct {
		name  string
		value string
		def   bool
		want  bool
	}{
		{name: "one", value: "1", want: true},
		{name: "true", value: "true", want: true},
		{name: "yes", value: "yes", want: true},
		{name: "on", value: "on", want: true},
		{name: "mixed case true", value: "TrUe", want: true},
		{name: "zero", value: "0", def: true, want: false},
		{name: "false", value: "false", def: true, want: false},
		{name: "no", value: "no", def: true, want: false},
		{name: "off", value: "off", def: true, want: false},
		{name: "unknown defaults true", value: "maybe", def: true, want: true},
		{name: "unknown defaults false", value: "maybe", want: false},
		{name: "missing default", value: "", def: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extra := map[string]string{}
			if tt.value != "" {
				extra["enabled"] = tt.value
			}
			opts := Options{Extra: extra}
			if got := opts.Bool("enabled", tt.def); got != tt.want {
				t.Fatalf("Bool() = %t, want %t", got, tt.want)
			}
		})
	}
}
