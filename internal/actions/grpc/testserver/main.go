package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	var (
		port        = flag.String("port", "50051", "Port to listen on")
		tls         = flag.Bool("tls", false, "Enable TLS")
		certFile    = flag.String("cert", "", "Path to TLS certificate file")
		keyFile     = flag.String("key", "", "Path to TLS private key file")
		connectPort = flag.String("connect-port", "", "Port to serve the Connect protocol on, over HTTP/1.1; none when empty")
		help        = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *help {
		fmt.Print("Test gRPC Server for probe\n\n")
		fmt.Print("Options:\n")
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("The server provides a UserService with the following methods:")
		fmt.Println("  - GetUser(user_id) -> User")
		fmt.Println("  - CreateUser(user) -> User")
		fmt.Println("  - UpdateUser(user_id, updates) -> User")
		fmt.Println("  - ListUsers(page_size, filter) -> Users[]")
		fmt.Println("  - DeleteUser(user_id) -> Success")
		return
	}

	// Create and start the test server
	server := NewServer()

	// Set the specified port
	server.SetPort(*port)

	// Configure TLS if enabled
	if *tls {
		if *certFile == "" || *keyFile == "" {
			log.Fatal("TLS enabled but -cert and -key flags are required")
		}
		server.SetTLS(*tls, *certFile, *keyFile)
		fmt.Printf("Starting gRPC test server with TLS on port %s...\n", *port)
	} else {
		fmt.Printf("Starting gRPC test server on port %s...\n", *port)
	}

	err := server.Start()
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	fmt.Printf("Serving gRPC on port %s...\n", *port)

	if *connectPort != "" {
		if err := server.StartConnect(*connectPort); err != nil {
			log.Fatalf("Failed to start the Connect server: %v", err)
		}
		fmt.Printf("Serving the Connect protocol on port %s...\n", *connectPort)
	}

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	server.Stop()
}
