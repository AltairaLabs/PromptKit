package main

import (
	"reflect"
	"testing"
)

func TestNormalizeHelpArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "nil args",
			args: nil,
			want: []string{},
		},
		{
			name: "empty args",
			args: []string{},
			want: []string{},
		},
		{
			name: "single -? becomes --help",
			args: []string{"-?"},
			want: []string{"--help"},
		},
		{
			name: "-? after subcommand",
			args: []string{"run", "-?"},
			want: []string{"run", "--help"},
		},
		{
			name: "-? mixed with other flags",
			args: []string{"run", "--config", "foo.yaml", "-?"},
			want: []string{"run", "--config", "foo.yaml", "--help"},
		},
		{
			name: "multiple -? all rewritten",
			args: []string{"-?", "run", "-?"},
			want: []string{"--help", "run", "--help"},
		},
		{
			name: "--help passes through unchanged",
			args: []string{"--help"},
			want: []string{"--help"},
		},
		{
			name: "-h passes through unchanged",
			args: []string{"run", "-h"},
			want: []string{"run", "-h"},
		},
		{
			name: "no question mark passes through unchanged",
			args: []string{"run", "--config", "foo.yaml"},
			want: []string{"run", "--config", "foo.yaml"},
		},
		{
			name: "embedded ? not rewritten",
			args: []string{"--filter=a?b", "-?x"},
			want: []string{"--filter=a?b", "-?x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeHelpArgs(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("normalizeHelpArgs(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestNormalizeHelpArgsDoesNotMutateInput(t *testing.T) {
	in := []string{"run", "-?"}
	_ = normalizeHelpArgs(in)
	if in[1] != "-?" {
		t.Errorf("normalizeHelpArgs mutated its input: got %v", in)
	}
}
