package main

import (
"context"
"log"
"net"

"google.golang.org/grpc"
fieldops "github.com/lfreixial/proto-opscope/pkg/fieldops"
playerv1 "github.com/lfreixial/proto-opscope/gen/example/player/v1"
)

type playerServer struct {
playerv1.UnimplementedPlayerServiceServer
}

func (s *playerServer) CreatePlayer(_ context.Context, req *playerv1.Player) (*playerv1.Player, error) {
return &playerv1.Player{Id: "123", Name: req.Name, Email: req.Email, TeamId: req.TeamId}, nil
}
func (s *playerServer) UpdatePlayer(_ context.Context, req *playerv1.Player) (*playerv1.Player, error) {
return &playerv1.Player{Id: "123", Name: req.Name, Score: req.Score}, nil
}
func (s *playerServer) GetPlayer(_ context.Context, _ *playerv1.GetPlayerRequest) (*playerv1.Player, error) {
return &playerv1.Player{Id: "123", Name: "Luis", Email: "luis@example.com", Score: 42, CreatedAt: "2025-01-01"}, nil
}

func main() {
lis, err := net.Listen("tcp", ":50051")
if err != nil {
log.Fatalf("failed to listen: %v", err)
}

s := grpc.NewServer()
playerv1.RegisterPlayerServiceServer(s, &playerServer{})

// *** The only change from standard gRPC setup ***
// Descriptors are auto-registered via init() in generated _fieldops.pb.go files.
fieldops.Register(s)

log.Println("gRPC server with filtered reflection listening on :50051")
if err := s.Serve(lis); err != nil {
log.Fatalf("failed to serve: %v", err)
}
}
