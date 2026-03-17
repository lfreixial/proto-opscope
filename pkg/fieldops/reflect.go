// Package fieldops provides a gRPC reflection server that filters message fields
// based on operation annotations, enabling per-RPC field visibility.
package fieldops

import (
	"io"
	"sync"

	"google.golang.org/grpc"
	rpbv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	rpbv1alpha "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

type filteredServer struct {
	filteredDescBytes []byte
	once              sync.Once
	filteredFD        *descriptorpb.FileDescriptorProto
	filteredSymbols   map[string]bool
	filename          string
	initErr           error
}

func (s *filteredServer) init() {
	s.once.Do(func() {
		s.filteredFD = &descriptorpb.FileDescriptorProto{}
		if err := proto.Unmarshal(s.filteredDescBytes, s.filteredFD); err != nil {
			s.initErr = err
			return
		}
		s.filename = s.filteredFD.GetName()
		s.filteredSymbols = buildSymbolMap(s.filteredFD)
	})
}

func buildSymbolMap(fd *descriptorpb.FileDescriptorProto) map[string]bool {
	m := map[string]bool{}
	pkg := fd.GetPackage()
	for _, msg := range fd.GetMessageType() {
		m[pkg+"."+msg.GetName()] = true
	}
	for _, svc := range fd.GetService() {
		m[pkg+"."+svc.GetName()] = true
		for _, method := range svc.GetMethod() {
			m[pkg+"."+svc.GetName()+"."+method.GetName()] = true
		}
	}
	return m
}

// Register creates a filtered reflection server and registers both v1 and v1alpha
// reflection services on the given gRPC server.
func Register(s *grpc.Server, filteredDescBytes []byte) {
	srv := &filteredServer{filteredDescBytes: filteredDescBytes}
	v1adapter := &v1Adapter{srv}
	v1alphaadapter := &v1alphaAdapter{srv}
	rpbv1.RegisterServerReflectionServer(s, v1adapter)
	rpbv1alpha.RegisterServerReflectionServer(s, v1alphaadapter)
}

type v1Adapter struct{ srv *filteredServer }

func (a *v1Adapter) ServerReflectionInfo(stream rpbv1.ServerReflection_ServerReflectionInfoServer) error {
	return a.srv.handleV1Stream(stream)
}

type v1alphaAdapter struct{ srv *filteredServer }

func (a *v1alphaAdapter) ServerReflectionInfo(stream rpbv1alpha.ServerReflection_ServerReflectionInfoServer) error {
	return a.srv.handleV1alphaStream(stream)
}

func (s *filteredServer) handleV1Stream(stream rpbv1.ServerReflection_ServerReflectionInfoServer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		resp := s.handleRequest(req)
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}

func (s *filteredServer) handleV1alphaStream(stream rpbv1alpha.ServerReflection_ServerReflectionInfoServer) error {
	for {
		alphaReq, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		req := alphaToV1Request(alphaReq)
		resp := s.handleRequest(req)
		alphaResp := v1ToAlphaResponse(resp)
		if err := stream.Send(alphaResp); err != nil {
			return err
		}
	}
}

func alphaToV1Request(r *rpbv1alpha.ServerReflectionRequest) *rpbv1.ServerReflectionRequest {
	out := &rpbv1.ServerReflectionRequest{Host: r.Host}
	switch v := r.MessageRequest.(type) {
	case *rpbv1alpha.ServerReflectionRequest_FileByFilename:
		out.MessageRequest = &rpbv1.ServerReflectionRequest_FileByFilename{FileByFilename: v.FileByFilename}
	case *rpbv1alpha.ServerReflectionRequest_FileContainingSymbol:
		out.MessageRequest = &rpbv1.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: v.FileContainingSymbol}
	case *rpbv1alpha.ServerReflectionRequest_FileContainingExtension:
		out.MessageRequest = &rpbv1.ServerReflectionRequest_FileContainingExtension{
			FileContainingExtension: &rpbv1.ExtensionRequest{
				ContainingType:  v.FileContainingExtension.ContainingType,
				ExtensionNumber: v.FileContainingExtension.ExtensionNumber,
			},
		}
	case *rpbv1alpha.ServerReflectionRequest_AllExtensionNumbersOfType:
		out.MessageRequest = &rpbv1.ServerReflectionRequest_AllExtensionNumbersOfType{AllExtensionNumbersOfType: v.AllExtensionNumbersOfType}
	case *rpbv1alpha.ServerReflectionRequest_ListServices:
		out.MessageRequest = &rpbv1.ServerReflectionRequest_ListServices{ListServices: v.ListServices}
	}
	return out
}

func v1ToAlphaResponse(r *rpbv1.ServerReflectionResponse) *rpbv1alpha.ServerReflectionResponse {
	out := &rpbv1alpha.ServerReflectionResponse{
		ValidHost:       r.ValidHost,
		OriginalRequest: nil,
	}
	switch v := r.MessageResponse.(type) {
	case *rpbv1.ServerReflectionResponse_FileDescriptorResponse:
		out.MessageResponse = &rpbv1alpha.ServerReflectionResponse_FileDescriptorResponse{
			FileDescriptorResponse: &rpbv1alpha.FileDescriptorResponse{
				FileDescriptorProto: v.FileDescriptorResponse.FileDescriptorProto,
			},
		}
	case *rpbv1.ServerReflectionResponse_AllExtensionNumbersResponse:
		out.MessageResponse = &rpbv1alpha.ServerReflectionResponse_AllExtensionNumbersResponse{
			AllExtensionNumbersResponse: &rpbv1alpha.ExtensionNumberResponse{
				BaseTypeName:    v.AllExtensionNumbersResponse.BaseTypeName,
				ExtensionNumber: v.AllExtensionNumbersResponse.ExtensionNumber,
			},
		}
	case *rpbv1.ServerReflectionResponse_ListServicesResponse:
		svcs := make([]*rpbv1alpha.ServiceResponse, 0, len(v.ListServicesResponse.Service))
		for _, svc := range v.ListServicesResponse.Service {
			svcs = append(svcs, &rpbv1alpha.ServiceResponse{Name: svc.Name})
		}
		out.MessageResponse = &rpbv1alpha.ServerReflectionResponse_ListServicesResponse{
			ListServicesResponse: &rpbv1alpha.ListServiceResponse{Service: svcs},
		}
	case *rpbv1.ServerReflectionResponse_ErrorResponse:
		out.MessageResponse = &rpbv1alpha.ServerReflectionResponse_ErrorResponse{
			ErrorResponse: &rpbv1alpha.ErrorResponse{
				ErrorCode:    v.ErrorResponse.ErrorCode,
				ErrorMessage: v.ErrorResponse.ErrorMessage,
			},
		}
	}
	return out
}

func (s *filteredServer) handleRequest(req *rpbv1.ServerReflectionRequest) *rpbv1.ServerReflectionResponse {
	s.init()
	if s.initErr != nil {
		return errorResponse(s.initErr.Error())
	}

	switch v := req.MessageRequest.(type) {
	case *rpbv1.ServerReflectionRequest_ListServices:
		svcs := make([]*rpbv1.ServiceResponse, 0, len(s.filteredFD.GetService()))
		pkg := s.filteredFD.GetPackage()
		for _, svc := range s.filteredFD.GetService() {
			svcs = append(svcs, &rpbv1.ServiceResponse{Name: pkg + "." + svc.GetName()})
		}
		return &rpbv1.ServerReflectionResponse{
			MessageResponse: &rpbv1.ServerReflectionResponse_ListServicesResponse{
				ListServicesResponse: &rpbv1.ListServiceResponse{Service: svcs},
			},
		}

	case *rpbv1.ServerReflectionRequest_FileByFilename:
		name := v.FileByFilename
		if name == s.filename {
			b, err := marshalFD(s.filteredFD)
			if err != nil {
				return errorResponse(err.Error())
			}
			return fileDescriptorResponse(b)
		}
		fd, err := protoregistry.GlobalFiles.FindFileByPath(name)
		if err != nil {
			return errorResponse(err.Error())
		}
		resp, err := globalFileLookup(fd)
		if err != nil {
			return errorResponse(err.Error())
		}
		return resp

	case *rpbv1.ServerReflectionRequest_FileContainingSymbol:
		symbol := v.FileContainingSymbol
		if s.filteredSymbols[symbol] {
			b, err := marshalFD(s.filteredFD)
			if err != nil {
				return errorResponse(err.Error())
			}
			return fileDescriptorResponse(b)
		}
		d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(symbol))
		if err != nil {
			return errorResponse(err.Error())
		}
		resp, err := globalFileLookup(d.ParentFile())
		if err != nil {
			return errorResponse(err.Error())
		}
		return resp

	case *rpbv1.ServerReflectionRequest_FileContainingExtension:
		typeName := v.FileContainingExtension.ContainingType
		extNum := v.FileContainingExtension.ExtensionNumber
		ext, err := protoregistry.GlobalTypes.FindExtensionByNumber(protoreflect.FullName(typeName), protoreflect.FieldNumber(extNum))
		if err != nil {
			return errorResponse(err.Error())
		}
		resp, err := globalFileLookup(ext.TypeDescriptor().ParentFile())
		if err != nil {
			return errorResponse(err.Error())
		}
		return resp

	case *rpbv1.ServerReflectionRequest_AllExtensionNumbersOfType:
		typeName := v.AllExtensionNumbersOfType
		var nums []int32
		protoregistry.GlobalTypes.RangeExtensionsByMessage(protoreflect.FullName(typeName), func(ext protoreflect.ExtensionType) bool {
			nums = append(nums, int32(ext.TypeDescriptor().Number()))
			return true
		})
		return &rpbv1.ServerReflectionResponse{
			MessageResponse: &rpbv1.ServerReflectionResponse_AllExtensionNumbersResponse{
				AllExtensionNumbersResponse: &rpbv1.ExtensionNumberResponse{
					BaseTypeName:    typeName,
					ExtensionNumber: nums,
				},
			},
		}

	default:
		return errorResponse("unsupported request type")
	}
}

func marshalFD(fd *descriptorpb.FileDescriptorProto) ([]byte, error) {
	return proto.Marshal(fd)
}

func fileDescriptorResponse(b []byte) *rpbv1.ServerReflectionResponse {
	return &rpbv1.ServerReflectionResponse{
		MessageResponse: &rpbv1.ServerReflectionResponse_FileDescriptorResponse{
			FileDescriptorResponse: &rpbv1.FileDescriptorResponse{
				FileDescriptorProto: [][]byte{b},
			},
		},
	}
}

func globalFileLookup(fd protoreflect.FileDescriptor) (*rpbv1.ServerReflectionResponse, error) {
	p := protodesc.ToFileDescriptorProto(fd)
	b, err := marshalFD(p)
	if err != nil {
		return nil, err
	}
	return fileDescriptorResponse(b), nil
}

func errorResponse(msg string) *rpbv1.ServerReflectionResponse {
	return &rpbv1.ServerReflectionResponse{
		MessageResponse: &rpbv1.ServerReflectionResponse_ErrorResponse{
			ErrorResponse: &rpbv1.ErrorResponse{
				ErrorCode:    int32(2), // UNKNOWN
				ErrorMessage: msg,
			},
		},
	}
}
