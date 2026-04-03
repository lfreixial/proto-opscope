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
func buildFilteredDescriptor(file *protogen.File, rules []methodRule, allMessages map[string]*descriptorpb.DescriptorProto) (*descriptorpb.FileDescriptorProto, error) {
	orig := proto.Clone(file.Proto).(*descriptorpb.FileDescriptorProto)

	type syntheticKey struct{ msgFQN, op string }
	synthetics := map[syntheticKey][]fieldInfo{}

	for _, rule := range rules {
		key := syntheticKey{rule.InputFQN, rule.Operation.String()}
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
		parts := strings.Split(key.msgFQN, ".")
		shortName := parts[len(parts)-1]
		synthetic := &descriptorpb.DescriptorProto{
			Name: proto.String(shortName + "_" + key.op),
		}
		origMsg := allMessages[key.msgFQN]
		if origMsg != nil {
			for _, fi := range allowedFields {
				for _, origField := range origMsg.GetField() {
					if origField.GetNumber() == fi.Number {
						cloned := proto.Clone(origField).(*descriptorpb.FieldDescriptorProto)
						cloned.Options = nil // strip field_op annotations from synthetic fields
						synthetic.Field = append(synthetic.Field, cloned)
						break
					}
				}
			}
		}
		orig.MessageType = append(orig.MessageType, synthetic)
	}

	// Strip source code info — unnecessary for reflection and bloats the descriptor.
	orig.SourceCodeInfo = nil

	return orig, nil
}
