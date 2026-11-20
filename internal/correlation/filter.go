package correlation

import (
	"errors"
	"fmt"
	"regexp"
	"sync"

	"github.com/ilamparithi-in/matfix/internal/bus"
)

// regexCache caches compiled regular expressions to avoid repeated compilation.
var (
	regexCacheMu sync.RWMutex
	regexCache   = make(map[string]*regexp.Regexp)
)

// compileRegexCached retrieves a compiled regexp from cache or compiles and caches it.
func compileRegexCached(pattern string) (*regexp.Regexp, error) {
	regexCacheMu.RLock()
	if re, ok := regexCache[pattern]; ok {
		regexCacheMu.RUnlock()
		return re, nil
	}
	regexCacheMu.RUnlock()

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	regexCacheMu.Lock()
	if len(regexCache) > 1024 {
		regexCache = make(map[string]*regexp.Regexp)
	}
	regexCache[pattern] = re
	regexCacheMu.Unlock()

	return re, nil
}

// # stringSet

// stringSet is a compiled include/exclude set for string matching.
type stringSet struct {
	include map[string]struct{} // nil = unrestricted
	exclude map[string]struct{} // nil = no exclusions
}

// newStringSet builds a stringSet from a *StringSetFilter. A nil filter produces
// a stringSet that matches every value (no constraints).
func newStringSet(f *StringSetFilter) stringSet {
	if f == nil {
		return stringSet{}
	}
	ss := stringSet{}
	if len(f.Include) > 0 {
		ss.include = make(map[string]struct{}, len(f.Include))
		for _, v := range f.Include {
			ss.include[v] = struct{}{}
		}
	}
	if len(f.Exclude) > 0 {
		ss.exclude = make(map[string]struct{}, len(f.Exclude))
		for _, v := range f.Exclude {
			ss.exclude[v] = struct{}{}
		}
	}
	return ss
}

// active reports whether this stringSet has any constraint.
func (ss stringSet) active() bool {
	return ss.include != nil || ss.exclude != nil
}

// matches reports whether v passes the include/exclude constraints.
func (ss stringSet) matches(v string) bool {
	if ss.exclude != nil {
		if _, excluded := ss.exclude[v]; excluded {
			return false
		}
	}
	if ss.include != nil {
		if _, included := ss.include[v]; !included {
			return false
		}
	}
	return true
}

// # compiledFilterNode

// compiledFilterNode mirrors the shape of FilterNode with pre-compiled fields.
type compiledFilterNode struct {
	senderID  stringSet
	roomID    stringSet
	eventType stringSet

	inReplyTo        string
	bodyRegex        *regexp.Regexp // nil when BodyRegex == ""
	reactionKey      string
	relatesToEventID string
	hasAttachment    *bool
	minTimestamp     int64
	maxTimestamp     int64

	allOf []*compiledFilterNode
	anyOf []*compiledFilterNode
	not   *compiledFilterNode
}

// CompiledFilter is the top-level evaluated filter.
type CompiledFilter struct {
	root *compiledFilterNode
}

type compiledFilter = CompiledFilter

// Validate checks a FilterNode for semantic errors before compilation.
func Validate(n *FilterNode) error {
	if n == nil {
		return nil
	}
	if n.AllOf != nil && len(n.AllOf) == 0 {
		return errors.New("correlation: all_of must be non-empty when set")
	}
	if n.AnyOf != nil && len(n.AnyOf) == 0 {
		return errors.New("correlation: any_of must be non-empty when set")
	}
	if n.BodyRegex != "" {
		if _, err := compileRegexCached(n.BodyRegex); err != nil {
			return fmt.Errorf("correlation: invalid body_regex %q: %w", n.BodyRegex, err)
		}
	}
	if n.MinTimestamp != 0 && n.MaxTimestamp != 0 && n.MinTimestamp > n.MaxTimestamp {
		return fmt.Errorf("correlation: min_timestamp (%d) > max_timestamp (%d)", n.MinTimestamp, n.MaxTimestamp)
	}
	for _, sf := range []*StringSetFilter{n.SenderID, n.RoomID, n.EventType} {
		if sf != nil && len(sf.Include) == 0 && len(sf.Exclude) == 0 {
			return errors.New("correlation: StringSetFilter must have at least one include or exclude entry")
		}
	}
	for _, child := range n.AllOf {
		if err := Validate(child); err != nil {
			return err
		}
	}
	for _, child := range n.AnyOf {
		if err := Validate(child); err != nil {
			return err
		}
	}
	return Validate(n.Not)
}

func validate(n *FilterNode) error {
	return Validate(n)
}

// # Compilation

// compileNode recursively compiles a *FilterNode into a *compiledFilterNode.
func compileNode(n *FilterNode) (*compiledFilterNode, error) {
	if n == nil {
		return nil, nil
	}
	cn := &compiledFilterNode{
		senderID:         newStringSet(n.SenderID),
		roomID:           newStringSet(n.RoomID),
		eventType:        newStringSet(n.EventType),
		inReplyTo:        n.InReplyTo,
		reactionKey:      n.ReactionKey,
		relatesToEventID: n.RelatesToEventID,
		hasAttachment:    n.HasAttachment,
		minTimestamp:     n.MinTimestamp,
		maxTimestamp:     n.MaxTimestamp,
	}
	if n.BodyRegex != "" {
		re, err := compileRegexCached(n.BodyRegex)
		if err != nil {
			return nil, fmt.Errorf("correlation: invalid body_regex %q: %w", n.BodyRegex, err)
		}
		cn.bodyRegex = re
	}
	for _, child := range n.AllOf {
		cc, err := compileNode(child)
		if err != nil {
			return nil, err
		}
		cn.allOf = append(cn.allOf, cc)
	}
	for _, child := range n.AnyOf {
		cc, err := compileNode(child)
		if err != nil {
			return nil, err
		}
		cn.anyOf = append(cn.anyOf, cc)
	}
	if n.Not != nil {
		cc, err := compileNode(n.Not)
		if err != nil {
			return nil, err
		}
		cn.not = cc
	}
	return cn, nil
}

// NewCompiledFilter validates and compiles a *FilterNode tree.
// A nil root produces a filter that matches every event.
func NewCompiledFilter(root *FilterNode) (*CompiledFilter, error) {
	if err := Validate(root); err != nil {
		return nil, err
	}
	cn, err := compileNode(root)
	if err != nil {
		return nil, err
	}
	return &CompiledFilter{root: cn}, nil
}

func newCompiledFilter(root *FilterNode) (*CompiledFilter, error) {
	return NewCompiledFilter(root)
}

// # Matching

// Matches reports whether env satisfies the filter for the given accountID.
// If accountID is empty, the account check is bypassed (matches any account).
func (cf *CompiledFilter) Matches(env bus.EventEnvelope, accountID string) bool {
	if accountID != "" && env.AccountID != accountID {
		return false
	}
	if cf.root == nil {
		return true
	}
	return cf.root.matchesEnvelope(env)
}

// MatchesEnvelope reports whether env satisfies the filter predicates.
func (cf *CompiledFilter) MatchesEnvelope(env bus.EventEnvelope) bool {
	if cf.root == nil {
		return true
	}
	return cf.root.matchesEnvelope(env)
}

func (cf *CompiledFilter) matches(env bus.EventEnvelope, accountID string) bool {
	return cf.Matches(env, accountID)
}

func matrixEventType(t bus.EventType) string {
	switch t {
	case bus.EventInboundMessage:
		return "m.room.message"
	case bus.EventInboundReaction:
		return "m.reaction"
	case bus.EventInboundEdit:
		return "m.room.message.edit"
	case bus.EventInboundRedaction:
		return "m.room.redaction"
	case bus.EventInboundMembership:
		return "m.room.member"
	case bus.EventInboundReceipt:
		return "m.receipt"
	default:
		return string(t)
	}
}

// matchesEnvelope evaluates all predicates for this node (all ANDed).
func (n *compiledFilterNode) matchesEnvelope(env bus.EventEnvelope) bool {
	// Room set filter
	if n.roomID.active() && !n.roomID.matches(env.RoomID) {
		return false
	}
	// Event type set filter (supports both internal bus.EventType and standard Matrix event types)
	if n.eventType.active() {
		if !n.eventType.matches(string(env.Type)) && !n.eventType.matches(matrixEventType(env.Type)) {
			return false
		}
	}
	// Payload-specific predicates (inapplicable-predicate semantics)
	if !n.matchesPayload(env) {
		return false
	}
	// AllOf: every child must match
	for _, child := range n.allOf {
		if !child.matchesEnvelope(env) {
			return false
		}
	}
	// AnyOf: at least one child must match (empty slice is vacuously true)
	if len(n.anyOf) > 0 {
		matched := false
		for _, child := range n.anyOf {
			if child.matchesEnvelope(env) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	// Not: child must not match
	if n.not != nil && n.not.matchesEnvelope(env) {
		return false
	}
	return true
}

// matchesPayload evaluates payload-specific predicates with inapplicable-predicate
// strict semantics: a predicate that is set but cannot apply to the given event
// type causes the node to return false immediately.
func (n *compiledFilterNode) matchesPayload(env bus.EventEnvelope) bool {
	switch p := env.Payload.(type) {
	case bus.InboundMessageEvent:
		if n.senderID.active() && !n.senderID.matches(p.SenderID) {
			return false
		}
		if n.inReplyTo != "" && p.InReplyTo != n.inReplyTo {
			return false
		}
		if n.bodyRegex != nil && !n.bodyRegex.MatchString(p.Body) {
			return false
		}
		if n.hasAttachment != nil && *n.hasAttachment != (p.Attachment != nil) {
			return false
		}
		if n.reactionKey != "" {
			return false // inapplicable to messages
		}
		if n.relatesToEventID != "" {
			return false // inapplicable to messages
		}
		if !n.matchesTimestamp(p.Timestamp.UnixMilli()) {
			return false
		}

	case bus.InboundReactionEvent:
		if n.senderID.active() && !n.senderID.matches(p.SenderID) {
			return false
		}
		if n.reactionKey != "" && p.Key != n.reactionKey {
			return false
		}
		if n.relatesToEventID != "" && p.RelatesToEventID != n.relatesToEventID {
			return false
		}
		if n.inReplyTo != "" {
			return false // inapplicable to reactions
		}
		if n.bodyRegex != nil {
			return false // inapplicable to reactions
		}
		if n.hasAttachment != nil {
			return false // always false for reactions
		}
		if !n.matchesTimestamp(p.Timestamp.UnixMilli()) {
			return false
		}

	case bus.InboundEditEvent:
		if n.senderID.active() && !n.senderID.matches(p.SenderID) {
			return false
		}
		if n.bodyRegex != nil && !n.bodyRegex.MatchString(p.NewBody) {
			return false
		}
		if n.relatesToEventID != "" && p.RelatesToEventID != n.relatesToEventID {
			return false
		}
		if n.inReplyTo != "" {
			return false // inapplicable to edits
		}
		if n.reactionKey != "" {
			return false // inapplicable to edits
		}
		if n.hasAttachment != nil {
			return false // always false for edits
		}
		if !n.matchesTimestamp(p.Timestamp.UnixMilli()) {
			return false
		}

	case bus.InboundRedactionEvent:
		if n.senderID.active() && !n.senderID.matches(p.SenderID) {
			return false
		}
		if n.relatesToEventID != "" && p.Redacts != n.relatesToEventID {
			return false
		}
		if n.inReplyTo != "" {
			return false // inapplicable to redactions
		}
		if n.bodyRegex != nil {
			return false // inapplicable to redactions
		}
		if n.reactionKey != "" {
			return false // inapplicable to redactions
		}
		if n.hasAttachment != nil {
			return false // always false for redactions
		}
		if !n.matchesTimestamp(p.Timestamp.UnixMilli()) {
			return false
		}

	case bus.InboundMembershipEvent:
		if n.senderID.active() && !n.senderID.matches(p.UserID) {
			return false
		}
		// All other scalar predicates are inapplicable to membership events.
		if n.inReplyTo != "" || n.bodyRegex != nil || n.reactionKey != "" ||
			n.relatesToEventID != "" || n.hasAttachment != nil {
			return false
		}
		if !n.matchesTimestamp(p.Timestamp.UnixMilli()) {
			return false
		}

	case bus.InboundReceiptEvent:
		if n.senderID.active() && !n.senderID.matches(p.UserID) {
			return false
		}
		// All other scalar predicates are inapplicable to receipt events.
		if n.inReplyTo != "" || n.bodyRegex != nil || n.reactionKey != "" ||
			n.relatesToEventID != "" || n.hasAttachment != nil {
			return false
		}
		if !n.matchesTimestamp(p.Timestamp.UnixMilli()) {
			return false
		}

	default:
		return false
	}
	return true
}

// matchesTimestamp checks the min/max timestamp window. 0 = unset.
func (n *compiledFilterNode) matchesTimestamp(ts int64) bool {
	if n.minTimestamp != 0 && ts < n.minTimestamp {
		return false
	}
	if n.maxTimestamp != 0 && ts > n.maxTimestamp {
		return false
	}
	return true
}
