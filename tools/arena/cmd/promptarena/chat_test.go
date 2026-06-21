package main

import "testing"

func TestChatCmd_Registered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"chat"})
	if err != nil {
		t.Fatalf("find chat: %v", err)
	}
	if cmd.Name() != "chat" {
		t.Fatalf("want chat command, got %q", cmd.Name())
	}
	if cmd.Flag("config") == nil {
		t.Fatalf("chat command missing --config flag")
	}
	if cmd.Flag("mock-provider") == nil {
		t.Fatalf("chat command missing --mock-provider flag")
	}
	if cmd.Flag("mock-config") == nil {
		t.Fatalf("chat command missing --mock-config flag")
	}
}
