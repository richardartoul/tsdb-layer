package server

import (
	"context"
	"fmt"

	pb "github.com/richardartoul/tsdb-layer/protos/.gen"
)

var _ pb.TSDBLayerServer = &server{}

type server struct {
}

func NewServer() pb.TSDBLayerServer {
	return &server{}
}

func (s *server) WriteBatch(context.Context, *pb.WriteBatchRequest) (*pb.Empty, error) {
	fmt.Println("hmm1")
	return nil, nil
}

func (s *server) ReadBatch(context.Context, *pb.ReadBatchRequest) (*pb.ReadBatchResponse, error) {
	fmt.Println("hmm2")
	return nil, nil
}
