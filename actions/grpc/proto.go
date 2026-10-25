package grpc

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"buf.build/go/protovalidate"
	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/linker"
	"github.com/mozership/probe/internal/contractcheck"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// contract checks a call against .proto files, which say what the service
// takes and answers independently of the server, whose own definition the
// call is made with.
type contract struct {
	files linker.Files
	// paths are the files given, as proto.files writes them, by the name
	// each is compiled under.
	paths map[string]string
	// request is whether the request is checked as well as the response.
	request bool
	// strict is whether a field the .proto files do not declare breaks
	// them.
	strict bool
	// validator checks the rules the files annotate fields with by
	// buf.validate.
	validator protovalidate.Validator
}

// takeProto removes proto from the parameters and returns the contract it
// names, or nil when the step checks nothing: when proto is missing, or
// false, as a step writes it to leave out a check its job's defaults ask
// for. The files are compiled before the call is made, so that ones that
// cannot be compiled fail the step without calling anything.
func takeProto(m map[string]any) (*contract, error) {
	v, ok := m["proto"]
	if !ok {
		return nil, nil
	}
	delete(m, "proto")

	switch o := v.(type) {
	case bool:
		if o {
			return nil, errors.New("proto must be a map with files, or false")
		}
		return nil, nil
	case map[string]any:
		for k := range o {
			switch k {
			case "files", "import_paths", "request", "strict":
			default:
				return nil, fmt.Errorf("proto takes files, import_paths, request and strict, not %s", k)
			}
		}
		files, err := stringList(o, "files")
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			return nil, errors.New("proto.files must list the .proto files of the service")
		}
		importPaths, err := stringList(o, "import_paths")
		if err != nil {
			return nil, err
		}
		request, err := contractcheck.Flag(o, "proto", "request", true)
		if err != nil {
			return nil, err
		}
		strict, err := contractcheck.Flag(o, "proto", "strict", false)
		if err != nil {
			return nil, err
		}
		c, err := loadContract(files, importPaths)
		if err != nil {
			return nil, err
		}
		c.request, c.strict = request, strict
		return c, nil
	default:
		return nil, errors.New("proto must be a map with files, or false")
	}
}

func stringList(o map[string]any, key string) ([]string, error) {
	v, ok := o[key]
	if !ok {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("proto.%s must be a list of paths", key)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok || s == "" {
			return nil, fmt.Errorf("proto.%s must be a list of paths", key)
		}
		out = append(out, s)
	}
	return out, nil
}

// loadContract compiles the .proto files, as protoc would with the import
// paths given, the working directory when none is. Each file is named by
// its path under the first import path that holds it, as an import of it
// names it.
func loadContract(files, importPaths []string) (*contract, error) {
	if len(importPaths) == 0 {
		importPaths = []string{"."}
	}
	names := make([]string, 0, len(files))
	paths := map[string]string{}
	for _, f := range files {
		name, err := importName(f, importPaths)
		if err != nil {
			return nil, err
		}
		names = append(names, name)
		paths[name] = f
	}
	sources := &protocompile.SourceResolver{ImportPaths: importPaths}
	resolver := protocompile.ResolverFunc(func(path string) (protocompile.SearchResult, error) {
		if slices.Contains(annotationFiles, path) {
			if fd, err := protoregistry.GlobalFiles.FindFileByPath(path); err == nil {
				return protocompile.SearchResult{Proto: protodesc.ToFileDescriptorProto(fd)}, nil
			}
		}
		return sources.FindFileByPath(path)
	})
	compiler := protocompile.Compiler{Resolver: protocompile.WithStandardImports(resolver)}
	compiled, err := compiler.Compile(context.Background(), names...)
	if err != nil {
		return nil, fmt.Errorf("proto.files: %w", err)
	}
	validator, err := protovalidate.New()
	if err != nil {
		return nil, fmt.Errorf("proto.files: %w", err)
	}
	return &contract{files: compiled, paths: paths, validator: validator}, nil
}

// importName returns the name file is imported by: its path under the
// first of importPaths that holds it.
func importName(file string, importPaths []string) (string, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", fmt.Errorf("proto.files: %w", err)
	}
	for _, dir := range importPaths {
		d, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		// A path under the directory does not lead out of it by ..,
		// though its first name may start with two dots.
		if rel, err := filepath.Rel(d, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel), nil
		}
	}
	return "", fmt.Errorf("proto.files: %s is under none of the import paths %s", file, strings.Join(importPaths, ", "))
}

// method returns the method the .proto files declare for service, by its
// name or its full name, and method, or nil when they declare none.
func (c *contract) method(service, method string) protoreflect.MethodDescriptor {
	for _, f := range c.files {
		services := f.Services()
		for i := 0; i < services.Len(); i++ {
			s := services.Get(i)
			if string(s.Name()) == service || string(s.FullName()) == service {
				return s.Methods().ByName(protoreflect.Name(method))
			}
		}
	}
	return nil
}

// matched returns what the call was matched to in the .proto files, which
// a report of their coverage counts: the file that declares the service, as
// proto.files gives it, and the method, such as users.v1.UserService/GetUser.
func (c *contract) matched(m protoreflect.MethodDescriptor) map[string]any {
	file := m.ParentFile().Path()
	if p, ok := c.paths[file]; ok {
		file = p
	}
	return map[string]any{
		"spec":      file,
		"operation": string(m.Parent().FullName()) + "/" + string(m.Name()),
	}
}

// checkRequest returns what in body, the JSON of the request, the request
// message the .proto files declare does not take. An empty body is an empty
// message, which a message with a required field does not take.
func (c *contract) checkRequest(spec protoreflect.MethodDescriptor, body string) []any {
	if !c.request {
		return nil
	}
	if body == "" {
		body = "{}"
	}
	msg := dynamicpb.NewMessage(spec.Input())
	if err := protojson.Unmarshal([]byte(body), msg); err != nil {
		return []any{contractcheck.Violation("request", fmt.Sprintf("request body does not keep to %s", spec.Input().FullName()), err.Error(), "")}
	}
	return c.constraints("request", msg)
}

// checkDefinition returns where the server's definition of the method, as
// its reflection tells it, differs from the one the .proto files declare:
// the messages it takes and answers, and their fields at any depth, by
// number, name, kind and cardinality. A field only the server declares
// breaks the files under strict.
func (c *contract) checkDefinition(spec, server protoreflect.MethodDescriptor) []any {
	var out []any
	if spec.IsStreamingClient() != server.IsStreamingClient() || spec.IsStreamingServer() != server.IsStreamingServer() {
		out = append(out, contractcheck.Violation("response", fmt.Sprintf("the server's definition of %s %s", spec.Name(), streamingKind(server)), fmt.Sprintf("the .proto files declare that it %s", streamingKind(spec)), ""))
	}
	pairs := []struct {
		what         string
		spec, server protoreflect.MessageDescriptor
	}{
		{"request", spec.Input(), server.Input()},
		{"response", spec.Output(), server.Output()},
	}
	seen := map[[2]protoreflect.FullName]bool{}
	for _, p := range pairs {
		if p.spec.FullName() != p.server.FullName() {
			out = append(out, contractcheck.Violation("response", fmt.Sprintf("the server's %s message is %s", p.what, p.server.FullName()), fmt.Sprintf("the .proto files declare %s", p.spec.FullName()), ""))
			continue
		}
		out = append(out, c.compareMessages(p.spec, p.server, "$", seen)...)
	}
	return out
}

func (c *contract) compareMessages(spec, server protoreflect.MessageDescriptor, path string, seen map[[2]protoreflect.FullName]bool) []any {
	key := [2]protoreflect.FullName{spec.FullName(), server.FullName()}
	if seen[key] {
		return nil
	}
	seen[key] = true

	var out []any
	sf := spec.Fields()
	for i := 0; i < sf.Len(); i++ {
		s := sf.Get(i)
		field := path + "." + string(s.Name())
		v := server.Fields().ByNumber(s.Number())
		if v == nil {
			out = append(out, contractcheck.Violation("response", fmt.Sprintf("the server's %s declares no field %d", server.FullName(), s.Number()), fmt.Sprintf("the .proto files declare %s %s = %d", fieldType(s), s.Name(), s.Number()), field))
			continue
		}
		if v.Name() != s.Name() || fieldType(v) != fieldType(s) {
			out = append(out, contractcheck.Violation("response", fmt.Sprintf("the server's %s declares %s %s = %d", server.FullName(), fieldType(v), v.Name(), v.Number()), fmt.Sprintf("the .proto files declare %s %s = %d", fieldType(s), s.Name(), s.Number()), field))
			continue
		}
		// proto2 tells a required field from an optional one, which the type
		// alone does not.
		if v.Cardinality() != s.Cardinality() {
			out = append(out, contractcheck.Violation("response", fmt.Sprintf("the server's %s declares %s %s as %s", server.FullName(), fieldType(v), v.Name(), v.Cardinality()), fmt.Sprintf("the .proto files declare it %s", s.Cardinality()), field))
			continue
		}
		if s.Message() != nil && !s.IsMap() {
			out = append(out, c.compareMessages(s.Message(), v.Message(), field, seen)...)
		}
		if s.IsMap() && s.MapValue().Message() != nil {
			out = append(out, c.compareMessages(s.MapValue().Message(), v.MapValue().Message(), field+"[]", seen)...)
		}
	}
	if c.strict {
		vf := server.Fields()
		for i := 0; i < vf.Len(); i++ {
			v := vf.Get(i)
			if sf.ByNumber(v.Number()) == nil {
				out = append(out, contractcheck.Violation("response", fmt.Sprintf("the server's %s declares %s %s = %d", server.FullName(), fieldType(v), v.Name(), v.Number()), "the .proto files do not declare it", path+"."+string(v.Name())))
			}
		}
	}
	return out
}

// streamingKind says which ways md streams.
func streamingKind(md protoreflect.MethodDescriptor) string {
	switch {
	case md.IsStreamingClient() && md.IsStreamingServer():
		return "streams both ways"
	case md.IsStreamingClient():
		return "streams its requests"
	case md.IsStreamingServer():
		return "streams its responses"
	default:
		return "streams neither way"
	}
}

// fieldType is the type of a field as a .proto file writes it, such as
// repeated string, map<string, int32> or users.Profile.
func fieldType(f protoreflect.FieldDescriptor) string {
	if f.IsMap() {
		return fmt.Sprintf("map<%s, %s>", fieldType(f.MapKey()), fieldType(f.MapValue()))
	}
	t := f.Kind().String()
	switch f.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		t = string(f.Message().FullName())
	case protoreflect.EnumKind:
		t = string(f.Enum().FullName())
	}
	if f.IsList() {
		return "repeated " + t
	}
	return t
}

// checkResponse returns what in the response, data as it came in, the
// message the .proto files declare does not take. It is read by the files'
// definition: a field whose number the files declare but whose encoding is
// not theirs is left unread, and breaks them, as a field the files do not
// declare does under strict, and a reply that cannot be read at all breaks
// them too.
func (c *contract) checkResponse(spec protoreflect.MethodDescriptor, data []byte) []any {
	msg := dynamicpb.NewMessage(spec.Output())
	if err := proto.Unmarshal(data, msg); err != nil {
		return []any{contractcheck.Violation("response", fmt.Sprintf("response body does not read as %s", spec.Output().FullName()), err.Error(), "")}
	}
	return append(c.unread(msg, "$"), c.constraints("response", msg)...)
}

// checkResponseJSON is checkResponse for a response that came in as JSON, as
// a Connect call with codec json answers: a value of another type than the
// files declare breaks them, as a field they do not declare does under
// strict.
func (c *contract) checkResponseJSON(spec protoreflect.MethodDescriptor, data []byte) []any {
	msg := dynamicpb.NewMessage(spec.Output())
	if err := (protojson.UnmarshalOptions{DiscardUnknown: !c.strict}).Unmarshal(data, msg); err != nil {
		return []any{contractcheck.Violation("response", fmt.Sprintf("response body does not read as %s", spec.Output().FullName()), err.Error(), "")}
	}
	return c.constraints("response", msg)
}

// unread returns a violation for each field of msg, at any depth, that was
// left unread: one whose encoding is not the one the .proto files declare
// for its number, and under strict one whose number they do not declare.
func (c *contract) unread(msg protoreflect.Message, path string) []any {
	var out []any
	fields := msg.Descriptor().Fields()
	b := msg.GetUnknown()
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeField(b)
		if n < 0 {
			break
		}
		b = b[n:]
		if f := fields.ByNumber(num); f != nil {
			out = append(out, contractcheck.Violation("response", fmt.Sprintf("field %d of %s is not encoded as %s %s", num, msg.Descriptor().FullName(), fieldType(f), f.Name()), fmt.Sprintf("it arrived as wire type %d", typ), path+"."+string(f.Name())))
		} else if c.strict {
			out = append(out, contractcheck.Violation("response", fmt.Sprintf("field %d of %s is not declared in the .proto files", num, msg.Descriptor().FullName()), "the server sends a field the files do not declare", path))
		}
	}

	var names []string
	msg.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		names = append(names, string(f.Name()))
		return true
	})
	sort.Strings(names)
	for _, name := range names {
		f := fields.ByName(protoreflect.Name(name))
		v := msg.Get(f)
		field := path + "." + name
		switch {
		case f.IsMap():
			if f.MapValue().Message() == nil {
				continue
			}
			keys := []protoreflect.MapKey{}
			v.Map().Range(func(k protoreflect.MapKey, _ protoreflect.Value) bool {
				keys = append(keys, k)
				return true
			})
			sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
			for _, k := range keys {
				out = append(out, c.unread(v.Map().Get(k).Message(), fmt.Sprintf("%s[%s]", field, k.String()))...)
			}
		case f.IsList():
			if f.Message() == nil {
				continue
			}
			for i := 0; i < v.List().Len(); i++ {
				out = append(out, c.unread(v.List().Get(i).Message(), fmt.Sprintf("%s[%d]", field, i))...)
			}
		case f.Message() != nil:
			out = append(out, c.unread(v.Message(), field)...)
		}
	}
	return out
}

// hasRequestViolation reports whether violations hold one found in the
// request.
func hasRequestViolation(violations []any) bool {
	for _, v := range violations {
		if m, ok := v.(map[string]any); ok && m["in"] == "request" {
			return true
		}
	}
	return false
}
