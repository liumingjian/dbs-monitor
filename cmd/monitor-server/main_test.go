package main

import (
	"context"
	"testing"
)

func TestRunCommandRejectsUnsupportedArguments(t *testing.T) {
	for _, arguments := range [][]string{
		{"unsupported"},
		{rotateMasterKeyCommand, "unexpected"},
	} {
		err := runCommand(context.Background(), arguments)
		if err == nil {
			t.Fatalf("runCommand(%q) succeeded", arguments)
		}
		if got, want := err.Error(), "usage: dbs-monitor-server [rotate-master-key]"; got != want {
			t.Fatalf("runCommand(%q) error = %q, want %q", arguments, got, want)
		}
	}
}
