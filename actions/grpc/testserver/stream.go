package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/mozership/probe/actions/grpc/testserver/pb"
	"google.golang.org/grpc"
)

// WatchUsers sends count users, numbered from 1, or when count is 0 one
// every 20ms until the call ends.
func (s *Server) WatchUsers(req *pb.WatchUsersRequest, stream grpc.ServerStreamingServer[pb.User]) error {
	return watchUsers(stream.Context(), req, stream.Send)
}

// ImportUsers counts the users the client sends.
func (s *Server) ImportUsers(stream grpc.ClientStreamingServer[pb.User, pb.ImportUsersResponse]) error {
	n := 0
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&pb.ImportUsersResponse{Imported: int32(n)})
		}
		if err != nil {
			return err
		}
		n++
	}
}

func watchUsers(ctx context.Context, req *pb.WatchUsersRequest, send func(*pb.User) error) error {
	for i := 1; req.Count == 0 || i <= int(req.Count); i++ {
		if err := send(&pb.User{Id: strconv.Itoa(i), Name: fmt.Sprintf("User %d", i)}); err != nil {
			return err
		}
		if req.Count == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	return nil
}
