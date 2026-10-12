package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"connectrpc.com/connect"
	"github.com/mozership/probe/actions/grpc/testserver/pb"
	"google.golang.org/grpc/status"
)

// unary serves method with the Connect protocol, answering an error
// with the code of the gRPC status it carries.
func unary[Req, Res any](mux *http.ServeMux, procedure string, method func(context.Context, *Req) (*Res, error)) {
	mux.Handle(procedure, connect.NewUnaryHandler(procedure, func(ctx context.Context, req *connect.Request[Req]) (*connect.Response[Res], error) {
		res, err := method(ctx, req.Msg)
		if err != nil {
			st := status.Convert(err)
			return nil, connect.NewError(connect.Code(st.Code()), errors.New(st.Message()))
		}
		return connect.NewResponse(res), nil
	}))
}

// StartConnect serves the UserService with the Connect protocol on port,
// over HTTP/1.1, as a server behind a proxy that passes no HTTP/2 is
// reached.
func (s *Server) StartConnect(port string) error {
	mux := http.NewServeMux()
	unary(mux, "/UserService/GetUser", s.GetUser)
	unary(mux, "/UserService/CreateUser", s.CreateUser)
	unary(mux, "/UserService/UpdateUser", s.UpdateUser)
	unary(mux, "/UserService/ListUsers", s.ListUsers)
	unary(mux, "/UserService/DeleteUser", s.DeleteUser)
	mux.Handle("/UserService/WatchUsers", connect.NewServerStreamHandler("/UserService/WatchUsers",
		func(ctx context.Context, req *connect.Request[pb.WatchUsersRequest], stream *connect.ServerStream[pb.User]) error {
			return watchUsers(ctx, req.Msg, stream.Send)
		}))
	mux.Handle("/UserService/ImportUsers", connect.NewClientStreamHandler("/UserService/ImportUsers",
		func(ctx context.Context, stream *connect.ClientStream[pb.User]) (*connect.Response[pb.ImportUsersResponse], error) {
			n := 0
			for stream.Receive() {
				n++
			}
			if err := stream.Err(); err != nil {
				return nil, err
			}
			return connect.NewResponse(&pb.ImportUsersResponse{Imported: int32(n)}), nil
		}))

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	s.connect = &http.Server{Handler: mux}
	go func() {
		if err := s.connect.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("Failed to serve Connect: %v\n", err)
		}
	}()
	return nil
}
