// Package publicjson adapts the canonical public API and publisher JSON to typed
// protobuf messages. It does not replace the publisher schema validator or hash
// reserialized bytes as if they were an external publisher's original bytes.
package publicjson

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

// omitted marks a value the public contract does not define, so the property is
// left out of the public object entirely instead of being emitted under a
// protobuf-only spelling.
type omitted struct{}

// fillForUnion resolves a public discriminator object against the oneof's message
// fields, including a name the protobuf JSON name does not carry.
func fillForUnion(md protoreflect.MessageDescriptor, key string) protoreflect.FieldDescriptor {
	for i := 0; i < md.Fields().Len(); i++ {
		fd := md.Fields().Get(i)
		if fd.Message() != nil && publicName(md, fd) == key {
			return fd
		}
	}
	return nil
}

// The generated oneof wrappers are wire-only. Public access contracts carry a
// discriminator in the selected object and have no protobuf wrapper property.
func union(md protoreflect.MessageDescriptor) bool {
	return md.Oneofs().Len() == 1 && !md.Oneofs().Get(0).IsSynthetic() && md.Oneofs().Get(0).Fields().Len() == md.Fields().Len()
}

// fieldFor resolves a public property name to its wire field. The canonical
// contract spelling is authoritative, so a property whose protobuf JSON name
// differs is resolved through the generated mapping rather than only through the
// protobuf name. This is how `monthlyPriceUSDMicros` reaches
// `monthly_price_usd_micros`, which protobuf alone spells `monthlyPriceUsdMicros`.
func fieldFor(md protoreflect.MessageDescriptor, key string, toPublic bool) protoreflect.FieldDescriptor {
	// Decoding is strict: the public boundary accepts exactly the contract
	// vocabulary. Resolving through the protobuf JSON name is only accepted when
	// that name is itself the published spelling, so the protobuf-only spelling is
	// not a second inbound alias for the same field.
	if toPublic {
		if field, ok := fieldToPublic[md.Name()][key]; ok {
			return md.Fields().ByName(protoreflect.Name(field))
		}
		return md.Fields().ByJSONName(key)
	}
	if field, ok := publicToField[md.Name()][key]; ok {
		return md.Fields().ByName(protoreflect.Name(field))
	}
	fd := md.Fields().ByJSONName(key)
	if fd != nil && publicName(md, fd) != key {
		return nil
	}
	return fd
}

// publicName is the property name an encoded response carries for a field.
func publicName(md protoreflect.MessageDescriptor, fd protoreflect.FieldDescriptor) string {
	if name, ok := fieldToPublic[md.Name()][string(fd.Name())]; ok {
		return name
	}
	return fd.JSONName()
}

func convert(v map[string]any, md protoreflect.MessageDescriptor, toPublic bool, m protoreflect.Message) (map[string]any, error) {
	if union(md) {
		if toPublic {
			for key, value := range v {
				fd := md.Fields().ByJSONName(key)
				if fd == nil {
					fd = fillForUnion(md, key)
				}
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
				return map[string]any{publicName(md, fd): candidate}, nil
			}
		}
		return nil, fmt.Errorf("invalid %s variant", md.Name())
	}
	out := map[string]any{}
	for key, value := range v {
		fd := fieldFor(md, key, toPublic)
		if fd == nil {
			return nil, fmt.Errorf("unknown publisher property %s.%s", md.Name(), key)
		}
		// Encoding answers with the contract spelling, so a response cannot leak the
		// protobuf name. Decoding hands protobuf its own JSON name, which is what the
		// generated message unmarshaller accepts.
		outKey := key
		if toPublic {
			outKey = publicName(md, fd)
			if !m.Has(fd) && !required[md.Name()][outKey] {
				continue
			}
		} else {
			outKey = fd.JSONName()
		}
		if fd.IsMap() {
			out[outKey] = value
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
				if toPublic {
					// The wire value has no published text. A required property would
					// make the response contract-invalid, so that is an error; an
					// optional one is simply not part of the public vocabulary and is
					// omitted rather than emitted under a protobuf spelling.
					if required[md.Name()][outKey] {
						return nil, fmt.Errorf("required property %s.%s has no public value for %s", md.Name(), outKey, text)
					}
					return omitted{}, nil
				}
				return nil, fmt.Errorf("unsupported publisher enum %s: %s", fd.Enum().Name(), text)
			}
			if fd.Message() != nil {
				if fd.Message().FullName() == "google.protobuf.Timestamp" || fd.Message().FullName() == "google.protobuf.Struct" || fd.Message().FullName() == "google.protobuf.Value" {
					return x, nil
				}
				obj, ok := x.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("invalid publisher object %s", key)
				}
				return convert(obj, fd.Message(), toPublic, child)
			}
			if fd.Kind() == protoreflect.Int64Kind || fd.Kind() == protoreflect.Uint64Kind {
				// The scalar form is the contract's, not protobuf's. protojson always
				// emits a 64-bit integer as a quoted string; the contract quotes only
				// the fields it types as a decimal string (USDMicros,
				// NonnegativeInt64), so the rest are emitted as JSON numbers. Decoding
				// accepts either form, exactly as protojson does.
				asString := stringScalars[md.Name()][string(fd.Name())]
				if toPublic {
					text, ok := x.(string)
					if !ok {
						number, isNumber := x.(json.Number)
						if !isNumber {
							return nil, fmt.Errorf("invalid %s integer", key)
						}
						text = number.String()
					}
					if asString {
						return text, nil
					}
					return json.Number(text), nil
				}
				// Decoding enforces the declared form as well: the contract quotes a
				// decimal string, so a bare JSON number is not the published shape, and
				// a field the contract types numerically is not accepted quoted.
				switch typed := x.(type) {
				case string:
					if !asString {
						return nil, fmt.Errorf("%s is a decimal string, the contract types it as a number", key)
					}
					return json.Number(typed), nil
				case json.Number:
					if asString {
						return nil, fmt.Errorf("%s is a number, the contract types it as a decimal string", key)
					}
					return typed, nil
				}
				return nil, fmt.Errorf("invalid %s integer", key)
			}
			return x, nil
		}
		if _, skip := value.(omitted); skip {
			continue
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
				if _, skip := y.(omitted); skip {
					continue
				}
				converted = append(converted, y)
			}
			out[outKey] = converted
		} else {
			var child protoreflect.Message
			if toPublic && fd.Message() != nil {
				child = m.Get(fd).Message()
			}
			x, err := cv(value, child)
			if err != nil {
				return nil, err
			}
			if _, skip := x.(omitted); skip {
				continue
			}
			out[outKey] = x
		}
	}
	return out, nil
}
