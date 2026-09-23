package client

import (
	"archive/zip"
	"bytes"
	"fmt"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const rpcBundleMaxBytes = 5 << 20

// This builder accepts only the native public diagnostics projection, never
// Config, private files, a caller path, or a retired diagnostics envelope.
func buildNativeDiagnosticsArchive(source *ipc.Diagnostics) ([]byte, error) {
	if source == nil || proto.Size(source) > rpcBundleMaxBytes {
		return nil, fmt.Errorf("invalid diagnostics archive source")
	}
	clean := proto.Clone(source).(*ipc.Diagnostics)
	redactNativeDiagnosticsMessage(clean.ProtoReflect())
	data, err := protojson.MarshalOptions{Indent: "  "}.Marshal(clean)
	if err != nil || len(data) > rpcBundleMaxBytes {
		return nil, fmt.Errorf("diagnostics archive source exceeds encoding limits")
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	header := &zip.FileHeader{Name: "diagnostics.json", Method: zip.Deflate}
	header.SetMode(0o600)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return nil, fmt.Errorf("diagnostics archive creation failed")
	}
	if _, err := entry.Write(data); err != nil {
		return nil, fmt.Errorf("diagnostics archive encoding failed")
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("diagnostics archive finalization failed")
	}
	if buffer.Len() > rpcBundleMaxBytes {
		return nil, fmt.Errorf("diagnostics archive exceeds size limit")
	}
	return buffer.Bytes(), nil
}

func redactNativeDiagnosticsMessage(message protoreflect.Message) {
	// Unknown fields may contain data outside the reviewed public vocabulary.
	message.SetUnknown(nil)
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.Name() == "browser_url" {
			message.Clear(field)
			return true
		}
		if field.IsList() {
			list := value.List()
			for i := 0; i < list.Len(); i++ {
				switch field.Kind() {
				case protoreflect.MessageKind:
					redactNativeDiagnosticsMessage(list.Get(i).Message())
				case protoreflect.StringKind:
					list.Set(i, protoreflect.ValueOfString(RedactServiceLogMessage(list.Get(i).String())))
				}
			}
			return true
		}
		switch field.Kind() {
		case protoreflect.MessageKind:
			redactNativeDiagnosticsMessage(value.Message())
		case protoreflect.StringKind:
			message.Set(field, protoreflect.ValueOfString(RedactServiceLogMessage(value.String())))
		}
		return true
	})
}
