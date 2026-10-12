package grpc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozership/probe/oas"
	"github.com/mozership/probe/report"
)

func TestNewCoverage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.proto")
	// The imports are not at hand: the file is parsed on its own.
	content := `syntax = "proto3";
package users.v1;
import "buf/validate/validate.proto";
import "users/v1/messages.proto";
service UserService {
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
  rpc DeleteUser(DeleteUserRequest) returns (DeleteUserResponse);
}
service AdminService {
  rpc Ban(BanRequest) returns (BanResponse);
}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &report.Report{Jobs: []report.Job{{Steps: []report.Step{
		{Contract: &report.Contract{Spec: path, Operation: "users.v1.UserService/GetUser"}},
		{Contract: &report.Contract{Spec: path, Operation: "users.v1.UserService/GetUser"}},
		{Contract: &report.Contract{Spec: path, Operation: "users.v1.AdminService/Ban"}},
	}}}}

	cov, err := NewCoverage(path, r)
	if err != nil {
		t.Fatal(err)
	}
	want := []oas.OperationCoverage{
		{Operation: "users.v1.UserService/GetUser", Steps: 2},
		{Operation: "users.v1.UserService/DeleteUser"},
		{Operation: "users.v1.AdminService/Ban", Steps: 1},
	}
	if len(cov.Operations) != len(want) {
		t.Fatalf("operations = %+v, want %+v", cov.Operations, want)
	}
	for i := range want {
		if cov.Operations[i].Operation != want[i].Operation || cov.Operations[i].Steps != want[i].Steps {
			t.Errorf("operations[%d] = %+v, want %+v", i, cov.Operations[i], want[i])
		}
	}
	if !cov.OperationsOnly {
		t.Error("OperationsOnly = false, want true: a .proto file declares no responses")
	}
}

func TestNewCoverageErrors(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.proto")
	if err := os.WriteFile(broken, []byte("service {"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &report.Report{}
	for path, want := range map[string]string{
		filepath.Join(dir, "none.proto"): "failed to read .proto file",
		broken:                           "failed to parse .proto file",
	} {
		if _, err := NewCoverage(path, r); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("NewCoverage(%s) = %v, want %q", path, err, want)
		}
	}
}
