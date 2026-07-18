package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/thapakazi/kazi/internal/engine"
	"github.com/thapakazi/kazi/internal/runtime"
)

func TestExitCode(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{errors.New("boom"), 1},
		{fmt.Errorf("wrap: %w", ErrUsage), 2},
		{fmt.Errorf("wrap: %w", engine.ErrStackNotFound), 3},
		{fmt.Errorf("wrap: %w", runtime.ErrNoRuntime), 4},
	}
	for _, c := range cases {
		if got := exitCode(c.err); got != c.want {
			t.Errorf("exitCode(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestErrCode(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{ErrUsage, "usage"},
		{engine.ErrStackNotFound, "stack_not_found"},
		{runtime.ErrNoRuntime, "no_runtime"},
		{errors.New("x"), "runtime_failure"},
	}
	for _, c := range cases {
		if got := errCode(c.err); got != c.want {
			t.Errorf("errCode(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestExactArgsWrapsUsage(t *testing.T) {
	if err := exactArgs(1)(nil, []string{}); !errors.Is(err, ErrUsage) {
		t.Errorf("want ErrUsage, got %v", err)
	}
	if err := exactArgs(1)(nil, []string{"a"}); err != nil {
		t.Errorf("want nil, got %v", err)
	}
}

func TestRangeArgsWrapsUsage(t *testing.T) {
	cases := []struct {
		n    int
		want bool // want ErrUsage
	}{
		{0, true},
		{1, false},
		{2, false},
		{3, true},
	}
	for _, c := range cases {
		args := make([]string, c.n)
		err := rangeArgs(1, 2)(nil, args)
		if got := errors.Is(err, ErrUsage); got != c.want {
			t.Errorf("rangeArgs(1,2) with %d args: err=%v, want ErrUsage=%v", c.n, err, c.want)
		}
	}
}
