package grpc

import (
	"errors"
	"fmt"
	"sort"

	_ "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/protovalidate"
	"github.com/mozership/probe/internal/contractcheck"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// annotationFiles are the .proto files of the annotations a contract reads,
// which are taken from the definitions built into Probe rather than from
// the import paths, so that the files of a service can import them without
// having them at hand.
var annotationFiles = []string{
	"buf/validate/validate.proto",
	"google/api/field_behavior.proto",
}

// constraints returns what in msg, a request or a response found in the
// message as in says, breaks the constraints the .proto files annotate its
// fields with: the rules of buf.validate, which protovalidate checks, and
// google.api.field_behavior, by which a request must hold the fields that
// are REQUIRED and a response must not hold those that are INPUT_ONLY.
func (c *contract) constraints(in string, msg protoreflect.Message) []any {
	var out []any
	if c.validator != nil {
		var ve *protovalidate.ValidationError
		if err := c.validator.Validate(msg.Interface()); errors.As(err, &ve) {
			for _, v := range ve.Violations {
				p := v.Proto
				out = append(out, contractcheck.Violation(in,
					fmt.Sprintf("%s body breaks the rule %s of buf.validate", in, p.GetRuleId()),
					p.GetMessage(),
					fieldPath(protovalidate.FieldPathString(p.GetField()))))
			}
		} else if err != nil {
			out = append(out, contractcheck.Violation(in, fmt.Sprintf("%s body cannot be checked against buf.validate", in), err.Error(), ""))
		}
	}
	switch in {
	case "request":
		out = append(out, behaviors(msg, "$", annotations.FieldBehavior_REQUIRED, false, func(f protoreflect.FieldDescriptor, path string) any {
			return contractcheck.Violation(in, fmt.Sprintf("request body lacks the field %s, which is REQUIRED", f.Name()), "google.api.field_behavior says a request must hold it", path)
		})...)
	case "response":
		out = append(out, behaviors(msg, "$", annotations.FieldBehavior_INPUT_ONLY, true, func(f protoreflect.FieldDescriptor, path string) any {
			return contractcheck.Violation(in, fmt.Sprintf("response body holds the field %s, which is INPUT_ONLY", f.Name()), "google.api.field_behavior says a response must not hold it", path)
		})...)
	}
	return out
}

func fieldPath(p string) string {
	if p == "" {
		return ""
	}
	return "$." + p
}

// behaviors returns a violation, made by found, for each field of msg at any
// depth whose google.api.field_behavior holds behavior and which is set
// when set is true, or not set when it is false. Only the messages msg
// holds are gone into, as a field of one it does not hold is not asked for.
func behaviors(msg protoreflect.Message, path string, behavior annotations.FieldBehavior, set bool, found func(protoreflect.FieldDescriptor, string) any) []any {
	var out []any
	fields := msg.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		field := path + "." + string(f.Name())
		if hasBehavior(f, behavior) && msg.Has(f) == set {
			out = append(out, found(f, field))
		}
		if !msg.Has(f) {
			continue
		}
		v := msg.Get(f)
		switch {
		case f.IsMap():
			if f.MapValue().Message() == nil {
				continue
			}
			keys := []protoreflect.MapKey{}
			v.Map().Range(func(k protoreflect.MapKey, _ protoreflect.Value) bool {
				keys = append(keys, k)
				return true
			})
			sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
			for _, k := range keys {
				out = append(out, behaviors(v.Map().Get(k).Message(), fmt.Sprintf("%s[%s]", field, k.String()), behavior, set, found)...)
			}
		case f.IsList():
			if f.Message() == nil {
				continue
			}
			for j := 0; j < v.List().Len(); j++ {
				out = append(out, behaviors(v.List().Get(j).Message(), fmt.Sprintf("%s[%d]", field, j), behavior, set, found)...)
			}
		case f.Message() != nil:
			out = append(out, behaviors(v.Message(), field, behavior, set, found)...)
		}
	}
	return out
}

// hasBehavior reports whether f is annotated with behavior by
// google.api.field_behavior. The extension is read by its name through
// reflection: the options of compiled .proto files hold it as a value of
// their own, not of the type generated for it.
func hasBehavior(f protoreflect.FieldDescriptor, behavior annotations.FieldBehavior) bool {
	opts := f.Options()
	if opts == nil {
		return false
	}
	found := false
	name := annotations.E_FieldBehavior.TypeDescriptor().FullName()
	opts.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.FullName() != name || !fd.IsList() {
			return true
		}
		for i := 0; i < v.List().Len(); i++ {
			if v.List().Get(i).Enum() == protoreflect.EnumNumber(behavior) {
				found = true
			}
		}
		return false
	})
	return found
}
