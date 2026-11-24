package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mozership/probe/internal/smtptest"
)

func main() {
	// Default SMTP port
	addr := ":25"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	// Create mock SMTP server
	server := &smtptest.Server{
		Addr: addr,
		Name: "probe-test-smtp.local",
		Log:  log.New(os.Stdout, "[SMTP] ", log.LstdFlags),
	}

	fmt.Printf("Starting SMTP test server on %s...\n", addr)
	fmt.Printf("Server name: %s\n", server.Name)
	fmt.Println("Press Ctrl+C to stop")

	// Start server in background
	go func() {
		if err := server.Serve(); err != nil {
			log.Fatalf("Failed to start SMTP server: %v", err)
		}
	}()

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("\nShutting down SMTP server...")
}
