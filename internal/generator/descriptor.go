package generator

import (
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

// buildFilteredDescriptor clones the file descriptor and adds synthetic message
// types that contain only the fields allowed for each operation.
// allMessages is a cross-file map keyed by fully-qualified name
// (e.g. "dpprotos.services.entities.users.v1.UserInformation") built by buildAllMessages.
func buildFilteredDescriptor(file *protogen.File, rules []methodRule, allMessages map[string]*descriptorpb.DescriptorProto, allTypeFiles map[string]string) (*descriptorpb.FileDescriptorProto, error) {
	orig := proto.Clone(file.Proto).(*descriptorpb.FileDescriptorProto)
	pkg := orig.GetPackage()

	// created tracks synthetic names already added to avoid duplicates/cycles.
	created := map[string]bool{}

	// addSynthetic recursively creates a synthetic message for msgFQN scoped to op,
	// appending it (and any sub-synthetics) to orig.MessageType.
	// Returns the short synthetic name (e.g. "UserInformation_UPDATE").
	var addSynthetic func(msgFQN, op string) string
	addSynthetic = func(msgFQN, op string) string {
		parts := strings.Split(msgFQN, ".")
		shortName := parts[len(parts)-1]
		syntheticName := shortName + "_" + op

		if created[syntheticName] {
			return syntheticName
		}
		created[syntheticName] = true

		origMsg := allMessages[msgFQN]
		synthetic := &descriptorpb.DescriptorProto{
			Name: proto.String(syntheticName),
		}

		if origMsg != nil {
			allowedFields := getFieldsForOpRaw(origMsg, op)
			// oneofIndexMap remaps original oneof_decl indices to synthetic indices.
			// Proto3 optional fields use synthetic oneofs; without this, cloned
			// fields would have oneof_index values pointing to non-existent entries.
			oneofIndexMap := map[int32]int32{}
			for _, fi := range allowedFields {
				for _, origField := range origMsg.GetField() {
					if origField.GetNumber() != fi.Number {
						continue
					}
					cloned := proto.Clone(origField).(*descriptorpb.FieldDescriptorProto)
					cloned.Options = nil

					// Remap oneof_index: copy the referenced oneof_decl entry into
					// the synthetic message and update the index accordingly.
					if cloned.OneofIndex != nil {
						origOneofIdx := cloned.GetOneofIndex()
						if newIdx, ok := oneofIndexMap[origOneofIdx]; ok {
							cloned.OneofIndex = proto.Int32(newIdx)
						} else {
							origOneof := origMsg.GetOneofDecl()[origOneofIdx]
							clonedOneof := proto.Clone(origOneof).(*descriptorpb.OneofDescriptorProto)
							newIdx = int32(len(synthetic.GetOneofDecl()))
							synthetic.OneofDecl = append(synthetic.OneofDecl, clonedOneof)
							oneofIndexMap[origOneofIdx] = newIdx
							cloned.OneofIndex = proto.Int32(newIdx)
						}
					}

					// If this field is a message type, check whether the sub-message
					// has any field_op annotations for this operation. If so, create
					// a filtered sub-synthetic and rewrite the type reference.
					if cloned.GetType() == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
						subTypeName := cloned.GetTypeName() // e.g. ".pkg.UserInformation"
						subFQN := strings.TrimPrefix(subTypeName, ".")
						subMsg := allMessages[subFQN]
						if subMsg != nil && len(getFieldsForOpRaw(subMsg, op)) > 0 {
							subSyntheticName := addSynthetic(subFQN, op)
							cloned.TypeName = proto.String("." + pkg + "." + subSyntheticName)
						}
					}

					synthetic.Field = append(synthetic.Field, cloned)
					break
				}
			}
		}

		orig.MessageType = append(orig.MessageType, synthetic)
		return syntheticName
	}

	// Update method InputType in cloned services and build top-level synthetics.
	type syntheticKey struct{ msgFQN, op string }
	topLevel := map[syntheticKey]string{} // key → syntheticName

	for _, rule := range rules {
		key := syntheticKey{rule.InputFQN, rule.Operation.String()}
		if _, already := topLevel[key]; !already {
			syntheticName := addSynthetic(rule.InputFQN, rule.Operation.String())
			topLevel[key] = syntheticName
		}
	}

	for _, svc := range orig.GetService() {
		for _, m := range svc.GetMethod() {
			for _, rule := range rules {
				svcParts := strings.Split(rule.ServiceFQN, ".")
				shortSvcName := svcParts[len(svcParts)-1]
				if svc.GetName() == shortSvcName && m.GetName() == rule.MethodName {
					newType := "." + pkg + "." + rule.SyntheticName
					m.InputType = proto.String(newType)
					break
				}
			}
		}
	}

	// Proto requires every referenced type to be in a directly-imported file;
	// transitive imports are not sufficient. Scan all messages (including the
	// newly created synthetics) and add any missing direct dependencies.
	existingDeps := make(map[string]bool)
	for _, dep := range orig.GetDependency() {
		existingDeps[dep] = true
	}
	for _, msg := range orig.GetMessageType() {
		for _, field := range msg.GetField() {
			if field.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE &&
				field.GetType() != descriptorpb.FieldDescriptorProto_TYPE_ENUM {
				continue
			}
			typeFQN := strings.TrimPrefix(field.GetTypeName(), ".")
			if sourceFile, ok := allTypeFiles[typeFQN]; ok && !existingDeps[sourceFile] {
				orig.Dependency = append(orig.Dependency, sourceFile)
				existingDeps[sourceFile] = true
			}
		}
	}

	// Strip source code info — unnecessary for reflection and bloats the descriptor.
	orig.SourceCodeInfo = nil

	return orig, nil
}

// getFieldsForOpRaw returns the fields of a raw DescriptorProto that are annotated
// with the given operation string (e.g. "UPDATE").
func getFieldsForOpRaw(msg *descriptorpb.DescriptorProto, op string) []fieldInfo {
	target := parseOpString(op)
	if target == OperationUnspecified {
		return nil
	}
	var fields []fieldInfo
	for _, field := range msg.GetField() {
		ops := readFieldOps(field.GetOptions())
		for _, o := range ops {
			if o == target {
				fields = append(fields, fieldInfo{
					Number: field.GetNumber(),
					Name:   field.GetName(),
				})
				break
			}
		}
	}
	return fields
}

func parseOpString(op string) Operation {
	switch op {
	case "CREATE":
		return OperationCreate
	case "READ":
		return OperationRead
	case "UPDATE":
		return OperationUpdate
	case "DELETE":
		return OperationDelete
	default:
		return OperationUnspecified
	}
}

