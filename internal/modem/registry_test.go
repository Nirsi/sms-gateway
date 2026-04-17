package modem

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeModem satisfies the Modem interface for registry tests.
type fakeModem struct{ name string }

func (fakeModem) Open(context.Context) error                 { return nil }
func (fakeModem) Close() error                               { return nil }
func (fakeModem) GetStatus(context.Context) (*Status, error) { return &Status{}, nil }
func (fakeModem) SendSMS(context.Context, string, string) (*SendResult, error) {
	return &SendResult{}, nil
}

// resetRegistry swaps the package registry for isolated tests and returns
// a function that restores the original.
func resetRegistry(t *testing.T) func() {
	t.Helper()
	registryMu.Lock()
	orig := registry
	registry = map[string]Factory{}
	registryMu.Unlock()
	return func() {
		registryMu.Lock()
		registry = orig
		registryMu.Unlock()
	}
}

func TestRegisterAndOpen(t *testing.T) {
	defer resetRegistry(t)()

	Register("fake", func(opts Options) (Modem, error) {
		return fakeModem{name: opts.Port}, nil
	})

	m, err := Open("fake", Options{Port: "COM1"})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	fm, ok := m.(fakeModem)
	if !ok {
		t.Fatalf("Open returned type %T, want fakeModem", m)
	}
	if fm.name != "COM1" {
		t.Fatalf("factory received Port=%q, want COM1", fm.name)
	}
}

func TestOpenUnknownDriver(t *testing.T) {
	defer resetRegistry(t)()

	Register("alpha", func(Options) (Modem, error) { return fakeModem{}, nil })

	_, err := Open("nope", Options{})
	if err == nil {
		t.Fatal("Open with unknown driver returned nil error")
	}
	if !strings.Contains(err.Error(), "unknown driver") {
		t.Fatalf("error %q should mention unknown driver", err)
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("error %q should list available drivers", err)
	}
}

func TestOpenPropagatesFactoryError(t *testing.T) {
	defer resetRegistry(t)()

	want := errors.New("factory boom")
	Register("boom", func(Options) (Modem, error) { return nil, want })

	_, err := Open("boom", Options{})
	if !errors.Is(err, want) {
		t.Fatalf("Open error = %v, want %v", err, want)
	}
}

func TestRegisterEmptyNamePanics(t *testing.T) {
	defer resetRegistry(t)()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register with empty name did not panic")
		}
	}()
	Register("", func(Options) (Modem, error) { return fakeModem{}, nil })
}

func TestRegisterNilFactoryPanics(t *testing.T) {
	defer resetRegistry(t)()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register with nil factory did not panic")
		}
	}()
	Register("nilf", nil)
}

func TestRegisterDuplicatePanics(t *testing.T) {
	defer resetRegistry(t)()

	Register("dup", func(Options) (Modem, error) { return fakeModem{}, nil })

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("duplicate Register did not panic")
		}
	}()
	Register("dup", func(Options) (Modem, error) { return fakeModem{}, nil })
}

func TestDriversIsSorted(t *testing.T) {
	defer resetRegistry(t)()

	for _, n := range []string{"charlie", "alpha", "bravo"} {
		Register(n, func(Options) (Modem, error) { return fakeModem{}, nil })
	}

	got := Drivers()
	want := []string{"alpha", "bravo", "charlie"}
	if len(got) != len(want) {
		t.Fatalf("Drivers() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("Drivers()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRegistryConcurrentSafe(t *testing.T) {
	defer resetRegistry(t)()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = Drivers()
			_, _ = Open("missing", Options{})
		}()
	}
	Register("x", func(Options) (Modem, error) { return fakeModem{}, nil })
	wg.Wait()
}
