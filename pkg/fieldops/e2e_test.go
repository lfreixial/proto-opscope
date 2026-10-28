package fieldops_test

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	rpbv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"

	playerv1 "github.com/lfreixial/proto-opscope/gen/example/player/v1"
	fieldops "github.com/lfreixial/proto-opscope/pkg/fieldops"
)

// TestE2E_ReflectionResolvesAllServices walks the reflection flow a client like
// grpcurl or Postman performs (list services → file containing symbol → resolve
// dependencies) and checks that both annotated and non-annotated services resolve.
func TestE2E_ReflectionResolvesAllServices(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	s := grpc.NewServer()
	playerv1.RegisterPlayerServiceServer(s, playerv1.UnimplementedPlayerServiceServer{})
	playerv1.RegisterHealthServiceServer(s, playerv1.UnimplementedHealthServiceServer{})
	fieldops.Register(s)
	go s.Serve(lis)
	defer s.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	stream, err := rpbv1.NewServerReflectionClient(conn).ServerReflectionInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	send := func(req *rpbv1.ServerReflectionRequest) *rpbv1.ServerReflectionResponse {
		t.Helper()
		if err := stream.Send(req); err != nil {
			t.Fatal(err)
		}
		resp, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if e := resp.GetErrorResponse(); e != nil {
			t.Fatalf("reflection error for %v: %s", req.MessageRequest, e.GetErrorMessage())
		}
		return resp
	}

	// Collect file descriptors, fetching dependencies by filename as a client would.
	fdps := map[string]*descriptorpb.FileDescriptorProto{}
	var addFiles func(resp *rpbv1.ServerReflectionResponse)
	addFiles = func(resp *rpbv1.ServerReflectionResponse) {
		for _, b := range resp.GetFileDescriptorResponse().GetFileDescriptorProto() {
			fd := &descriptorpb.FileDescriptorProto{}
			if err := proto.Unmarshal(b, fd); err != nil {
				t.Fatal(err)
			}
			if _, seen := fdps[fd.GetName()]; seen {
				continue
			}
			fdps[fd.GetName()] = fd
			for _, dep := range fd.GetDependency() {
				if _, seen := fdps[dep]; !seen {
					addFiles(send(&rpbv1.ServerReflectionRequest{
						MessageRequest: &rpbv1.ServerReflectionRequest_FileByFilename{FileByFilename: dep},
					}))
				}
			}
		}
	}

	listed := map[string]bool{}
	for _, svc := range send(&rpbv1.ServerReflectionRequest{
		MessageRequest: &rpbv1.ServerReflectionRequest_ListServices{},
	}).GetListServicesResponse().GetService() {
		listed[svc.GetName()] = true
		addFiles(send(&rpbv1.ServerReflectionRequest{
			MessageRequest: &rpbv1.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: svc.GetName()},
		}))
	}

	for _, want := range []string{"example.player.v1.PlayerService", "example.player.v1.HealthService"} {
		if !listed[want] {
			t.Errorf("ListServices missing %s; got %v", want, listed)
		}
	}

	set := &descriptorpb.FileDescriptorSet{}
	for _, fd := range fdps {
		set.File = append(set.File, fd)
	}
	files, err := protodesc.NewFiles(set)
	if err != nil {
		t.Fatalf("descriptors returned by reflection do not link: %v", err)
	}

	inputOf := func(svc, method string) string {
		t.Helper()
		d, err := files.FindDescriptorByName(protoreflect.FullName(svc))
		if err != nil {
			t.Fatalf("resolve %s: %v", svc, err)
		}
		m := d.(protoreflect.ServiceDescriptor).Methods().ByName(protoreflect.Name(method))
		if m == nil {
			t.Fatalf("%s has no method %s", svc, method)
		}
		return string(m.Input().FullName())
	}

	tests := []struct{ svc, method, wantInput string }{
		{"example.player.v1.PlayerService", "CreatePlayer", "example.player.v1.Player_CREATE"},
		{"example.player.v1.PlayerService", "UpdatePlayer", "example.player.v1.Player_UPDATE"},
		{"example.player.v1.PlayerService", "GetPlayer", "example.player.v1.GetPlayerRequest"},
		{"example.player.v1.PlayerService", "ListPlayers", "example.player.v1.ListPlayersRequest"},
		{"example.player.v1.HealthService", "Check", "example.player.v1.HealthCheckRequest"},
	}
	for _, tt := range tests {
		if got := inputOf(tt.svc, tt.method); got != tt.wantInput {
			t.Errorf("%s.%s input = %s, want %s", tt.svc, tt.method, got, tt.wantInput)
		}
	}
}
