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
func (s *playerServer) ListPlayers(_ context.Context, req *playerv1.ListPlayersRequest) (*playerv1.ListPlayersResponse, error) {
all := []*playerv1.Player{
{Id: "123", Name: "Luis", Email: "luis@example.com", TeamId: "red", Score: 42, CreatedAt: "2025-01-01"},
{Id: "456", Name: "Ana", Email: "ana@example.com", TeamId: "blue", Score: 37, CreatedAt: "2025-02-14"},
{Id: "789", Name: "Rui", Email: "rui@example.com", TeamId: "red", Score: 18, CreatedAt: "2025-03-09"},
}
resp := &playerv1.ListPlayersResponse{}
for _, p := range all {
if req.TeamId != "" && p.TeamId != req.TeamId {
continue
}
if req.Limit > 0 && int32(len(resp.Players)) >= req.Limit {
break
}
resp.Players = append(resp.Players, p)
}
return resp, nil
}

type healthServer struct {
playerv1.UnimplementedHealthServiceServer
}

func (s *healthServer) Check(_ context.Context, _ *playerv1.HealthCheckRequest) (*playerv1.HealthCheckResponse, error) {
return &playerv1.HealthCheckResponse{Status: "SERVING"}, nil
}

func main() {
lis, err := net.Listen("tcp", ":50051")
if err != nil {
log.Fatalf("failed to listen: %v", err)
}

s := grpc.NewServer()
playerv1.RegisterPlayerServiceServer(s, &playerServer{})
playerv1.RegisterHealthServiceServer(s, &healthServer{})

// *** The only change from standard gRPC setup ***
// Descriptors are auto-registered via init() in generated _fieldops.pb.go files.
fieldops.Register(s)

log.Println("gRPC server with filtered reflection listening on :50051")
if err := s.Serve(lis); err != nil {
log.Fatalf("failed to serve: %v", err)
}
}
