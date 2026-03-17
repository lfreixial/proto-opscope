package generator

import (
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

// buildFilteredDescriptor clones the file descriptor and adds synthetic message
// types that contain only the fields allowed for each operation.
func buildFilteredDescriptor(file *protogen.File, rules []methodRule) (*descriptorpb.FileDescriptorProto, error) {
	orig := proto.Clone(file.Proto).(*descriptorpb.FileDescriptorProto)

	// Build map of original messages by short name.
	origMessages := map[string]*descriptorpb.DescriptorProto{}
	for _, msg := range orig.GetMessageType() {
		origMessages[msg.GetName()] = msg
	}

	type syntheticKey struct{ msgName, op string }
	synthetics := map[syntheticKey][]fieldInfo{}

	for _, rule := range rules {
		// Extract short message name from FQN.
		parts := strings.Split(rule.InputFQN, ".")
		shortName := parts[len(parts)-1]
		key := syntheticKey{shortName, rule.Operation.String()}
		synthetics[key] = rule.AllowedFields
	}

	// Update method InputType in cloned services.
	for _, svc := range orig.GetService() {
		for _, m := range svc.GetMethod() {
			for _, rule := range rules {
				// Match by short service name and method name.
				svcParts := strings.Split(rule.ServiceFQN, ".")
				shortSvcName := svcParts[len(svcParts)-1]
				if svc.GetName() == shortSvcName && m.GetName() == rule.MethodName {
					newType := "." + orig.GetPackage() + "." + rule.SyntheticName
					m.InputType = proto.String(newType)
					break
				}
			}
		}
	}

	// Create synthetic message types.
	for key, allowedFields := range synthetics {
		synthetic := &descriptorpb.DescriptorProto{
			Name: proto.String(key.msgName + "_" + key.op),
		}
		origMsg := origMessages[key.msgName]
		if origMsg != nil {
			for _, fi := range allowedFields {
				for _, origField := range origMsg.GetField() {
					if origField.GetNumber() == fi.Number {
						synthetic.Field = append(synthetic.Field, proto.Clone(origField).(*descriptorpb.FieldDescriptorProto))
						break
					}
				}
			}
		}
		orig.MessageType = append(orig.MessageType, synthetic)
	}

	return orig, nil
}
