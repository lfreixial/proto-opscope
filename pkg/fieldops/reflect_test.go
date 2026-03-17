package fieldops

import (
	"testing"

	rpbv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	rpbv1alpha "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

// --- helpers ---

func marshalDescriptor(t *testing.T, fd *descriptorpb.FileDescriptorProto) []byte {
	t.Helper()
	b, err := proto.Marshal(fd)
	if err != nil {
		t.Fatalf("marshal descriptor: %v", err)
	}
	return b
}

func testDescriptor(t *testing.T) []byte {
	t.Helper()
	return marshalDescriptor(t, &descriptorpb.FileDescriptorProto{
		Name:    proto.String("test/v1/test.proto"),
		Package: proto.String("test.v1"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Item"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("id"), Number: proto.Int32(1), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
					{Name: proto.String("name"), Number: proto.Int32(2), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
				},
			},
			{
				Name: proto.String("Item_CREATE"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("name"), Number: proto.Int32(2), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("ItemService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("CreateItem"),
						InputType:  proto.String(".test.v1.Item_CREATE"),
						OutputType: proto.String(".test.v1.Item"),
					},
				},
			},
		},
	})
}

// --- buildSymbolMap ---

func TestBuildSymbolMap(t *testing.T) {
	fd := &descriptorpb.FileDescriptorProto{
		Package: proto.String("test.v1"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Foo")},
			{Name: proto.String("Bar")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("FooService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{Name: proto.String("GetFoo")},
					{Name: proto.String("CreateFoo")},
				},
			},
		},
	}

	m := buildSymbolMap(fd)

	expected := []string{
		"test.v1.Foo",
		"test.v1.Bar",
		"test.v1.FooService",
		"test.v1.FooService.GetFoo",
		"test.v1.FooService.CreateFoo",
	}
	for _, sym := range expected {
		if !m[sym] {
			t.Errorf("symbol %q not found in map", sym)
		}
	}
	if len(m) != len(expected) {
		t.Errorf("symbol map has %d entries, want %d", len(m), len(expected))
	}
}

// --- AddDescriptor ---

func TestAddDescriptor(t *testing.T) {
	// Save and restore global state.
	registryMu.Lock()
	saved := registry
	registry = nil
	registryMu.Unlock()
	defer func() {
		registryMu.Lock()
		registry = saved
		registryMu.Unlock()
	}()

	AddDescriptor([]byte{0x01})
	AddDescriptor([]byte{0x02})

	registryMu.Lock()
	defer registryMu.Unlock()
	if len(registry) != 2 {
		t.Fatalf("registry has %d entries, want 2", len(registry))
	}
}

// --- handleRequest ---

func TestHandleRequest_ListServices(t *testing.T) {
	srv := &filteredServer{rawDescs: [][]byte{testDescriptor(t)}}

	resp := srv.handleRequest(&rpbv1.ServerReflectionRequest{
		MessageRequest: &rpbv1.ServerReflectionRequest_ListServices{ListServices: ""},
	})

	listResp, ok := resp.MessageResponse.(*rpbv1.ServerReflectionResponse_ListServicesResponse)
	if !ok {
		t.Fatal("expected ListServicesResponse")
	}
	svcs := listResp.ListServicesResponse.Service
	if len(svcs) != 1 {
		t.Fatalf("got %d services, want 1", len(svcs))
	}
	if svcs[0].Name != "test.v1.ItemService" {
		t.Errorf("service = %q, want %q", svcs[0].Name, "test.v1.ItemService")
	}
}

func TestHandleRequest_FileByFilename(t *testing.T) {
	srv := &filteredServer{rawDescs: [][]byte{testDescriptor(t)}}

	resp := srv.handleRequest(&rpbv1.ServerReflectionRequest{
		MessageRequest: &rpbv1.ServerReflectionRequest_FileByFilename{FileByFilename: "test/v1/test.proto"},
	})

	fdResp, ok := resp.MessageResponse.(*rpbv1.ServerReflectionResponse_FileDescriptorResponse)
	if !ok {
		t.Fatal("expected FileDescriptorResponse")
	}
	if len(fdResp.FileDescriptorResponse.FileDescriptorProto) != 1 {
		t.Fatal("expected 1 file descriptor")
	}

	fd := &descriptorpb.FileDescriptorProto{}
	if err := proto.Unmarshal(fdResp.FileDescriptorResponse.FileDescriptorProto[0], fd); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fd.GetName() != "test/v1/test.proto" {
		t.Errorf("filename = %q, want %q", fd.GetName(), "test/v1/test.proto")
	}
}

func TestHandleRequest_FileByFilename_NotFound(t *testing.T) {
	srv := &filteredServer{rawDescs: [][]byte{testDescriptor(t)}}

	resp := srv.handleRequest(&rpbv1.ServerReflectionRequest{
		MessageRequest: &rpbv1.ServerReflectionRequest_FileByFilename{FileByFilename: "nonexistent.proto"},
	})

	if _, ok := resp.MessageResponse.(*rpbv1.ServerReflectionResponse_ErrorResponse); !ok {
		t.Error("expected ErrorResponse for unknown file")
	}
}

func TestHandleRequest_FileContainingSymbol(t *testing.T) {
	srv := &filteredServer{rawDescs: [][]byte{testDescriptor(t)}}

	symbols := []string{
		"test.v1.ItemService",
		"test.v1.Item",
		"test.v1.Item_CREATE",
		"test.v1.ItemService.CreateItem",
	}
	for _, sym := range symbols {
		resp := srv.handleRequest(&rpbv1.ServerReflectionRequest{
			MessageRequest: &rpbv1.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: sym},
		})
		if _, ok := resp.MessageResponse.(*rpbv1.ServerReflectionResponse_FileDescriptorResponse); !ok {
			t.Errorf("symbol %q: expected FileDescriptorResponse", sym)
		}
	}
}

func TestHandleRequest_UnsupportedType(t *testing.T) {
	srv := &filteredServer{rawDescs: [][]byte{testDescriptor(t)}}

	resp := srv.handleRequest(&rpbv1.ServerReflectionRequest{})

	if _, ok := resp.MessageResponse.(*rpbv1.ServerReflectionResponse_ErrorResponse); !ok {
		t.Error("expected ErrorResponse for unsupported request type")
	}
}

func TestHandleRequest_InvalidDescriptor(t *testing.T) {
	srv := &filteredServer{rawDescs: [][]byte{{0xff, 0xff, 0xff}}}

	resp := srv.handleRequest(&rpbv1.ServerReflectionRequest{
		MessageRequest: &rpbv1.ServerReflectionRequest_ListServices{ListServices: ""},
	})

	if _, ok := resp.MessageResponse.(*rpbv1.ServerReflectionResponse_ErrorResponse); !ok {
		t.Error("expected ErrorResponse for invalid descriptor")
	}
}

// --- multiple descriptors ---

func TestHandleRequest_MultipleDescriptors(t *testing.T) {
	desc1 := marshalDescriptor(t, &descriptorpb.FileDescriptorProto{
		Name:    proto.String("svc1.proto"),
		Package: proto.String("pkg1"),
		Service: []*descriptorpb.ServiceDescriptorProto{
			{Name: proto.String("Service1")},
		},
	})
	desc2 := marshalDescriptor(t, &descriptorpb.FileDescriptorProto{
		Name:    proto.String("svc2.proto"),
		Package: proto.String("pkg2"),
		Service: []*descriptorpb.ServiceDescriptorProto{
			{Name: proto.String("Service2")},
		},
	})

	srv := &filteredServer{rawDescs: [][]byte{desc1, desc2}}

	// ListServices returns both.
	resp := srv.handleRequest(&rpbv1.ServerReflectionRequest{
		MessageRequest: &rpbv1.ServerReflectionRequest_ListServices{ListServices: ""},
	})
	listResp := resp.MessageResponse.(*rpbv1.ServerReflectionResponse_ListServicesResponse)
	if len(listResp.ListServicesResponse.Service) != 2 {
		t.Fatalf("got %d services, want 2", len(listResp.ListServicesResponse.Service))
	}

	// FileByFilename resolves each file.
	for _, name := range []string{"svc1.proto", "svc2.proto"} {
		resp := srv.handleRequest(&rpbv1.ServerReflectionRequest{
			MessageRequest: &rpbv1.ServerReflectionRequest_FileByFilename{FileByFilename: name},
		})
		if _, ok := resp.MessageResponse.(*rpbv1.ServerReflectionResponse_FileDescriptorResponse); !ok {
			t.Errorf("expected FileDescriptorResponse for %s", name)
		}
	}

	// FileContainingSymbol routes to the correct file.
	resp = srv.handleRequest(&rpbv1.ServerReflectionRequest{
		MessageRequest: &rpbv1.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: "pkg2.Service2"},
	})
	fdResp := resp.MessageResponse.(*rpbv1.ServerReflectionResponse_FileDescriptorResponse)
	fd := &descriptorpb.FileDescriptorProto{}
	proto.Unmarshal(fdResp.FileDescriptorResponse.FileDescriptorProto[0], fd)
	if fd.GetName() != "svc2.proto" {
		t.Errorf("expected svc2.proto, got %s", fd.GetName())
	}
}

// --- v1alpha conversion ---

func TestAlphaToV1Request(t *testing.T) {
	tests := []struct {
		name  string
		alpha *rpbv1alpha.ServerReflectionRequest
	}{
		{
			"list services",
			&rpbv1alpha.ServerReflectionRequest{
				MessageRequest: &rpbv1alpha.ServerReflectionRequest_ListServices{ListServices: ""},
			},
		},
		{
			"file by filename",
			&rpbv1alpha.ServerReflectionRequest{
				MessageRequest: &rpbv1alpha.ServerReflectionRequest_FileByFilename{FileByFilename: "test.proto"},
			},
		},
		{
			"file containing symbol",
			&rpbv1alpha.ServerReflectionRequest{
				MessageRequest: &rpbv1alpha.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: "pkg.Svc"},
			},
		},
		{
			"file containing extension",
			&rpbv1alpha.ServerReflectionRequest{
				MessageRequest: &rpbv1alpha.ServerReflectionRequest_FileContainingExtension{
					FileContainingExtension: &rpbv1alpha.ExtensionRequest{ContainingType: "pkg.Msg", ExtensionNumber: 100},
				},
			},
		},
		{
			"all extension numbers",
			&rpbv1alpha.ServerReflectionRequest{
				MessageRequest: &rpbv1alpha.ServerReflectionRequest_AllExtensionNumbersOfType{AllExtensionNumbersOfType: "pkg.Msg"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v1req := alphaToV1Request(tt.alpha)
			if v1req == nil {
				t.Fatal("conversion returned nil")
			}
			if v1req.MessageRequest == nil {
				t.Fatal("converted request has nil MessageRequest")
			}
		})
	}
}

func TestV1ToAlphaResponse(t *testing.T) {
	tests := []struct {
		name string
		v1   *rpbv1.ServerReflectionResponse
	}{
		{
			"file descriptor",
			fileDescriptorResponse([]byte{0x01}),
		},
		{
			"list services",
			&rpbv1.ServerReflectionResponse{
				MessageResponse: &rpbv1.ServerReflectionResponse_ListServicesResponse{
					ListServicesResponse: &rpbv1.ListServiceResponse{
						Service: []*rpbv1.ServiceResponse{{Name: "test"}},
					},
				},
			},
		},
		{
			"error",
			errorResponse("boom"),
		},
		{
			"all extension numbers",
			&rpbv1.ServerReflectionResponse{
				MessageResponse: &rpbv1.ServerReflectionResponse_AllExtensionNumbersResponse{
					AllExtensionNumbersResponse: &rpbv1.ExtensionNumberResponse{
						BaseTypeName:    "pkg.Msg",
						ExtensionNumber: []int32{100, 200},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alpha := v1ToAlphaResponse(tt.v1)
			if alpha == nil {
				t.Fatal("conversion returned nil")
			}
			if alpha.MessageResponse == nil {
				t.Fatal("converted response has nil MessageResponse")
			}
		})
	}
}
