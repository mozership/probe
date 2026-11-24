package grpc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// The flags of an envelope of the Connect protocol's streams.
const (
	connectEnvelopeCompressed = 0b01
	connectEnvelopeEndStream  = 0b10
)

// connectEnd is the message that ends a stream of the Connect protocol,
// with the error the call ends with, if any, and the trailers.
type connectEnd struct {
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Metadata map[string][]string `json:"metadata"`
}

// writeEnvelope writes data in an envelope: a byte of flags, its length in
// four bytes, and data.
func writeEnvelope(w *bytes.Buffer, flags byte, data []byte) {
	var head [5]byte
	head[0] = flags
	binary.BigEndian.PutUint32(head[1:], uint32(len(data)))
	w.Write(head[:])
	w.Write(data)
}

// readEnvelope reads one envelope from r: io.EOF when r ends before one.
// The message is read as it comes rather than into a buffer of the length
// the envelope gives, so that a length no message follows takes no memory.
func readEnvelope(r io.Reader) (byte, []byte, error) {
	var head [5]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, nil, err
	}
	size := int64(binary.BigEndian.Uint32(head[1:]))
	var data bytes.Buffer
	if n, err := io.CopyN(&data, r, size); n < size {
		if err == nil || errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return 0, nil, err
	}
	return head[0], data.Bytes(), nil
}

// invokeConnectStream makes a call to a method that streams its requests or
// its responses with the Connect protocol, to url: the requests are sent in
// envelopes, and the responses come back in them, ended by one that holds
// the error the call ends with, if any, and the trailers. The responses are
// read until that end, or until max_messages of them have come.
func (r *Req) invokeConnectStream(ctx context.Context, client *http.Client, url, codec string, timeout time.Duration, spec protoreflect.MethodDescriptor, msgs []*dynamicpb.Message, violations []any, serverStream bool) (*Res, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var body bytes.Buffer
	for _, msg := range msgs {
		var data []byte
		var err error
		if codec == "proto" {
			data, err = proto.Marshal(msg)
		} else {
			data, err = protojson.Marshal(msg)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to encode the request: %w", err)
		}
		writeEnvelope(&body, 0, data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, fmt.Errorf("failed to make the request: %w", err)
	}
	for k, v := range r.Metadata {
		req.Header.Set(k, v)
	}
	contentType := "application/connect+" + codec
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Connect-Protocol-Version", connectProtocolVersion)
	if ms, ok := connectTimeout(timeout); ok {
		req.Header.Set("Connect-Timeout-Ms", ms)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	res := &Res{StatusCode: "OK", Metadata: connectMetadata(resp.Header), violations: violations, matched: r.contract.matched(spec)}

	// A call refused before its stream, such as one to a method the server
	// does not serve, is answered by the HTTP status alone.
	if resp.StatusCode != http.StatusOK {
		res.StatusCode = statusCodeName(httpToCode(resp.StatusCode))
		res.StatusMessage = resp.Status
		return res, nil
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaType != contentType {
		code := codes.Internal
		if !strings.HasPrefix(mediaType, "application/connect+") {
			code = codes.Unknown
		}
		res.StatusCode = statusCodeName(code)
		res.StatusMessage = fmt.Sprintf("invalid content-type: %q; expecting %q", resp.Header.Get("Content-Type"), contentType)
		return res, nil
	}

	// The stream is complete once the server ends it with its end message;
	// one left after max_messages, or broken off, is not.
	var raws [][]byte
	complete, stopped := false, false
read:
	for {
		if serverStream && r.MaxMessages > 0 && len(raws) >= r.MaxMessages {
			stopped = true
			break
		}
		flags, data, err := readEnvelope(resp.Body)
		switch {
		case errors.Is(err, io.EOF):
			// A stream that ends with neither a message nor its end holds
			// nothing to read, as a reply that cannot be read does not.
			if len(raws) == 0 {
				return nil, errors.New("failed to read the response: the stream ended without any message or its end message")
			}
			// The server answered, but ended the stream without its end.
			res.StatusCode = statusCodeName(codes.Internal)
			res.StatusMessage = "the stream ended without its end message"
			break read
		case err != nil:
			// The stream broke off. Without any message the server
			// answered nothing to test, which is an error.
			if len(raws) == 0 {
				return nil, err
			}
			code := codes.Unknown
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				code = codes.DeadlineExceeded
			}
			res.StatusCode = statusCodeName(code)
			res.StatusMessage = err.Error()
			break read
		case flags&connectEnvelopeEndStream != 0:
			readConnectEnd(res, data)
			complete = true
			break read
		case flags&connectEnvelopeCompressed != 0:
			return nil, errors.New("failed to read the response: the server compressed a message, which was not asked for")
		default:
			raws = append(raws, data)
		}
	}
	if stopped {
		// The stream was left before the server ended it, so it told no
		// status.
		cancel()
		res.StatusCode = ""
	}

	if err := r.readResponses(res, spec.Output(), spec, raws, codec, serverStream, complete); err != nil {
		return nil, err
	}
	return res, nil
}

// readConnectEnd sets the status and the trailers of res from data, the end
// message of a stream: the error it holds, or OK with none. A trailer wins
// over a header of the same name.
func readConnectEnd(res *Res, data []byte) {
	var end connectEnd
	if err := json.Unmarshal(data, &end); err != nil {
		res.StatusCode = statusCodeName(codes.Internal)
		res.StatusMessage = fmt.Sprintf("the end message of the stream is not JSON: %v", err)
		return
	}
	for k, v := range end.Metadata {
		if len(v) > 0 {
			res.Metadata[strings.ToLower(k)] = v[0]
		}
	}
	if end.Error == nil {
		return
	}
	code, ok := connectCodes[end.Error.Code]
	if !ok {
		code = codes.Unknown
	}
	res.StatusCode = statusCodeName(code)
	res.StatusMessage = end.Error.Message
}
