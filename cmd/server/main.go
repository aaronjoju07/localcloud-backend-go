package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"

	"google.golang.org/grpc"

	"github.com/aaronjoju07/localcloud-backend-go/internal/db"
	"github.com/aaronjoju07/localcloud-backend-go/internal/grpcserver"
)

func main() {
	// load env
	addr := fmt.Sprintf("%s:%s", os.Getenv("SERVER_HOST"), os.Getenv("GRPC_PORT"))
	log.Printf("starting gRPC server on %s", addr)

	// connect DB
	conn, err := db.Connect(context.Background())
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer conn.Close()

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	connCtx := context.Background()
	dbConn, err := conn.Acquire(connCtx)
	if err != nil {
		log.Fatalf("acquire db connection: %v", err)
	}
	defer dbConn.Release()

	grpcserver.RegisterServices(grpcServer, dbConn.Conn())

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
