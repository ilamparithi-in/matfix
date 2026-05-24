package correlation

import (
	"fmt"
	"regexp"

	"github.com/ilamparithi-in/matfix/internal/bus"
)

// # Compiled filter

// compiledFilter is a FilterSpec with its BodyRegex pre-compiled.
// It is created once at registration time to avoid per-event compilation overhead.
type compiledFilter struct {
	spec      FilterSpec
	bodyRegex *regexp.Regexp // nil when FilterSpec.BodyRegex is empty
}

// newCompiledFilter compiles the BodyRegex from spec.
// Returns an error if BodyRegex is non-empty but syntactically invalid.
func newCompiledFilter(spec FilterSpec) (compiledFilter, error) {
	cf := compiledFilter{spec: spec}
	if spec.BodyRegex != "" {
		re, err := regexp.Compile(spec.BodyRegex)
		if err != nil {
			return compiledFilter{}, fmt.Errorf("correlation: invalid body regex %q: %w", spec.BodyRegex, err)
		}
		cf.bodyRegex = re
	}
	return cf, nil
}

// matches reports whether env satisfies the filter for the given accountID.
func (cf compiledFilter) matches(env bus.EventEnvelope, accountID string) bool {
	if env.AccountID != accountID {
		return false
	}

	wantType := cf.spec.EventType
	if wantType == "" {
		wantType = bus.EventInboundMessage
	}
	if env.Type != wantType {
		return false
	}

	if cf.spec.RoomID != "" && env.RoomID != cf.spec.RoomID {
		return false
	}

	return cf.matchesPayload(env.Payload)
}

// matchesPayload evaluates sender, relation, and body criteria against the
// concrete payload type.
func (cf compiledFilter) matchesPayload(payload any) bool {
	switch p := payload.(type) {
	case bus.InboundMessageEvent:
		if cf.spec.SenderID != "" && p.SenderID != cf.spec.SenderID {
			return false
		}
		if cf.spec.InReplyTo != "" && p.InReplyTo != cf.spec.InReplyTo {
			return false
		}
		if cf.bodyRegex != nil && !cf.bodyRegex.MatchString(p.Body) {
			return false
		}

	case bus.InboundReactionEvent:
		if cf.spec.SenderID != "" && p.SenderID != cf.spec.SenderID {
			return false
		}

	case bus.InboundEditEvent:
		if cf.spec.SenderID != "" && p.SenderID != cf.spec.SenderID {
			return false
		}

	case bus.InboundRedactionEvent:
		if cf.spec.SenderID != "" && p.SenderID != cf.spec.SenderID {
			return false
		}

	case bus.InboundMembershipEvent:
		if cf.spec.SenderID != "" && p.UserID != cf.spec.SenderID {
			return false
		}

	case bus.InboundReceiptEvent:
		if cf.spec.SenderID != "" && p.UserID != cf.spec.SenderID {
			return false
		}

	default:
		return false
	}

	return true
}
