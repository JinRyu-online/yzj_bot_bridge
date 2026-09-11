package controlapi_test

import (
	"net"
	"testing"

	"yzj-bridge/internal/controlapi"
	"yzj-bridge/internal/runtime"
)

func TestStartFailsWhenAddrInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	api := &controlapi.Server{
		RT:   runtime.New(t.TempDir()),
		Addr: ln.Addr().String(),
	}
	if err := api.Start(); err == nil {
		api.Stop()
		t.Fatal("Start should fail when address is already in use")
	}
}

func TestStartThenSecondStartFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	api1 := &controlapi.Server{RT: runtime.New(t.TempDir()), Addr: addr}
	if err := api1.Start(); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer api1.Stop()

	api2 := &controlapi.Server{RT: runtime.New(t.TempDir()), Addr: addr}
	if err := api2.Start(); err == nil {
		api2.Stop()
		t.Fatal("second Start should fail when address is already in use")
	}
}
