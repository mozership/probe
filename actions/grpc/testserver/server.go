package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/mozership/probe/actions/grpc/testserver/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

// Server implements the UserService for testing
type Server struct {
	pb.UnimplementedUserServiceServer
	users  map[string]*pb.User
	nextID int
	mu     sync.RWMutex
	server *grpc.Server
	// connect serves the service with the Connect protocol, when it is
	// started.
	connect  *http.Server
	address  string
	port     string
	tls      bool
	certFile string
	keyFile  string
}

// NewServer creates a new test server instance
func NewServer() *Server {
	return &Server{
		users:  make(map[string]*pb.User),
		nextID: 1,
		port:   "0", // Default to random port
	}
}

// SetPort sets the port for the server to listen on
// Use "0" for random port assignment
func (s *Server) SetPort(port string) {
	s.port = port
}

// SetTLS configures TLS settings for the server
func (s *Server) SetTLS(tls bool, certFile, keyFile string) {
	s.tls = tls
	s.certFile = certFile
	s.keyFile = keyFile
}

// Start starts the test server on the configured port
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", ":"+s.port)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.address = lis.Addr().String()

	// Configure TLS if enabled
	var opts []grpc.ServerOption
	if s.tls {
		if s.certFile == "" || s.keyFile == "" {
			return fmt.Errorf("TLS enabled but cert or key file not specified")
		}

		cert, err := tls.LoadX509KeyPair(s.certFile, s.keyFile)
		if err != nil {
			return fmt.Errorf("failed to load TLS certificates: %w", err)
		}

		creds := credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.NoClientCert, // Allow connections without client certificates
		})
		opts = append(opts, grpc.Creds(creds))
	}

	s.server = grpc.NewServer(opts...)

	// Register the service
	pb.RegisterUserServiceServer(s.server, s)

	// Enable reflection for dynamic discovery
	reflection.Register(s.server)

	// Start serving in a goroutine
	go func() {
		if err := s.server.Serve(lis); err != nil {
			fmt.Printf("Failed to serve: %v\n", err)
		}
	}()

	// Wait a bit for server to start
	time.Sleep(100 * time.Millisecond)

	// Add some test data
	s.seedTestData()

	return nil
}

// Stop stops the test server
func (s *Server) Stop() {
	if s.server != nil {
		s.server.GracefulStop()
	}
	if s.connect != nil {
		_ = s.connect.Close()
	}
}

// GetAddress returns the server's listening address
func (s *Server) GetAddress() string {
	return s.address
}

// seedTestData adds some initial test data
func (s *Server) seedTestData() {
	s.mu.Lock()
	defer s.mu.Unlock()

	testUser := &pb.User{
		Id:    "123",
		Name:  "Test User",
		Email: "test@example.com",
		Profile: &pb.Profile{
			Age:      30,
			Location: "Tokyo",
		},
		Preferences: []string{"email_notifications"},
		CreatedAt:   time.Now().Format(time.RFC3339),
	}
	s.users["123"] = testUser
}

// GetUser implements the GetUser RPC
func (s *Server) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.GetUserResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, exists := s.users[req.UserId]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "user %s not found", req.UserId)
	}

	// Create a copy to avoid modifying the original
	responseUser := &pb.User{
		Id:        user.Id,
		Name:      user.Name,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
	}

	if req.IncludeProfile && user.Profile != nil {
		responseUser.Profile = &pb.Profile{
			Age:      user.Profile.Age,
			Location: user.Profile.Location,
		}
	}

	responseUser.Preferences = make([]string, len(user.Preferences))
	copy(responseUser.Preferences, user.Preferences)

	return &pb.GetUserResponse{
		User: responseUser,
	}, nil
}

// CreateUser implements the CreateUser RPC
func (s *Server) CreateUser(ctx context.Context, req *pb.CreateUserRequest) (*pb.CreateUserResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate new ID
	newID := strconv.Itoa(s.nextID)
	s.nextID++

	// Create new user
	newUser := &pb.User{
		Id:        newID,
		Name:      req.User.Name,
		Email:     req.User.Email,
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	if req.User.Profile != nil {
		newUser.Profile = &pb.Profile{
			Age:      req.User.Profile.Age,
			Location: req.User.Profile.Location,
		}
	}

	// Use preferences from request or user object
	if len(req.Preferences) > 0 {
		newUser.Preferences = make([]string, len(req.Preferences))
		copy(newUser.Preferences, req.Preferences)
	} else if len(req.User.Preferences) > 0 {
		newUser.Preferences = make([]string, len(req.User.Preferences))
		copy(newUser.Preferences, req.User.Preferences)
	}

	s.users[newID] = newUser

	return &pb.CreateUserResponse{
		User: newUser,
	}, nil
}

// UpdateUser implements the UpdateUser RPC
func (s *Server) UpdateUser(ctx context.Context, req *pb.UpdateUserRequest) (*pb.UpdateUserResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.users[req.UserId]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "user %s not found", req.UserId)
	}

	// Update fields if provided
	if req.Updates.Name != "" {
		user.Name = req.Updates.Name
	}
	if req.Updates.Email != "" {
		user.Email = req.Updates.Email
	}
	if req.Updates.Profile != nil {
		if user.Profile == nil {
			user.Profile = &pb.Profile{}
		}
		if req.Updates.Profile.Age > 0 {
			user.Profile.Age = req.Updates.Profile.Age
		}
		if req.Updates.Profile.Location != "" {
			user.Profile.Location = req.Updates.Profile.Location
		}
	}
	if len(req.Updates.Preferences) > 0 {
		user.Preferences = make([]string, len(req.Updates.Preferences))
		copy(user.Preferences, req.Updates.Preferences)
	}

	return &pb.UpdateUserResponse{
		User: user,
	}, nil
}

// ListUsers implements the ListUsers RPC
func (s *Server) ListUsers(ctx context.Context, req *pb.ListUsersRequest) (*pb.ListUsersResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var users []*pb.User
	for _, user := range s.users {
		// Apply filters if specified
		if req.Filter != nil {
			if activeFilter, exists := req.Filter["active"]; exists {
				if activeFilter != "true" {
					continue // Skip inactive users for this simple test
				}
			}
		}
		users = append(users, user)
	}

	// Simple pagination
	pageSize := int(req.PageSize)
	if pageSize <= 0 {
		pageSize = 10
	}

	if len(users) > pageSize {
		users = users[:pageSize]
	}

	return &pb.ListUsersResponse{
		Users:         users,
		TotalCount:    int32(len(s.users)),
		NextPageToken: "", // Simple implementation without pagination
	}, nil
}

// DeleteUser implements the DeleteUser RPC
func (s *Server) DeleteUser(ctx context.Context, req *pb.DeleteUserRequest) (*pb.DeleteUserResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.users[req.UserId]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "user %s not found", req.UserId)
	}

	delete(s.users, req.UserId)

	return &pb.DeleteUserResponse{
		Success: true,
	}, nil
}
