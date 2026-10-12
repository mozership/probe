package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mozership/probe/jsonutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// streams returns whether md streams its requests and its responses. A
// method that streams both ways is an error, as a call is made here by
// sending all its requests before reading any response, which such a
// method need not answer.
func streams(md protoreflect.MethodDescriptor) (client, server bool, err error) {
	client, server = md.IsStreamingClient(), md.IsStreamingServer()
	if client && server {
		return false, false, fmt.Errorf("%s streams both ways, which the grpc action does not call", md.FullName())
	}
	return client, server, nil
}

// callShape returns how a call to md streams, and the JSON of each request
// it sends, or an error when the step does not fit md: a list in body to a
// method that takes one request, or max_messages to one that answers once.
func (r *Req) callShape(md protoreflect.MethodDescriptor) (clientStream, serverStream bool, bodies []string, err error) {
	if clientStream, serverStream, err = streams(md); err != nil {
		return false, false, nil, err
	}
	if r.MaxMessages > 0 && !serverStream {
		return false, false, nil, fmt.Errorf("max_messages is for a method that streams its responses, which %s does not", md.FullName())
	}
	if bodies, err = requestBodies(r.Body, clientStream); err != nil {
		return false, false, nil, err
	}
	return clientStream, serverStream, bodies, nil
}

// requestBodies returns the JSON of each message body sends. A method that
// streams its requests takes a list, each item of which is one message, or
// one object as the only one; any other method takes one object, and an
// empty body is one empty message to either.
func requestBodies(body string, clientStream bool) ([]string, error) {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "[") {
		return []string{body}, nil
	}
	if !clientStream {
		return nil, errors.New("body is a list, which only a method that streams its requests takes")
	}
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &items); err != nil {
		return nil, fmt.Errorf("failed to unmarshal request JSON: %w", err)
	}
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = string(item)
	}
	return out, nil
}

// buildRequests returns the messages of bodies, as in reads them, each
// checked to be one that can be encoded.
func buildRequests(in protoreflect.MessageDescriptor, bodies []string) ([]*dynamicpb.Message, error) {
	msgs := make([]*dynamicpb.Message, 0, len(bodies))
	for _, body := range bodies {
		msg := dynamicpb.NewMessage(in)
		if body != "" {
			if err := protojson.Unmarshal([]byte(body), msg); err != nil {
				return nil, fmt.Errorf("failed to unmarshal request JSON: %w", err)
			}
		}
		// A message that cannot be encoded, such as one without a required
		// field, cannot be sent either.
		if _, err := proto.Marshal(msg); err != nil {
			return nil, fmt.Errorf("failed to encode the request: %w", err)
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}

// checkRequests is checkRequest for each message of bodies; those of a
// stream are told by their index, such as $[1].user.
func (c *contract) checkRequests(spec protoreflect.MethodDescriptor, bodies []string, clientStream bool) []any {
	if !clientStream {
		return c.checkRequest(spec, bodies[0])
	}
	var out []any
	for i, body := range bodies {
		out = append(out, atIndex(c.checkRequest(spec, body), i)...)
	}
	return out
}

// atIndex returns violations with their field told under the index of the
// message of a stream they were found in: $.user becomes $[1].user, and a
// violation of no field is of $[1].
func atIndex(violations []any, i int) []any {
	prefix := fmt.Sprintf("$[%d]", i)
	for _, v := range violations {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		field, _ := m["field"].(string)
		m["field"] = prefix + strings.TrimPrefix(field, "$")
	}
	return violations
}

// readResponses reads raws, the response messages as they came in, encoded
// with codec, json or proto, by out, the definition the call is made with,
// and checks each against spec when the .proto files are given. res.body is
// the last; a call whose responses stream has every one in res.messages,
// and in res.complete whether the server ended the stream.
func (r *Req) readResponses(res *Res, out protoreflect.MessageDescriptor, spec protoreflect.MethodDescriptor, raws [][]byte, codec string, serverStream, complete bool) error {
	messages := make([]any, 0, len(raws))
	for i, raw := range raws {
		msg := dynamicpb.NewMessage(out)
		var readErr error
		if codec == "proto" {
			readErr = proto.Unmarshal(raw, msg)
		} else {
			readErr = protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(raw, msg)
		}
		body := ""
		if readErr == nil {
			if b, err := protojson.Marshal(msg); err == nil {
				body = string(b)
			}
		}
		var found []any
		if spec != nil {
			if codec == "proto" {
				found = r.contract.checkResponse(spec, raw)
			} else {
				found = r.contract.checkResponseJSON(spec, raw)
			}
			if serverStream {
				found = atIndex(found, i)
			}
			res.violations = append(res.violations, found...)
		}
		// A reply the definition cannot read is an error, unless the files'
		// check has just told it as a reply that breaks them.
		if readErr != nil && len(found) == 0 {
			return fmt.Errorf("failed to read the response: %w", readErr)
		}
		res.Body = body
		messages = append(messages, jsonutil.Decode(body))
	}
	if serverStream {
		res.messages = messages
		res.complete = &complete
	}
	return nil
}

// invokeStream makes a call to a method that streams its requests or its
// responses: it sends msgs, closes its side, and reads the responses until
// the server ends the stream, or until max_messages of them have come.
func (r *Req) invokeStream(ctx context.Context, conn *grpc.ClientConn, answer *answerHandler, md, spec protoreflect.MethodDescriptor, msgs []*dynamicpb.Message, violations []any, clientStream, serverStream bool) (*Res, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	name := fmt.Sprintf("/%s/%s", md.Parent().FullName(), md.Name())
	desc := &grpc.StreamDesc{StreamName: string(md.Name()), ClientStreams: clientStream, ServerStreams: serverStream}
	stream, err := conn.NewStream(answer.mark(ctx), desc, name, grpc.ForceCodec(rawCodec{}))
	if err != nil {
		return nil, err
	}
	// A send fails once the server has ended the stream, which then tells
	// why to the receive.
	for _, msg := range msgs {
		if err := stream.SendMsg(msg); err != nil {
			break
		}
	}
	_ = stream.CloseSend()

	var raws [][]byte
	var recvErr error
	complete := true
	for {
		if serverStream && r.MaxMessages > 0 && len(raws) >= r.MaxMessages {
			complete = false
			break
		}
		var raw []byte
		err := stream.RecvMsg(&raw)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			recvErr = err
			break
		}
		raws = append(raws, raw)
		// A method that answers once has its status read with the answer.
		if !serverStream {
			break
		}
	}
	if !complete {
		cancel()
	}

	res := &Res{StatusCode: "OK", Metadata: map[string]string{}, violations: violations}
	if spec != nil {
		res.matched = r.contract.matched(spec)
	}
	mds := []metadata.MD{stream.Trailer()}
	if header, err := stream.Header(); err == nil {
		mds = append([]metadata.MD{header}, mds...)
	}
	for _, md := range mds {
		for key, values := range md {
			if len(values) > 0 {
				res.Metadata[key] = values[0]
			}
		}
	}

	switch {
	case !complete:
		// The stream was left before the server ended it, so it told no
		// status.
		res.StatusCode = ""
	case recvErr != nil:
		st, ok := status.FromError(recvErr)
		if !ok {
			return nil, recvErr
		}
		// A status made up on this side, such as the timeout running out,
		// is an error, unless messages came before it: the server answered,
		// and the stream ended without its status.
		if !answer.answered.Load() {
			if len(raws) == 0 {
				return nil, recvErr
			}
			complete = false
		}
		res.StatusCode = statusCodeName(st.Code())
		res.StatusMessage = st.Message()
	}

	if err := r.readResponses(res, md.Output(), spec, raws, "proto", serverStream, complete); err != nil {
		return nil, err
	}
	return res, nil
}
