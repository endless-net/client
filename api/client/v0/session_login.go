package clientv0

import (
	"errors"
	"regexp"

	"google.golang.org/protobuf/proto"
)

// ErrInvalidSessionLoginInput describes invalid public input, without echoing it.
var ErrInvalidSessionLoginInput = errors.New("invalid session login input")

var sessionLoginUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validSessionLoginUUID(value string) bool {
	return sessionLoginUUID.MatchString(value) && value != "00000000-0000-0000-0000-000000000000"
}

func sessionLoginKnown(message proto.Message) bool {
	return len(message.ProtoReflect().GetUnknown()) == 0
}

func validSessionLoginProfile(profile *ProfileRef) bool {
	return profile == nil || sessionLoginKnown(profile) && (profile.ProfileId == "" || validSessionLoginUUID(profile.ProfileId))
}

// ValidateStartSessionLoginRequest checks typed input only. The runtime must
// separately enforce OS owner, current profile/registration, replay binding,
// instance/revision CAS, same user and durable dispatch before any side effects.
func ValidateStartSessionLoginRequest(request *StartSessionLoginRequest) error {
	if request == nil || !sessionLoginKnown(request) || !validSessionLoginProfile(request.Profile) || !validSessionLoginUUID(request.RequestId) || request.Mutation == nil || !sessionLoginKnown(request.Mutation) || request.Mutation.ExpectedInstanceId == "" || request.Mutation.ExpectedRevision == 0 {
		return ErrInvalidSessionLoginInput
	}
	return nil
}

// ValidateGetSessionLoginRequest checks the decoded selector. Binary Protobuf
// last-wins remains standard. This neither authorizes nor executes a lookup.
func ValidateGetSessionLoginRequest(request *GetSessionLoginRequest) error {
	if request == nil || !sessionLoginKnown(request) || !validSessionLoginProfile(request.Profile) {
		return ErrInvalidSessionLoginInput
	}
	switch selector := request.Selector.(type) {
	case *GetSessionLoginRequest_AttemptId:
		if selector != nil && validSessionLoginUUID(selector.AttemptId) {
			return nil
		}
	case *GetSessionLoginRequest_RequestId:
		if selector != nil && validSessionLoginUUID(selector.RequestId) {
			return nil
		}
	case *GetSessionLoginRequest_Current:
		if selector != nil && selector.Current {
			return nil
		}
	}
	return ErrInvalidSessionLoginInput
}
