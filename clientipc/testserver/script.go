package testserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"connectrpc.com/connect"
	pb "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Script is a test-harness file, not an IPC message or a production config.
// Request/response bodies use canonical protobuf JSON and generated types.
type Script struct {
	Steps []ScriptStep `json:"steps"`
}

type ScriptStep struct {
	Method    string            `json:"method"`
	Request   json.RawMessage   `json:"request"`
	Responses []json.RawMessage `json:"responses"`
	Failure   *ScriptFailure    `json:"failure,omitempty"`
	HoldOpen  bool              `json:"hold_open,omitempty"`
}

type ScriptFailure struct {
	RPCCode string          `json:"rpc_code"`
	Detail  json.RawMessage `json:"detail"`
}

// Load validates the complete file before returning a usable server. Error
// messages are intentionally fixed: protobuf JSON parse errors can echo tokens.
func Load(reader io.Reader) (*Server, error) {
	data, err := io.ReadAll(io.LimitReader(reader, (8<<20)+1))
	if err != nil || len(data) > 8<<20 {
		return nil, errors.New("cannot read bounded test script")
	}
	var script Script
	if err := strictJSON(data, &script); err != nil {
		return nil, errors.New("invalid test script JSON")
	}
	if len(script.Steps) == 0 || len(script.Steps) > 4096 {
		return nil, errors.New("test script requires 1..4096 steps")
	}
	server := New()
	methods := pb.File_client_v0_service_proto.Services().ByName("ClientService").Methods()
	for _, input := range script.Steps {
		method := methods.ByName(protoreflect.Name(input.Method))
		if method == nil {
			return nil, errors.New("unknown test script method")
		}
		request, err := decodeMessage(method.Input(), input.Request)
		if err != nil {
			return nil, err
		}
		step := Step{Method: input.Method, Request: request, HoldOpen: input.HoldOpen}
		for _, raw := range input.Responses {
			response, err := decodeMessage(method.Output(), raw)
			if err != nil {
				return nil, err
			}
			step.Responses = append(step.Responses, response)
		}
		if input.Failure != nil {
			var code connect.Code
			if err := code.UnmarshalText([]byte(input.Failure.RPCCode)); err != nil {
				return nil, errors.New("invalid scripted RPC error code")
			}
			failure := new(pb.Failure)
			if err := protojson.Unmarshal(input.Failure.Detail, failure); err != nil || failure.GetCode() == pb.ErrorCode_ERROR_CODE_UNSPECIFIED {
				return nil, errors.New("invalid scripted Failure")
			}
			// Preserve safe typed details, not an arbitrary transport error string.
			detail, err := connect.NewErrorDetail(failure)
			if err != nil {
				return nil, errors.New("invalid scripted Failure detail")
			}
			wireError := connect.NewError(code, errors.New("scripted RPC failure"))
			wireError.AddDetail(detail)
			step.Err = wireError
		}
		if err := server.Expect(step); err != nil {
			return nil, err
		}
	}
	return server, nil
}

func strictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func decodeMessage(descriptor protoreflect.MessageDescriptor, raw []byte) (proto.Message, error) {
	typeOf, err := protoregistry.GlobalTypes.FindMessageByName(descriptor.FullName())
	if err != nil {
		return nil, errors.New("unregistered test message")
	}
	message := typeOf.New().Interface()
	if len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("test message must be a protobuf JSON object")
	}
	if err := protojson.Unmarshal(raw, message); err != nil {
		return nil, errors.New("invalid test protobuf JSON")
	}
	return message, nil
}
