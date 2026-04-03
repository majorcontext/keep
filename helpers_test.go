package keep

import (
	"strings"
	"testing"
)

func TestNewHTTPCall(t *testing.T) {
	call := NewHTTPCall("GET", "api.github.com", "/repos")

	if call.Operation != "GET api.github.com/repos" {
		t.Errorf("Operation = %q, want %q", call.Operation, "GET api.github.com/repos")
	}
	if call.Params["method"] != "GET" {
		t.Errorf("Params[method] = %v, want %q", call.Params["method"], "GET")
	}
	if call.Params["host"] != "api.github.com" {
		t.Errorf("Params[host] = %v, want %q", call.Params["host"], "api.github.com")
	}
	if call.Params["path"] != "/repos" {
		t.Errorf("Params[path] = %v, want %q", call.Params["path"], "/repos")
	}
	if call.Context.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

func TestNewHTTPCallMethodUppercased(t *testing.T) {
	call := NewHTTPCall("post", "example.com", "/data")

	if call.Operation != "POST example.com/data" {
		t.Errorf("Operation = %q, want %q", call.Operation, "POST example.com/data")
	}
	if call.Params["method"] != "POST" {
		t.Errorf("Params[method] = %v, want %q", call.Params["method"], "POST")
	}
}

func TestNewMCPCall(t *testing.T) {
	params := map[string]any{"id": "123"}
	call := NewMCPCall("delete_issue", params)

	if call.Operation != "delete_issue" {
		t.Errorf("Operation = %q, want %q", call.Operation, "delete_issue")
	}
	if call.Params["id"] != "123" {
		t.Errorf("Params[id] = %v, want %q", call.Params["id"], "123")
	}
	if call.Context.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

func TestNewMCPCallNilParams(t *testing.T) {
	call := NewMCPCall("list_repos", nil)

	if call.Operation != "list_repos" {
		t.Errorf("Operation = %q, want %q", call.Operation, "list_repos")
	}
	if call.Params != nil {
		t.Errorf("Params = %v, want nil", call.Params)
	}
}

func TestSafeEvaluate(t *testing.T) {
	eng, err := LoadFromBytes([]byte(`
scope: test
mode: enforce
rules:
  - name: deny-all
    match:
      operation: "*"
    action: deny
    message: blocked
`))
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	defer eng.Close()

	call := Call{Operation: "anything"}
	result, err := SafeEvaluate(eng, call, "test")
	if err != nil {
		t.Fatalf("SafeEvaluate error: %v", err)
	}
	if result.Decision != Deny {
		t.Errorf("Decision = %q, want %q", result.Decision, Deny)
	}
}

func TestSafeEvaluateUnknownScope(t *testing.T) {
	eng, err := LoadFromBytes([]byte(`
scope: test
mode: enforce
rules:
  - name: deny-all
    match:
      operation: "*"
    action: deny
    message: blocked
`))
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	defer eng.Close()

	call := Call{Operation: "anything"}
	_, err = SafeEvaluate(eng, call, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown scope")
	}
}

func TestSafeEvaluatePanicRecovery(t *testing.T) {
	call := Call{Operation: "anything"}
	result, err := SafeEvaluate(nil, call, "test")
	if err == nil {
		t.Fatal("expected error from panic recovery")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("error should mention panic, got: %v", err)
	}
	if result.Decision != Deny {
		t.Errorf("Decision = %q, want %q (fail-closed on panic)", result.Decision, Deny)
	}
}
