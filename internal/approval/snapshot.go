// Package approval owns private durable browser receipts using producer DTOs.
package approval

import (
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/endless-net/client/internal/enrollment"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

var ErrProtocol = errors.New("approval response invalid")

func ValidBase(base string) bool {
	u, err := url.Parse(base)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Host == strings.ToLower(u.Host) && u.User == nil && u.Opaque == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.String() == base
}

type Snapshot struct {
	Request          coordinator.NodeEnrollmentRequest
	PollToken        string
	PollAfterSeconds int
	ObservedAt       time.Time
}

func (Snapshot) String() string   { return "[private approval snapshot]" }
func (Snapshot) GoString() string { return "[private approval snapshot]" }

func ValidToken(token string) bool {
	if len(token) < 32 || len(token) > 256 {
		return false
	}
	for _, c := range token {
		if c < 0x21 || c > 0x7e || !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~+/=", c) {
			return false
		}
	}
	return true
}

// The published ID is opaque, not a local ner_* format. Restrict it to one
// bounded path segment; escaped separators/queries must not become another call.
func ValidID(id string) bool {
	if len(id) == 0 || len(id) > 256 || id == "." || id == ".." {
		return false
	}
	for _, c := range id {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.~", c) {
			continue
		}
		return false
	}
	return true
}

func ValidateView(q coordinator.CreateNodeEnrollmentRequest, v coordinator.NodeEnrollmentRequest, base string, created time.Time, hint int, now time.Time, create bool) error {
	if !ValidID(v.ID) || v.Hostname != q.Hostname || v.PublicKey != q.PublicKey || v.IdentityPublicKey != q.IdentityPublicKey || v.DeviceFingerprint != q.DeviceFingerprint || v.CreatedAt.IsZero() || v.CreatedAt.Before(created) || v.CreatedAt.After(now) || !v.ExpiresAt.Equal(v.CreatedAt.Add(enrollment.ApprovalTTL)) {
		return ErrProtocol
	}
	if q.AccountID != "" && q.AccountID != v.AccountID || q.NetworkID != "" && q.NetworkID != v.NetworkID {
		return ErrProtocol
	}
	expected := base + "?" + url.Values{"enrollment_request": []string{v.ID}}.Encode()
	if v.ApprovalURL != expected || !ValidText(v.NetworkName, true, true) || hint < 0 || hint > int(enrollment.ApprovalTTL/time.Second) || create && hint == 0 {
		return ErrProtocol
	}
	for i, tag := range v.Tags {
		if !ValidText(tag, false, false) || slices.Contains(v.Tags[:i], tag) {
			return ErrProtocol
		}
	}
	approved := v.ApprovedAt != nil && approvalStamp(v.ApprovedAt, v, now) && ValidText(v.ApprovedByUserID, false, false) && ValidText(v.AccountID, false, false) && ValidText(v.NetworkID, false, false)
	rejected := v.RejectedAt != nil && approvalStamp(v.RejectedAt, v, now) && ValidText(v.RejectedByUserID, false, false) && ValidText(v.AccountID, false, false)
	if v.ApprovedAt != nil || v.ApprovedByUserID != "" {
		if !approved || v.RejectedAt != nil || v.RejectedByUserID != "" || v.RejectionReason != "" {
			return ErrProtocol
		}
	}
	if v.RejectedAt != nil || v.RejectedByUserID != "" || v.RejectionReason != "" {
		if !rejected || approved || !ValidText(v.RejectionReason, true, true) {
			return ErrProtocol
		}
	}
	if v.Status != "enrolled" && (v.NodeID != "" || v.CompletedAt != nil) {
		return ErrProtocol
	}
	switch v.Status {
	case "pending":
		if approved || rejected || !now.Before(v.ExpiresAt) || hint == 0 || v.AccountID != q.AccountID || v.NetworkID != q.NetworkID || v.NetworkName != q.NetworkName || !slices.Equal(v.Tags, q.Tags) {
			return ErrProtocol
		}
	case "approved":
		if !approved || !now.Before(v.ExpiresAt) {
			return ErrProtocol
		}
	case "rejected":
		if !rejected || v.NetworkID != q.NetworkID || v.NetworkName != q.NetworkName || !slices.Equal(v.Tags, q.Tags) {
			return ErrProtocol
		}
	case "expired":
		if now.Before(v.ExpiresAt) || rejected || !approved && (v.AccountID != q.AccountID || v.NetworkID != q.NetworkID || v.NetworkName != q.NetworkName || !slices.Equal(v.Tags, q.Tags)) {
			return ErrProtocol
		}
	case "enrolled":
		if !approved || !ValidID(v.NodeID) || !approvalStamp(v.CompletedAt, v, now) || v.CompletedAt.Before(*v.ApprovedAt) {
			return ErrProtocol
		}
	default:
		return ErrProtocol
	}
	if !create && v.Status != "pending" && hint != 0 {
		return ErrProtocol
	}
	return nil
}

func approvalStamp(stamp *time.Time, v coordinator.NodeEnrollmentRequest, now time.Time) bool {
	return stamp != nil && !stamp.Before(v.CreatedAt) && stamp.Before(v.ExpiresAt) && !stamp.After(now)
}

func sameApprovalTime(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}

func Successor(old, next coordinator.NodeEnrollmentRequest) bool {
	if old.ID != next.ID || !old.CreatedAt.Equal(next.CreatedAt) || !old.ExpiresAt.Equal(next.ExpiresAt) || old.ApprovalURL != next.ApprovalURL {
		return false
	}
	if old.ApprovedAt != nil || old.RejectedAt != nil {
		if old.AccountID != next.AccountID || old.NetworkID != next.NetworkID || old.NetworkName != next.NetworkName || !slices.Equal(old.Tags, next.Tags) || old.ApprovedByUserID != next.ApprovedByUserID || old.RejectedByUserID != next.RejectedByUserID || old.RejectionReason != next.RejectionReason || !sameApprovalTime(old.ApprovedAt, next.ApprovedAt) || !sameApprovalTime(old.RejectedAt, next.RejectedAt) {
			return false
		}
	}
	switch old.Status {
	case "pending":
		return true
	case "approved":
		return next.Status == "approved" || next.Status == "enrolled" || next.Status == "expired"
	case "expired", "rejected":
		return next.Status == old.Status
	case "enrolled":
		return next.Status == "enrolled" && next.NodeID == old.NodeID && sameApprovalTime(old.CompletedAt, next.CompletedAt)
	}
	return false
}

func ValidText(value string, empty, spaces bool) bool {
	return (empty && value == "" || validTextID(value)) && utf8.ValidString(value) && !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || !spaces && unicode.IsSpace(r)
	})
}

func validTextID(value string) bool {
	return value != "" && len(value) <= 256 && strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}

func (s Snapshot) Validate(q coordinator.CreateNodeEnrollmentRequest, base string, created time.Time) error {
	if !ValidToken(s.PollToken) || s.ObservedAt.IsZero() || s.ObservedAt.Before(s.Request.CreatedAt) {
		return ErrProtocol
	}
	return ValidateView(q, s.Request, base, created, s.PollAfterSeconds, s.ObservedAt, s.PollAfterSeconds > 0)
}
