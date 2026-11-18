package grpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	grpc_reflection_v1alpha "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// A listing tells the services a server has, or the methods of one, by the
// server's reflection, or by the .proto files of proto when they are
// given. It reads only, so it is made under --read-only too.

// reflectionServices are the services of reflection itself, which a listing
// leaves out, as no step calls them.
var reflectionServices = []string{
	"grpc.reflection.v1.ServerReflection",
	"grpc.reflection.v1alpha.ServerReflection",
}

// listing is what a listing found: the services, or the methods of one and
// the messages and enums they use.
type listing struct {
	services []any
	methods  []any
	types    map[string]any
}

// doList makes the listing the step asks for, and returns it as the result
// of a call: status_code OK, and the listing in res.
func (r *Req) doList(ctx context.Context) (*Result, error) {
	if r.Body != "" {
		return nil, errors.New("list takes no body: it lists what can be called, and calls nothing")
	}
	if r.MaxMessages > 0 {
		return nil, errors.New("list takes no max_messages: it lists what can be called, and calls nothing")
	}
	if r.Method != "" && r.Service == "" {
		return nil, errors.New("method needs service: list with service and method describes that method")
	}

	result := &Result{Req: *r}
	var l *listing
	var err error
	switch {
	case r.contract != nil:
		// The files tell what the server is meant to have, with nothing
		// sent to it.
		l, err = r.listFiles()
	case r.Protocol == "connect":
		return nil, errors.New("a Connect server is listed only with proto: reflection needs a stream an HTTP/1.1 connection cannot carry")
	default:
		l, err = r.listReflection(ctx)
	}
	if err != nil {
		return nil, err
	}
	result.Res = Res{StatusCode: "OK", Metadata: map[string]string{}, listing: l}
	return result, nil
}

// listFiles lists by the .proto files of proto.
func (r *Req) listFiles() (*listing, error) {
	var services []protoreflect.ServiceDescriptor
	for _, f := range r.contract.files {
		for i := 0; i < f.Services().Len(); i++ {
			services = append(services, f.Services().Get(i))
		}
	}
	if r.Service == "" {
		l := &listing{services: []any{}}
		for _, s := range services {
			l.services = append(l.services, string(s.FullName()))
		}
		slices.SortFunc(l.services, func(a, b any) int { return strings.Compare(a.(string), b.(string)) })
		return l, nil
	}
	for _, s := range services {
		if string(s.Name()) == r.Service || string(s.FullName()) == r.Service {
			return r.describe(s)
		}
	}
	return nil, fmt.Errorf("the .proto files declare no service %s", r.Service)
}

// listReflection lists by the server's reflection.
func (r *Req) listReflection(ctx context.Context) (l *listing, er error) {
	if r.Codec != "" {
		return nil, fmt.Errorf("codec is for protocol connect; a gRPC call is encoded as protobuf")
	}
	creds := insecure.NewCredentials()
	if r.TLS {
		tlsConfig, err := r.tlsConfig()
		if err != nil {
			return nil, err
		}
		creds = credentials.NewTLS(tlsConfig)
	}
	if err := r.checkGRPCHost(); err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(r.Addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
	}
	defer func() {
		if err := conn.Close(); er == nil {
			er = err
		}
	}()
	ctx = metadata.NewOutgoingContext(ctx, metadata.New(r.Metadata))
	client := grpc_reflection_v1alpha.NewServerReflectionClient(conn)

	if r.Service != "" {
		s, err := r.getServiceDescriptor(ctx, client, r.Service)
		if err != nil {
			return nil, fmt.Errorf("failed to get service descriptor: %w", err)
		}
		return r.describe(s)
	}
	names, err := listServices(ctx, client)
	if err != nil {
		return nil, err
	}
	l = &listing{services: []any{}}
	for _, name := range names {
		if !slices.Contains(reflectionServices, name) {
			l.services = append(l.services, name)
		}
	}
	return l, nil
}

// listServices returns the names of the services the server's reflection
// lists, sorted.
func listServices(ctx context.Context, client grpc_reflection_v1alpha.ServerReflectionClient) (names []string, er error) {
	stream, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, reflectionError("failed to create reflection stream", err)
	}
	defer func() {
		if err := stream.CloseSend(); er == nil {
			er = err
		}
	}()
	//nolint:staticcheck // v1alpha reflection API is still widely used
	err = stream.Send(&grpc_reflection_v1alpha.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1alpha.ServerReflectionRequest_ListServices{},
	})
	if err != nil {
		if errors.Is(err, io.EOF) {
			if _, recvErr := stream.Recv(); recvErr != nil && !errors.Is(recvErr, io.EOF) {
				err = recvErr
			}
		}
		return nil, reflectionError("failed to send reflection request", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		if err == io.EOF {
			return nil, errors.New("unexpected end of reflection stream")
		}
		return nil, reflectionError("failed to receive reflection response", err)
	}
	//nolint:staticcheck // v1alpha reflection API is still widely used
	if errResp := resp.GetErrorResponse(); errResp != nil {
		//nolint:staticcheck // v1alpha reflection API is still widely used
		if codes.Code(errResp.GetErrorCode()) == codes.Unimplemented {
			//nolint:staticcheck // v1alpha reflection API is still widely used
			return nil, fmt.Errorf("%w: %s", errNoReflection, errResp.GetErrorMessage())
		}
		//nolint:staticcheck // v1alpha reflection API is still widely used
		return nil, fmt.Errorf("reflection error: %s", errResp.GetErrorMessage())
	}
	//nolint:staticcheck // v1alpha reflection API is still widely used
	list := resp.GetListServicesResponse()
	if list == nil {
		return nil, errors.New("unexpected response type from reflection")
	}
	//nolint:staticcheck // v1alpha reflection API is still widely used
	for _, s := range list.GetService() {
		names = append(names, s.GetName())
	}
	slices.Sort(names)
	return names, nil
}

// describe tells the methods of s, or the method of the step alone, and the
// messages and enums they use, at any depth.
func (r *Req) describe(s protoreflect.ServiceDescriptor) (*listing, error) {
	l := &listing{methods: []any{}, types: map[string]any{}}
	methods := s.Methods()
	for i := 0; i < methods.Len(); i++ {
		m := methods.Get(i)
		if r.Method != "" && string(m.Name()) != r.Method {
			continue
		}
		l.methods = append(l.methods, map[string]any{
			"name":             string(m.Name()),
			"service":          string(s.FullName()),
			"request":          string(m.Input().FullName()),
			"response":         string(m.Output().FullName()),
			"client_streaming": m.IsStreamingClient(),
			"server_streaming": m.IsStreamingServer(),
			"idempotency":      idempotencyLevel(m).String(),
		})
		addMessage(l.types, m.Input())
		addMessage(l.types, m.Output())
	}
	if r.Method != "" && len(l.methods) == 0 {
		return nil, fmt.Errorf("method %s not found in service %s", r.Method, s.FullName())
	}
	return l, nil
}

// addMessage adds m to types by its full name, with each message and enum
// its fields use, once each.
func addMessage(types map[string]any, m protoreflect.MessageDescriptor) {
	name := string(m.FullName())
	if _, ok := types[name]; ok {
		return
	}
	// The message is in types before its fields are, so that one that
	// holds itself, at any depth, is added once.
	types[name] = nil
	fields := []any{}
	var messages []protoreflect.MessageDescriptor
	var enums []protoreflect.EnumDescriptor
	fs := m.Fields()
	for i := 0; i < fs.Len(); i++ {
		f := fs.Get(i)
		field := map[string]any{
			"name":      string(f.Name()),
			"json_name": f.JSONName(),
			"number":    int(f.Number()),
			"type":      fieldType(f),
		}
		if o := f.ContainingOneof(); o != nil && !o.IsSynthetic() {
			field["oneof"] = string(o.Name())
		}
		if f.HasPresence() && f.ContainingOneof() != nil && f.ContainingOneof().IsSynthetic() {
			field["optional"] = true
		}
		fields = append(fields, field)
		for _, v := range []protoreflect.FieldDescriptor{f, f.MapKey(), f.MapValue()} {
			if v == nil {
				continue
			}
			if v.Message() != nil && !v.IsMap() {
				messages = append(messages, v.Message())
			}
			if v.Enum() != nil {
				enums = append(enums, v.Enum())
			}
		}
	}
	types[name] = map[string]any{"kind": "message", "fields": fields}
	for _, e := range enums {
		addEnum(types, e)
	}
	for _, sub := range messages {
		addMessage(types, sub)
	}
}

// addEnum adds e to types by its full name, with its values.
func addEnum(types map[string]any, e protoreflect.EnumDescriptor) {
	name := string(e.FullName())
	if _, ok := types[name]; ok {
		return
	}
	values := []any{}
	for i := 0; i < e.Values().Len(); i++ {
		values = append(values, string(e.Values().Get(i).Name()))
	}
	types[name] = map[string]any{"kind": "enum", "values": values}
}
