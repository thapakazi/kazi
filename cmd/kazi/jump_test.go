package main

import (
	"strings"
	"testing"
)

func TestKjFunctionShape(t *testing.T) {
	// The kj function must call `kazi jump <arg> --print` and cd into the
	// result, and must not cd on failure (&& guard).
	for _, want := range []string{"kj()", "kazi jump", "--print", "&& cd"} {
		if !strings.Contains(kjFunction, want) {
			t.Errorf("kjFunction missing %q:\n%s", want, kjFunction)
		}
	}
}
