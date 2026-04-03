package generator

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

const (
	fieldOpExtNum = 51234 // field_op extension field number on FieldOptions
	rpcOpExtNum   = 51235 // rpc_op extension field number on MethodOptions
)

// Operation represents the type of operation an RPC endpoint performs.
type Operation int32

const (
	OperationUnspecified Operation = 0
	OperationCreate      Operation = 1
	OperationRead        Operation = 2
	OperationUpdate      Operation = 3
	OperationDelete      Operation = 4
)

func (o Operation) String() string {
	switch o {
	case OperationCreate:
		return "CREATE"
	case OperationRead:
		return "READ"
	case OperationUpdate:
		return "UPDATE"
	case OperationDelete:
		return "DELETE"
	default:
		return "UNSPECIFIED"
	}
}

type fieldInfo struct {
	Number int32
	Name   string
}

type methodRule struct {
	ServiceFQN    string
	MethodName    string
	Operation     Operation
	InputFQN      string
	AllowedFields []fieldInfo
	SyntheticName string
}

// Generate is the entry point for the protoc plugin.
func Generate(gen *protogen.Plugin) error {
	allMessages := buildAllMessages(gen)
	allTypeFiles := buildAllTypeFiles(gen)
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		if err := generateFile(gen, f, allMessages, allTypeFiles); err != nil {
			return err
		}
	}
	return nil
}

// buildAllMessages indexes every message across all files by fully-qualified name
// (e.g. "dpprotos.services.entities.users.v1.UserInformation") so that
// buildFilteredDescriptor can resolve types defined in imported files.
func buildAllMessages(gen *protogen.Plugin) map[string]*descriptorpb.DescriptorProto {
	m := make(map[string]*descriptorpb.DescriptorProto)
	for _, f := range gen.Files {
		pkg := f.Proto.GetPackage()
		for _, msg := range f.Proto.GetMessageType() {
			fqn := pkg + "." + msg.GetName()
			m[fqn] = msg
		}
	}
	return m
}

// buildAllTypeFiles maps every message and enum FQN to the proto file that defines it.
// This is used to add missing direct imports when synthetic messages reference types
// from files not already in the descriptor's dependency list.
func buildAllTypeFiles(gen *protogen.Plugin) map[string]string {
	m := make(map[string]string)
	for _, f := range gen.Files {
		pkg := f.Proto.GetPackage()
		name := f.Proto.GetName()
		for _, msg := range f.Proto.GetMessageType() {
			m[pkg+"."+msg.GetName()] = name
		}
		for _, enum := range f.Proto.GetEnumType() {
			m[pkg+"."+enum.GetName()] = name
		}
	}
	return m
}

func generateFile(gen *protogen.Plugin, file *protogen.File, allMessages map[string]*descriptorpb.DescriptorProto, allTypeFiles map[string]string) error {
	var rules []methodRule

	for _, svc := range file.Services {
		for _, method := range svc.Methods {
			op, ok := readRpcOp(method.Desc.Options())
			if !ok || op == OperationUnspecified {
				continue
			}
			allowedFields := getFieldsForOp(method.Input, op)
			if len(allowedFields) == 0 {
				continue // no field_op annotations → keep original input type
			}
			inputShortName := string(method.Input.Desc.Name())
			rule := methodRule{
				ServiceFQN:    string(svc.Desc.FullName()),
				MethodName:    string(method.Desc.Name()),
				Operation:     op,
				InputFQN:      string(method.Input.Desc.FullName()),
				AllowedFields: allowedFields,
				SyntheticName: inputShortName + "_" + op.String(),
			}
			rules = append(rules, rule)
		}
	}

	if len(rules) == 0 {
		return nil
	}

	filteredFD, err := buildFilteredDescriptor(file, rules, allMessages, allTypeFiles)
	if err != nil {
		return fmt.Errorf("buildFilteredDescriptor: %w", err)
	}

	descBytes, err := proto.Marshal(filteredFD)
	if err != nil {
		return fmt.Errorf("marshal filtered descriptor: %w", err)
	}

	g := gen.NewGeneratedFile(file.GeneratedFilenamePrefix+"_fieldops.pb.go", file.GoImportPath)

	data := templateData{
		PackageName:      string(file.GoPackageName),
		DescBytesLiteral: formatBytesLiteral(descBytes),
	}

	if err := generatedFileTemplate.Execute(g, data); err != nil {
		return fmt.Errorf("template execute: %w", err)
	}

	return nil
}

type templateData struct {
	PackageName      string
	DescBytesLiteral string
}

func formatBytesLiteral(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, byt := range b {
		if i%16 == 0 {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString("\t")
		}
		sb.WriteString(fmt.Sprintf("0x%02x, ", byt))
	}
	return sb.String()
}

func readRpcOp(opts proto.Message) (Operation, bool) {
	if opts == nil || proto.Size(opts) == 0 {
		return 0, false
	}
	b, err := proto.Marshal(opts)
	if err != nil {
		return 0, false
	}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			break
		}
		b = b[n:]
		if num == rpcOpExtNum && typ == protowire.VarintType {
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				break
			}
			return Operation(v), true
		}
		n = skipField(b, num, typ)
		if n < 0 {
			break
		}
		b = b[n:]
	}
	return 0, false
}

func readFieldOps(opts proto.Message) []Operation {
	if opts == nil || proto.Size(opts) == 0 {
		return nil
	}
	b, err := proto.Marshal(opts)
	if err != nil {
		return nil
	}
	var ops []Operation
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			break
		}
		b = b[n:]
		if num == fieldOpExtNum && typ == protowire.VarintType {
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				break
			}
			ops = append(ops, Operation(v))
			b = b[n:]
			continue
		}
		n = skipField(b, num, typ)
		if n < 0 {
			break
		}
		b = b[n:]
	}
	return ops
}

func getFieldsForOp(msg *protogen.Message, op Operation) []fieldInfo {
	var fields []fieldInfo
	for _, field := range msg.Fields {
		ops := readFieldOps(field.Desc.Options())
		for _, o := range ops {
			if o == op {
				fields = append(fields, fieldInfo{
					Number: int32(field.Desc.Number()),
					Name:   string(field.Desc.Name()),
				})
				break
			}
		}
	}
	return fields
}

func skipField(b []byte, num protowire.Number, typ protowire.Type) int {
	switch typ {
	case protowire.VarintType:
		_, n := protowire.ConsumeVarint(b)
		return n
	case protowire.Fixed32Type:
		_, n := protowire.ConsumeFixed32(b)
		return n
	case protowire.Fixed64Type:
		_, n := protowire.ConsumeFixed64(b)
		return n
	case protowire.BytesType:
		_, n := protowire.ConsumeBytes(b)
		return n
	case protowire.StartGroupType:
		_, n := protowire.ConsumeGroup(num, b)
		return n
	default:
		return -1
	}
}
