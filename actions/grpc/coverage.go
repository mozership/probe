package grpc

import (
	"fmt"
	"os"

	"github.com/bufbuild/protocompile/parser"
	"github.com/bufbuild/protocompile/reporter"
	"github.com/mozership/probe/internal/oas"
	"github.com/mozership/probe/report"
)

// NewCoverage counts the steps of r that called each method the .proto
// file at path declares, as proto.files names the file. The file is parsed
// on its own, without its imports, as its services and methods are all
// that is counted. A .proto file declares no statuses its methods end
// with, so that its coverage is told by methods alone.
func NewCoverage(path string, r *report.Report) (*oas.Coverage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read .proto file: %w", err)
	}
	defer func() { _ = f.Close() }()

	handler := reporter.NewHandler(nil)
	node, err := parser.Parse(path, f, handler)
	if err != nil {
		return nil, fmt.Errorf("failed to parse .proto file: %w", err)
	}
	result, err := parser.ResultFromAST(node, false, handler)
	if err != nil {
		return nil, fmt.Errorf("failed to parse .proto file: %w", err)
	}
	fd := result.FileDescriptorProto()

	var declared []oas.Declared
	for _, s := range fd.GetService() {
		service := s.GetName()
		if pkg := fd.GetPackage(); pkg != "" {
			service = pkg + "." + service
		}
		for _, m := range s.GetMethod() {
			declared = append(declared, oas.Declared{Operation: service + "/" + m.GetName()})
		}
	}
	cov, err := oas.CountCoverage(path, declared, r)
	if err != nil {
		return nil, err
	}
	cov.OperationsOnly = true
	return cov, nil
}
