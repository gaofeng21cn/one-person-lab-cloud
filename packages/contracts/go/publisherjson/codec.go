// Package publisherjson adapts the public publisher JSON vocabulary to typed
// protobuf messages. It does not replace the publisher schema validator or hash
// reserialized bytes as if they were an external publisher's original bytes.
package publisherjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func Marshal(m proto.Message) ([]byte, error) {
	if m == nil || !m.ProtoReflect().IsValid() {
		return nil, fmt.Errorf("publisher message is required")
	}
	raw, err := protojson.MarshalOptions{EmitDefaultValues: true}.Marshal(m)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err = dec.Decode(&value); err != nil {
		return nil, err
	}
	value, err = convert(value, m.ProtoReflect().Descriptor(), true, m.ProtoReflect())
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
func Unmarshal(data []byte, m proto.Message) error {
	var value map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return err
	}
	if dec.Decode(new(any)) != io.EOF {
		return fmt.Errorf("publisher JSON has trailing data")
	}
	value, err := convert(value, m.ProtoReflect().Descriptor(), false, nil)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return protojson.Unmarshal(raw, m)
}

// The generated oneof wrappers are wire-only. Public access contracts carry a
// discriminator in the selected object and have no protobuf wrapper property.
func union(md protoreflect.MessageDescriptor) bool {
	return md.Oneofs().Len() == 1 && !md.Oneofs().Get(0).IsSynthetic() && md.Oneofs().Get(0).Fields().Len() == md.Fields().Len()
}
func convert(v map[string]any, md protoreflect.MessageDescriptor, toPublic bool, m protoreflect.Message) (map[string]any, error) {
	if union(md) {
		if toPublic {
			for key, value := range v {
				fd := md.Fields().ByJSONName(key)
				if fd != nil && fd.Message() != nil {
					return convert(value.(map[string]any), fd.Message(), true, m.Get(fd).Message())
				}
			}
			return nil, fmt.Errorf("missing %s variant", md.Name())
		}
		for i := 0; i < md.Fields().Len(); i++ {
			fd := md.Fields().Get(i)
			if fd.Message() == nil {
				continue
			}
			candidate, err := convert(v, fd.Message(), false, nil)
			if err == nil {
				return map[string]any{fd.JSONName(): candidate}, nil
			}
		}
		return nil, fmt.Errorf("invalid %s variant", md.Name())
	}
	out := map[string]any{}
	for key, value := range v {
		fd := md.Fields().ByJSONName(key)
		if fd == nil {
			return nil, fmt.Errorf("unknown publisher property %s.%s", md.Name(), key)
		}
		if toPublic && !m.Has(fd) && !required[md.Name()][key] {
			continue
		}
		if fd.IsMap() {
			out[key] = value
			continue
		}
		cv := func(x any, child protoreflect.Message) (any, error) {
			if fd.Kind() == protoreflect.EnumKind {
				text, ok := x.(string)
				if !ok {
					return nil, fmt.Errorf("invalid %s enum", key)
				}
				for n, public := range enumNames[fd.Enum().FullName()] {
					ev := fd.Enum().Values().ByNumber(n)
					if ev == nil {
						continue
					}
					if toPublic && text == string(ev.Name()) {
						return public, nil
					}
					if !toPublic && text == public {
						return string(ev.Name()), nil
					}
				}
				return nil, fmt.Errorf("unsupported publisher enum %s: %s", fd.Enum().Name(), text)
			}
			if fd.Message() != nil {
				obj, ok := x.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("invalid publisher object %s", key)
				}
				return convert(obj, fd.Message(), toPublic, child)
			}
			if toPublic && (fd.Kind() == protoreflect.Int64Kind || fd.Kind() == protoreflect.Uint64Kind) {
				if text, ok := x.(string); ok {
					return json.Number(text), nil
				}
			}
			return x, nil
		}
		if fd.IsList() {
			list, ok := value.([]any)
			if !ok {
				return nil, fmt.Errorf("invalid publisher list %s", key)
			}
			converted := make([]any, 0, len(list))
			for i, x := range list {
				var child protoreflect.Message
				if toPublic && fd.Message() != nil {
					child = m.Get(fd).List().Get(i).Message()
				}
				y, err := cv(x, child)
				if err != nil {
					return nil, err
				}
				converted = append(converted, y)
			}
			out[key] = converted
		} else {
			var child protoreflect.Message
			if toPublic && fd.Message() != nil {
				child = m.Get(fd).Message()
			}
			x, err := cv(value, child)
			if err != nil {
				return nil, err
			}
			out[key] = x
		}
	}
	return out, nil
}
