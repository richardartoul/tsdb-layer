package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"

	pb "github.com/richardartoul/tsdb-layer/protos/.gen"
	"github.com/richardartoul/tsdb-layer/src/layer/server"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	useTLS   = flag.Bool("use_tls", false, "Connection uses TLS if true, else plain TCP")
	certFile = flag.String("cert_file", "", "The TLS cert file")
	keyFile  = flag.String("key_file", "", "The TLS key file")
	port     = flag.Int("port", 10000, "The server port")
)

func main() {
	flag.Parse()
	var (
		opts  []grpc.ServerOption
		dopts []grpc.DialOption
	)
	if *useTLS {
		if *certFile == "" {
			log.Fatalf("cert_file path is required")
		}
		if *keyFile == "" {
			log.Fatalf("key_file path is required")
		}
		creds, err := credentials.NewServerTLSFromFile(*certFile, *keyFile)
		if err != nil {
			log.Fatalf("Failed to generate credentials %v", err)
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
		dopts = []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	} else {
		dopts = []grpc.DialOption{grpc.WithInsecure()}
	}

	conn, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to initial TCP listen : %v\n", err)
	}

	go func() {
		// Start gRPC.
		grpcServer := grpc.NewServer(opts...)
		pb.RegisterTSDBLayerServer(grpcServer, server.NewServer())
		log.Printf("gRPC Listening on %s\n", conn.Addr().String())
		if err := grpcServer.Serve(conn); err != nil {
			log.Fatalf("error initializing gRPC: %v", err)
		}
	}()

	connString := fmt.Sprintf("localhost:%d", *port)
	mux := runtime.NewServeMux()
	err = pb.RegisterTSDBLayerHandlerFromEndpoint(context.Background(), mux, connString, dopts)
	if err != nil {
		log.Fatalf("Failed to register http handler from endpoint: %v\n", err)
	}

	port := *port + 1
	log.Printf("HTTP Listening on %d\n", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), mux))
}
