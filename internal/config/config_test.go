package config

import "testing"

func TestModemOptsFlagSetParsesKeyValue(t *testing.T) {
	f := &modemOptsFlag{}
	if err := f.Set("pin=1234"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if got, want := f.m["pin"], "1234"; got != want {
		t.Fatalf("pin = %q, want %q", got, want)
	}
}

func TestModemOptsFlagSetTrimsWhitespace(t *testing.T) {
	f := &modemOptsFlag{}
	if err := f.Set("  pin  =  1234  "); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if got, want := f.m["pin"], "1234"; got != want {
		t.Fatalf("pin = %q, want %q (value should be trimmed)", got, want)
	}
}

func TestModemOptsFlagSetRepeatedOverwrites(t *testing.T) {
	f := &modemOptsFlag{}
	if err := f.Set("key=a"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := f.Set("key=b"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if got, want := f.m["key"], "b"; got != want {
		t.Fatalf("key = %q, want %q (later value should overwrite)", got, want)
	}
}

func TestModemOptsFlagSetAllowsEmptyValue(t *testing.T) {
	// Missing value (e.g. "pin=") is valid — it is explicitly an empty string.
	f := &modemOptsFlag{}
	if err := f.Set("pin="); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if _, ok := f.m["pin"]; !ok {
		t.Fatal("pin key not set when value is empty")
	}
}

func TestModemOptsFlagSetRejectsMissingEquals(t *testing.T) {
	f := &modemOptsFlag{}
	if err := f.Set("noequals"); err == nil {
		t.Fatal("Set accepted value with no '='")
	}
}

func TestModemOptsFlagSetRejectsEmptyKey(t *testing.T) {
	f := &modemOptsFlag{}
	if err := f.Set("=value"); err == nil {
		t.Fatal("Set accepted empty key")
	}
	if err := f.Set("   =value"); err == nil {
		t.Fatal("Set accepted whitespace-only key")
	}
}

func TestModemOptsFlagStringRoundTrip(t *testing.T) {
	f := &modemOptsFlag{}
	if s := f.String(); s != "" {
		t.Fatalf("empty flag String() = %q, want \"\"", s)
	}
	if err := f.Set("a=1"); err != nil {
		t.Fatalf("Set error: %v", err)
	}
	if s := f.String(); s != "a=1" {
		t.Fatalf("String() = %q, want %q", s, "a=1")
	}
}
