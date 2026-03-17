package generator

import (
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

// --- helpers ---

func makeFieldOptions(ops ...Operation) *descriptorpb.FieldOptions {
	var b []byte
	for _, op := range ops {
		b = protowire.AppendTag(b, fieldOpExtNum, protowire.VarintType)
		b = protowire.AppendVarint(b, uint64(op))
	}
	opts := &descriptorpb.FieldOptions{}
	if err := proto.Unmarshal(b, opts); err != nil {
		panic(err)
	}
	return opts
}

func makeMethodOptions(op Operation) *descriptorpb.MethodOptions {
	var b []byte
	b = protowire.AppendTag(b, rpcOpExtNum, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(op))
	opts := &descriptorpb.MethodOptions{}
	if err := proto.Unmarshal(b, opts); err != nil {
		panic(err)
	}
	return opts
}

// --- Operation.String ---

func TestOperationString(t *testing.T) {
	tests := []struct {
		op   Operation
		want string
	}{
		{OperationUnspecified, "UNSPECIFIED"},
		{OperationCreate, "CREATE"},
		{OperationRead, "READ"},
		{OperationUpdate, "UPDATE"},
		{OperationDelete, "DELETE"},
	}
	for _, tt := range tests {
		if got := tt.op.String(); got != tt.want {
			t.Errorf("Operation(%d).String() = %q, want %q", tt.op, got, tt.want)
		}
	}
}

// --- readRpcOp ---

func TestReadRpcOp(t *testing.T) {
	tests := []struct {
		name   string
		opts   proto.Message
		wantOp Operation
		wantOk bool
	}{
		{"nil options", nil, 0, false},
		{"empty options", &descriptorpb.MethodOptions{}, 0, false},
		{"create", makeMethodOptions(OperationCreate), OperationCreate, true},
		{"read", makeMethodOptions(OperationRead), OperationRead, true},
		{"update", makeMethodOptions(OperationUpdate), OperationUpdate, true},
		{"delete", makeMethodOptions(OperationDelete), OperationDelete, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, ok := readRpcOp(tt.opts)
			if op != tt.wantOp || ok != tt.wantOk {
				t.Errorf("readRpcOp() = (%v, %v), want (%v, %v)", op, ok, tt.wantOp, tt.wantOk)
			}
		})
	}
}

// --- readFieldOps ---

func TestReadFieldOps(t *testing.T) {
	tests := []struct {
		name string
		opts proto.Message
		want []Operation
	}{
		{"nil options", nil, nil},
		{"empty options", &descriptorpb.FieldOptions{}, nil},
		{"single op", makeFieldOptions(OperationCreate), []Operation{OperationCreate}},
		{
			"multiple ops",
			makeFieldOptions(OperationCreate, OperationRead, OperationUpdate),
			[]Operation{OperationCreate, OperationRead, OperationUpdate},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readFieldOps(tt.opts)
			if len(got) != len(tt.want) {
				t.Fatalf("readFieldOps() returned %d ops, want %d", len(got), len(tt.want))
			}
			for i, op := range got {
				if op != tt.want[i] {
					t.Errorf("readFieldOps()[%d] = %v, want %v", i, op, tt.want[i])
				}
			}
		})
	}
}

// --- formatBytesLiteral ---

func TestFormatBytesLiteral(t *testing.T) {
	if got := formatBytesLiteral(nil); got != "" {
		t.Errorf("formatBytesLiteral(nil) = %q, want empty", got)
	}
	if got := formatBytesLiteral([]byte{0xab, 0xcd}); got == "" {
		t.Error("formatBytesLiteral(non-empty) should not be empty")
	}
}

// --- buildFilteredDescriptor ---

func TestBuildFilteredDescriptor(t *testing.T) {
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("test.proto"),
		Package: proto.String("test.v1"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Player"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("id"), Number: proto.Int32(1), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
					{Name: proto.String("name"), Number: proto.Int32(2), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(), Options: makeFieldOptions(OperationCreate, OperationRead)},
					{Name: proto.String("email"), Number: proto.Int32(3), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(), Options: makeFieldOptions(OperationCreate)},
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("PlayerService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("CreatePlayer"),
						InputType:  proto.String(".test.v1.Player"),
						OutputType: proto.String(".test.v1.Player"),
					},
				},
			},
		},
		SourceCodeInfo: &descriptorpb.SourceCodeInfo{
			Location: []*descriptorpb.SourceCodeInfo_Location{{Path: []int32{1}}},
		},
	}

	file := &protogen.File{Proto: fdp}
	rules := []methodRule{
		{
			ServiceFQN:    "test.v1.PlayerService",
			MethodName:    "CreatePlayer",
			Operation:     OperationCreate,
			InputFQN:      "test.v1.Player",
			AllowedFields: []fieldInfo{{Number: 2, Name: "name"}, {Number: 3, Name: "email"}},
			SyntheticName: "Player_CREATE",
		},
	}

	result, err := buildFilteredDescriptor(file, rules)
	if err != nil {
		t.Fatalf("buildFilteredDescriptor() error: %v", err)
	}

	// Source code info stripped.
	if result.SourceCodeInfo != nil {
		t.Error("expected SourceCodeInfo to be nil")
	}

	// Synthetic message exists with correct fields.
	var synthetic *descriptorpb.DescriptorProto
	for _, msg := range result.GetMessageType() {
		if msg.GetName() == "Player_CREATE" {
			synthetic = msg
			break
		}
	}
	if synthetic == nil {
		t.Fatal("synthetic message Player_CREATE not found")
	}
	if len(synthetic.GetField()) != 2 {
		t.Fatalf("Player_CREATE has %d fields, want 2", len(synthetic.GetField()))
	}
	if synthetic.GetField()[0].GetName() != "name" || synthetic.GetField()[1].GetName() != "email" {
		t.Errorf("fields = [%s, %s], want [name, email]",
			synthetic.GetField()[0].GetName(), synthetic.GetField()[1].GetName())
	}

	// Synthetic fields have options stripped.
	for _, f := range synthetic.GetField() {
		if f.Options != nil {
			t.Errorf("synthetic field %q still has options", f.GetName())
		}
	}

	// Method InputType rewritten to synthetic.
	method := result.GetService()[0].GetMethod()[0]
	if method.GetInputType() != ".test.v1.Player_CREATE" {
		t.Errorf("InputType = %q, want %q", method.GetInputType(), ".test.v1.Player_CREATE")
	}

	// Original message preserved.
	found := false
	for _, msg := range result.GetMessageType() {
		if msg.GetName() == "Player" {
			found = true
			break
		}
	}
	if !found {
		t.Error("original Player message should be preserved")
	}
}

func TestBuildFilteredDescriptor_OriginalUnmodified(t *testing.T) {
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("test.proto"),
		Package: proto.String("test.v1"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Msg"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("a"), Number: proto.Int32(1), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("Svc"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{Name: proto.String("Do"), InputType: proto.String(".test.v1.Msg"), OutputType: proto.String(".test.v1.Msg")},
				},
			},
		},
	}

	file := &protogen.File{Proto: fdp}
	rules := []methodRule{
		{
			ServiceFQN:    "test.v1.Svc",
			MethodName:    "Do",
			Operation:     OperationCreate,
			InputFQN:      "test.v1.Msg",
			AllowedFields: []fieldInfo{{Number: 1, Name: "a"}},
			SyntheticName: "Msg_CREATE",
		},
	}

	_, err := buildFilteredDescriptor(file, rules)
	if err != nil {
		t.Fatal(err)
	}

	// Original proto must not be mutated.
	if len(fdp.GetMessageType()) != 1 {
		t.Errorf("original proto was mutated: has %d messages, want 1", len(fdp.GetMessageType()))
	}
	if fdp.GetService()[0].GetMethod()[0].GetInputType() != ".test.v1.Msg" {
		t.Error("original method InputType was mutated")
	}
}
