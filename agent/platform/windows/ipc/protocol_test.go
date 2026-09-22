package ipc

import "testing"

func TestLockRequestAndResponseValidation(t *testing.T) {
	request := Request{Version: ProtocolVersion, ID: "request-1", Command: CommandLock}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Response{Version: ProtocolVersion, ID: request.ID, Accepted: true}).ValidateFor(request); err != nil {
		t.Fatal(err)
	}
	if err := (Request{Version: ProtocolVersion, ID: "stop-1", Command: CommandStop}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Request{Version: ProtocolVersion, ID: "notice-1", Command: CommandNotify, Title: "Compasso", Body: "Aviso"}).Validate(); err != nil {
		t.Fatal(err)
	}

	invalidRequests := []Request{
		{Version: 2, ID: request.ID, Command: CommandLock},
		{Version: ProtocolVersion, Command: CommandLock},
		{Version: ProtocolVersion, ID: request.ID, Command: "unlock"},
		{Version: ProtocolVersion, ID: request.ID, Command: CommandNotify},
		{Version: ProtocolVersion, ID: request.ID, Command: CommandLock, Title: "unexpected"},
	}
	for _, invalid := range invalidRequests {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("invalid request accepted: %+v", invalid)
		}
	}

	invalidResponses := []Response{
		{Version: 2, ID: request.ID, Accepted: true},
		{Version: ProtocolVersion, ID: "other", Accepted: true},
		{Version: ProtocolVersion, ID: request.ID},
		{Version: ProtocolVersion, ID: request.ID, Accepted: true, Error: "contradiction"},
	}
	for _, invalid := range invalidResponses {
		if err := invalid.ValidateFor(request); err == nil {
			t.Fatalf("invalid response accepted: %+v", invalid)
		}
	}
}

func TestPipePathOnlyAcceptsLogicalLocalNames(t *testing.T) {
	path, err := PipePath("CompassoAgent-1")
	if err != nil {
		t.Fatal(err)
	}
	if path != `\\.\pipe\CompassoAgent-1` {
		t.Fatalf("pipe path = %q", path)
	}
	for _, invalid := range []string{"", `server\pipe\name`, `nested/name`, "name with spaces"} {
		if _, err := PipePath(invalid); err == nil {
			t.Fatalf("invalid pipe name accepted: %q", invalid)
		}
	}
}
