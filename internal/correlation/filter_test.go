package correlation

import (
	"testing"
	"time"

	"github.com/ilamparithi-in/matfix/internal/bus"
)

// # Test helpers

func boolPtr(b bool) *bool { return &b }

func makeEnv(accountID, roomID string, evtType bus.EventType, payload any) bus.EventEnvelope {
	return bus.EventEnvelope{
		Type:      evtType,
		AccountID: accountID,
		RoomID:    roomID,
		Payload:   payload,
	}
}

func mustCompile(t *testing.T, root *FilterNode) *compiledFilter {
	t.Helper()
	cf, err := newCompiledFilter(root)
	if err != nil {
		t.Fatalf("newCompiledFilter: %v", err)
	}
	return cf
}

var (
	ts1 = time.Unix(1_700_000_000, 0)
	ts2 = time.Unix(1_700_001_000, 0)
	ts3 = time.Unix(1_700_002_000, 0)
)

const (
	accountA = "acct-a"
	roomX    = "!room-x:example.org"
	senderB  = "@bob:example.org"
	senderC  = "@carol:example.org"
)

// # Validation

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		node    *FilterNode
		wantErr bool
	}{
		{"nil node", nil, false},
		{"empty node", &FilterNode{}, false},
		{
			"empty AllOf slice",
			&FilterNode{AllOf: []*FilterNode{}},
			true,
		},
		{
			"non-empty AllOf",
			&FilterNode{AllOf: []*FilterNode{{}}},
			false,
		},
		{
			"empty AnyOf slice",
			&FilterNode{AnyOf: []*FilterNode{}},
			true,
		},
		{
			"non-empty AnyOf",
			&FilterNode{AnyOf: []*FilterNode{{}}},
			false,
		},
		{
			"invalid body_regex",
			&FilterNode{BodyRegex: "[invalid"},
			true,
		},
		{
			"valid body_regex",
			&FilterNode{BodyRegex: "^(yes|no)$"},
			false,
		},
		{
			"min_timestamp greater than max_timestamp",
			&FilterNode{MinTimestamp: 2000, MaxTimestamp: 1000},
			true,
		},
		{
			"min_timestamp equals max_timestamp",
			&FilterNode{MinTimestamp: 1000, MaxTimestamp: 1000},
			false,
		},
		{
			"empty StringSetFilter on SenderID",
			&FilterNode{SenderID: &StringSetFilter{}},
			true,
		},
		{
			"StringSetFilter with include only",
			&FilterNode{SenderID: &StringSetFilter{Include: []string{"@a:b"}}},
			false,
		},
		{
			"StringSetFilter with exclude only",
			&FilterNode{SenderID: &StringSetFilter{Exclude: []string{"@a:b"}}},
			false,
		},
		{
			"nested invalid regex in AllOf child",
			&FilterNode{AllOf: []*FilterNode{{BodyRegex: "[bad"}}},
			true,
		},
		{
			"nested empty AnyOf in Not child",
			&FilterNode{Not: &FilterNode{AnyOf: []*FilterNode{}}},
			true,
		},
		{
			"nested valid Not child",
			&FilterNode{Not: &FilterNode{BodyRegex: "ok"}},
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validate(tc.node)
			if (err != nil) != tc.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// # stringSet (newStringSet + matches)

func TestStringSet(t *testing.T) {
	tests := []struct {
		name   string
		filter *StringSetFilter
		value  string
		want   bool
	}{
		{"nil filter passes everything", nil, "anything", true},
		{"include: value present", &StringSetFilter{Include: []string{"a", "b"}}, "a", true},
		{"include: value absent", &StringSetFilter{Include: []string{"a", "b"}}, "c", false},
		{"exclude: value present", &StringSetFilter{Exclude: []string{"a"}}, "a", false},
		{"exclude: value absent", &StringSetFilter{Exclude: []string{"a"}}, "b", true},
		{"both: value in include and exclude", &StringSetFilter{Include: []string{"a"}, Exclude: []string{"a"}}, "a", false},
		{"both: in include, not in exclude", &StringSetFilter{Include: []string{"a", "b"}, Exclude: []string{"b"}}, "a", true},
		{"both: not in include", &StringSetFilter{Include: []string{"a"}, Exclude: []string{"c"}}, "b", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ss := newStringSet(tc.filter)
			if got := ss.matches(tc.value); got != tc.want {
				t.Errorf("stringSet.matches(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// # Account guard

func TestAccountGuard(t *testing.T) {
	cf := mustCompile(t, &FilterNode{})
	env := makeEnv("other-account", roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderB, Timestamp: ts1,
	})
	if cf.matches(env, accountA) {
		t.Error("expected false for wrong account")
	}
}

// # Empty node matches all event types

func TestEmptyNodeMatchesAll(t *testing.T) {
	cf := mustCompile(t, &FilterNode{})
	cases := []struct {
		name    string
		evtType bus.EventType
		payload any
	}{
		{"message", bus.EventInboundMessage, bus.InboundMessageEvent{SenderID: senderB, Timestamp: ts1}},
		{"reaction", bus.EventInboundReaction, bus.InboundReactionEvent{SenderID: senderB, Key: "👍", Timestamp: ts1}},
		{"edit", bus.EventInboundEdit, bus.InboundEditEvent{SenderID: senderB, NewBody: "x", Timestamp: ts1}},
		{"redaction", bus.EventInboundRedaction, bus.InboundRedactionEvent{SenderID: senderB, Redacts: "$e", Timestamp: ts1}},
		{"membership", bus.EventInboundMembership, bus.InboundMembershipEvent{UserID: senderB, Timestamp: ts1}},
		{"receipt", bus.EventInboundReceipt, bus.InboundReceiptEvent{UserID: senderB, Timestamp: ts1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := makeEnv(accountA, roomX, tc.evtType, tc.payload)
			if !cf.matches(env, accountA) {
				t.Errorf("empty node should match %s", tc.name)
			}
		})
	}
}

// # Room and event-type set filters

func TestRoomSetFilter(t *testing.T) {
	cf := mustCompile(t, &FilterNode{
		RoomID: &StringSetFilter{Include: []string{roomX}},
	})
	payload := bus.InboundMessageEvent{SenderID: senderB, Timestamp: ts1}

	pass := makeEnv(accountA, roomX, bus.EventInboundMessage, payload)
	fail := makeEnv(accountA, "!other:example.org", bus.EventInboundMessage, payload)

	if !cf.matches(pass, accountA) {
		t.Error("expected match for included room")
	}
	if cf.matches(fail, accountA) {
		t.Error("expected no match for non-included room")
	}
}

func TestRoomExcludeFilter(t *testing.T) {
	cf := mustCompile(t, &FilterNode{
		RoomID: &StringSetFilter{Exclude: []string{roomX}},
	})
	payload := bus.InboundMessageEvent{SenderID: senderB, Timestamp: ts1}

	excluded := makeEnv(accountA, roomX, bus.EventInboundMessage, payload)
	other := makeEnv(accountA, "!other:example.org", bus.EventInboundMessage, payload)

	if cf.matches(excluded, accountA) {
		t.Error("expected no match for excluded room")
	}
	if !cf.matches(other, accountA) {
		t.Error("expected match for non-excluded room")
	}
}

func TestEventTypeSetFilter(t *testing.T) {
	cf := mustCompile(t, &FilterNode{
		EventType: &StringSetFilter{Include: []string{string(bus.EventInboundReaction)}},
	})
	reaction := makeEnv(accountA, roomX, bus.EventInboundReaction, bus.InboundReactionEvent{SenderID: senderB, Key: "👍", Timestamp: ts1})
	message := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{SenderID: senderB, Timestamp: ts1})

	if !cf.matches(reaction, accountA) {
		t.Error("reaction should match event_type include filter")
	}
	if cf.matches(message, accountA) {
		t.Error("message should not match event_type include filter for reaction only")
	}
}

// # Scalar predicates — applicable types

func TestInReplyTo(t *testing.T) {
	cf := mustCompile(t, &FilterNode{InReplyTo: "$reply-target"})

	match := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderB, InReplyTo: "$reply-target", Timestamp: ts1,
	})
	noMatch := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderB, InReplyTo: "$other", Timestamp: ts1,
	})
	empty := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderB, Timestamp: ts1,
	})

	if !cf.matches(match, accountA) {
		t.Error("expected match for correct in_reply_to")
	}
	if cf.matches(noMatch, accountA) {
		t.Error("expected no match for wrong in_reply_to")
	}
	if cf.matches(empty, accountA) {
		t.Error("expected no match when in_reply_to is empty")
	}
}

func TestBodyRegexMessage(t *testing.T) {
	cf := mustCompile(t, &FilterNode{BodyRegex: "^(yes|no)$"})

	yes := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{Body: "yes", Timestamp: ts1})
	no_ := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{Body: "no", Timestamp: ts1})
	bad := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{Body: "maybe", Timestamp: ts1})

	if !cf.matches(yes, accountA) {
		t.Error("expected 'yes' to match body_regex")
	}
	if !cf.matches(no_, accountA) {
		t.Error("expected 'no' to match body_regex")
	}
	if cf.matches(bad, accountA) {
		t.Error("expected 'maybe' to not match body_regex")
	}
}

func TestBodyRegexEdit(t *testing.T) {
	cf := mustCompile(t, &FilterNode{BodyRegex: "updated"})

	match := makeEnv(accountA, roomX, bus.EventInboundEdit, bus.InboundEditEvent{
		SenderID: senderB, NewBody: "updated content", Timestamp: ts1,
	})
	noMatch := makeEnv(accountA, roomX, bus.EventInboundEdit, bus.InboundEditEvent{
		SenderID: senderB, NewBody: "original content", Timestamp: ts1,
	})

	if !cf.matches(match, accountA) {
		t.Error("body_regex should match edit NewBody")
	}
	if cf.matches(noMatch, accountA) {
		t.Error("body_regex should not match non-matching edit NewBody")
	}
}

func TestHasAttachment(t *testing.T) {
	withAttachment := mustCompile(t, &FilterNode{HasAttachment: boolPtr(true)})
	withoutAttachment := mustCompile(t, &FilterNode{HasAttachment: boolPtr(false)})

	msgWithAtt := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID:   senderB,
		Timestamp:  ts1,
		Attachment: &bus.InboundAttachment{URL: "mxc://example.org/file"},
	})
	msgWithoutAtt := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID:  senderB,
		Timestamp: ts1,
	})

	if !withAttachment.matches(msgWithAtt, accountA) {
		t.Error("has_attachment=true should match message with attachment")
	}
	if withAttachment.matches(msgWithoutAtt, accountA) {
		t.Error("has_attachment=true should not match message without attachment")
	}
	if !withoutAttachment.matches(msgWithoutAtt, accountA) {
		t.Error("has_attachment=false should match message without attachment")
	}
	if withoutAttachment.matches(msgWithAtt, accountA) {
		t.Error("has_attachment=false should not match message with attachment")
	}
}

func TestReactionKey(t *testing.T) {
	cf := mustCompile(t, &FilterNode{
		EventType:   &StringSetFilter{Include: []string{string(bus.EventInboundReaction)}},
		ReactionKey: "👍",
	})

	thumbsUp := makeEnv(accountA, roomX, bus.EventInboundReaction, bus.InboundReactionEvent{
		SenderID: senderB, Key: "👍", Timestamp: ts1,
	})
	thumbsDown := makeEnv(accountA, roomX, bus.EventInboundReaction, bus.InboundReactionEvent{
		SenderID: senderB, Key: "👎", Timestamp: ts1,
	})

	if !cf.matches(thumbsUp, accountA) {
		t.Error("expected 👍 to match reaction_key filter")
	}
	if cf.matches(thumbsDown, accountA) {
		t.Error("expected 👎 to not match reaction_key filter")
	}
}

func TestRelatesToEventID(t *testing.T) {
	const target = "$target-event"
	cf := mustCompile(t, &FilterNode{RelatesToEventID: target})

	reactionMatch := makeEnv(accountA, roomX, bus.EventInboundReaction, bus.InboundReactionEvent{
		SenderID: senderB, RelatesToEventID: target, Timestamp: ts1,
	})
	reactionFail := makeEnv(accountA, roomX, bus.EventInboundReaction, bus.InboundReactionEvent{
		SenderID: senderB, RelatesToEventID: "$other", Timestamp: ts1,
	})
	editMatch := makeEnv(accountA, roomX, bus.EventInboundEdit, bus.InboundEditEvent{
		SenderID: senderB, RelatesToEventID: target, NewBody: "x", Timestamp: ts1,
	})
	redactionMatch := makeEnv(accountA, roomX, bus.EventInboundRedaction, bus.InboundRedactionEvent{
		SenderID: senderB, Redacts: target, Timestamp: ts1,
	})
	redactionFail := makeEnv(accountA, roomX, bus.EventInboundRedaction, bus.InboundRedactionEvent{
		SenderID: senderB, Redacts: "$other", Timestamp: ts1,
	})

	if !cf.matches(reactionMatch, accountA) {
		t.Error("relates_to_event_id should match reaction")
	}
	if cf.matches(reactionFail, accountA) {
		t.Error("relates_to_event_id should reject non-matching reaction")
	}
	if !cf.matches(editMatch, accountA) {
		t.Error("relates_to_event_id should match edit")
	}
	if !cf.matches(redactionMatch, accountA) {
		t.Error("relates_to_event_id should match redaction via Redacts field")
	}
	if cf.matches(redactionFail, accountA) {
		t.Error("relates_to_event_id should reject non-matching redaction")
	}
}

func TestSenderIDSetFilter(t *testing.T) {
	cf := mustCompile(t, &FilterNode{
		SenderID: &StringSetFilter{Exclude: []string{"@spam:example.org"}},
	})

	spamMsg := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: "@spam:example.org", Timestamp: ts1,
	})
	normalMsg := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderB, Timestamp: ts1,
	})
	spamReaction := makeEnv(accountA, roomX, bus.EventInboundReaction, bus.InboundReactionEvent{
		SenderID: "@spam:example.org", Key: "👍", Timestamp: ts1,
	})
	spamMembership := makeEnv(accountA, roomX, bus.EventInboundMembership, bus.InboundMembershipEvent{
		UserID: "@spam:example.org", Timestamp: ts1,
	})
	spamReceipt := makeEnv(accountA, roomX, bus.EventInboundReceipt, bus.InboundReceiptEvent{
		UserID: "@spam:example.org", Timestamp: ts1,
	})

	if cf.matches(spamMsg, accountA) {
		t.Error("spam sender should be excluded from messages")
	}
	if !cf.matches(normalMsg, accountA) {
		t.Error("normal sender should pass exclude filter")
	}
	if cf.matches(spamReaction, accountA) {
		t.Error("spam sender should be excluded from reactions")
	}
	if cf.matches(spamMembership, accountA) {
		t.Error("spam user should be excluded from membership events")
	}
	if cf.matches(spamReceipt, accountA) {
		t.Error("spam user should be excluded from receipt events")
	}
}

// # Inapplicable predicates — strict false semantics

func TestInapplicablePredicates(t *testing.T) {
	tests := []struct {
		name    string
		filter  *FilterNode
		evtType bus.EventType
		payload any
	}{
		// in_reply_to inapplicable
		{
			"in_reply_to on reaction",
			&FilterNode{InReplyTo: "$x"},
			bus.EventInboundReaction,
			bus.InboundReactionEvent{SenderID: senderB, Key: "👍", Timestamp: ts1},
		},
		{
			"in_reply_to on edit",
			&FilterNode{InReplyTo: "$x"},
			bus.EventInboundEdit,
			bus.InboundEditEvent{SenderID: senderB, NewBody: "x", Timestamp: ts1},
		},
		{
			"in_reply_to on redaction",
			&FilterNode{InReplyTo: "$x"},
			bus.EventInboundRedaction,
			bus.InboundRedactionEvent{SenderID: senderB, Redacts: "$y", Timestamp: ts1},
		},
		{
			"in_reply_to on membership",
			&FilterNode{InReplyTo: "$x"},
			bus.EventInboundMembership,
			bus.InboundMembershipEvent{UserID: senderB, Timestamp: ts1},
		},
		{
			"in_reply_to on receipt",
			&FilterNode{InReplyTo: "$x"},
			bus.EventInboundReceipt,
			bus.InboundReceiptEvent{UserID: senderB, Timestamp: ts1},
		},
		// body_regex inapplicable
		{
			"body_regex on reaction",
			&FilterNode{BodyRegex: "x"},
			bus.EventInboundReaction,
			bus.InboundReactionEvent{SenderID: senderB, Key: "x", Timestamp: ts1},
		},
		{
			"body_regex on redaction",
			&FilterNode{BodyRegex: "x"},
			bus.EventInboundRedaction,
			bus.InboundRedactionEvent{SenderID: senderB, Redacts: "$y", Timestamp: ts1},
		},
		{
			"body_regex on membership",
			&FilterNode{BodyRegex: "x"},
			bus.EventInboundMembership,
			bus.InboundMembershipEvent{UserID: senderB, Timestamp: ts1},
		},
		{
			"body_regex on receipt",
			&FilterNode{BodyRegex: "x"},
			bus.EventInboundReceipt,
			bus.InboundReceiptEvent{UserID: senderB, Timestamp: ts1},
		},
		// reaction_key inapplicable
		{
			"reaction_key on message",
			&FilterNode{ReactionKey: "👍"},
			bus.EventInboundMessage,
			bus.InboundMessageEvent{SenderID: senderB, Body: "👍", Timestamp: ts1},
		},
		{
			"reaction_key on edit",
			&FilterNode{ReactionKey: "👍"},
			bus.EventInboundEdit,
			bus.InboundEditEvent{SenderID: senderB, NewBody: "👍", Timestamp: ts1},
		},
		{
			"reaction_key on redaction",
			&FilterNode{ReactionKey: "👍"},
			bus.EventInboundRedaction,
			bus.InboundRedactionEvent{SenderID: senderB, Redacts: "$y", Timestamp: ts1},
		},
		{
			"reaction_key on membership",
			&FilterNode{ReactionKey: "👍"},
			bus.EventInboundMembership,
			bus.InboundMembershipEvent{UserID: senderB, Timestamp: ts1},
		},
		{
			"reaction_key on receipt",
			&FilterNode{ReactionKey: "👍"},
			bus.EventInboundReceipt,
			bus.InboundReceiptEvent{UserID: senderB, Timestamp: ts1},
		},
		// relates_to_event_id inapplicable
		{
			"relates_to_event_id on message",
			&FilterNode{RelatesToEventID: "$x"},
			bus.EventInboundMessage,
			bus.InboundMessageEvent{SenderID: senderB, Timestamp: ts1},
		},
		{
			"relates_to_event_id on membership",
			&FilterNode{RelatesToEventID: "$x"},
			bus.EventInboundMembership,
			bus.InboundMembershipEvent{UserID: senderB, Timestamp: ts1},
		},
		{
			"relates_to_event_id on receipt",
			&FilterNode{RelatesToEventID: "$x"},
			bus.EventInboundReceipt,
			bus.InboundReceiptEvent{UserID: senderB, Timestamp: ts1},
		},
		// has_attachment always false on non-message types
		{
			"has_attachment=true on reaction",
			&FilterNode{HasAttachment: boolPtr(true)},
			bus.EventInboundReaction,
			bus.InboundReactionEvent{SenderID: senderB, Key: "👍", Timestamp: ts1},
		},
		{
			"has_attachment=true on edit",
			&FilterNode{HasAttachment: boolPtr(true)},
			bus.EventInboundEdit,
			bus.InboundEditEvent{SenderID: senderB, NewBody: "x", Timestamp: ts1},
		},
		{
			"has_attachment=true on redaction",
			&FilterNode{HasAttachment: boolPtr(true)},
			bus.EventInboundRedaction,
			bus.InboundRedactionEvent{SenderID: senderB, Redacts: "$y", Timestamp: ts1},
		},
		{
			"has_attachment=true on membership",
			&FilterNode{HasAttachment: boolPtr(true)},
			bus.EventInboundMembership,
			bus.InboundMembershipEvent{UserID: senderB, Timestamp: ts1},
		},
		{
			"has_attachment=true on receipt",
			&FilterNode{HasAttachment: boolPtr(true)},
			bus.EventInboundReceipt,
			bus.InboundReceiptEvent{UserID: senderB, Timestamp: ts1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cf := mustCompile(t, tc.filter)
			env := makeEnv(accountA, roomX, tc.evtType, tc.payload)
			if cf.matches(env, accountA) {
				t.Errorf("inapplicable predicate should return false, got true")
			}
		})
	}
}

// # Timestamp window

func TestTimestampWindow(t *testing.T) {
	ts1ms := ts1.UnixMilli()
	ts2ms := ts2.UnixMilli()
	ts3ms := ts3.UnixMilli()

	tests := []struct {
		name         string
		minTimestamp int64
		maxTimestamp int64
		eventTS      time.Time
		want         bool
	}{
		{"no window (both 0)", 0, 0, ts2, true},
		{"min only: at min", ts1ms, 0, ts1, true},
		{"min only: above min", ts1ms, 0, ts2, true},
		{"min only: below min", ts2ms, 0, ts1, false},
		{"max only: at max", 0, ts3ms, ts3, true},
		{"max only: below max", 0, ts3ms, ts2, true},
		{"max only: above max", 0, ts1ms, ts2, false},
		{"in window", ts1ms, ts3ms, ts2, true},
		{"exactly at min", ts1ms, ts3ms, ts1, true},
		{"exactly at max", ts1ms, ts3ms, ts3, true},
		{"below window", ts2ms, ts3ms, ts1, false},
		{"above window", ts1ms, ts2ms, ts3, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cf := mustCompile(t, &FilterNode{
				MinTimestamp: tc.minTimestamp,
				MaxTimestamp: tc.maxTimestamp,
			})
			env := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
				SenderID: senderB, Timestamp: tc.eventTS,
			})
			if got := cf.matches(env, accountA); got != tc.want {
				t.Errorf("timestamp window [%d,%d] for ts=%d: got %v, want %v",
					tc.minTimestamp, tc.maxTimestamp, tc.eventTS.UnixMilli(), got, tc.want)
			}
		})
	}
}

// # Combinators

func TestAllOf(t *testing.T) {
	cf := mustCompile(t, &FilterNode{
		AllOf: []*FilterNode{
			{SenderID: &StringSetFilter{Include: []string{senderB}}},
			{BodyRegex: "hello"},
		},
	})

	both := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderB, Body: "hello world", Timestamp: ts1,
	})
	onlySender := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderB, Body: "goodbye", Timestamp: ts1,
	})
	onlyBody := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderC, Body: "hello there", Timestamp: ts1,
	})

	if !cf.matches(both, accountA) {
		t.Error("all_of: both conditions met should match")
	}
	if cf.matches(onlySender, accountA) {
		t.Error("all_of: only sender condition met should not match")
	}
	if cf.matches(onlyBody, accountA) {
		t.Error("all_of: only body condition met should not match")
	}
}

func TestAnyOf(t *testing.T) {
	cf := mustCompile(t, &FilterNode{
		AnyOf: []*FilterNode{
			{SenderID: &StringSetFilter{Include: []string{senderB}}},
			{BodyRegex: "hello"},
		},
	})

	bySenderB := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderB, Body: "goodbye", Timestamp: ts1,
	})
	bodyHello := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderC, Body: "hello there", Timestamp: ts1,
	})
	neither := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderC, Body: "goodbye", Timestamp: ts1,
	})

	if !cf.matches(bySenderB, accountA) {
		t.Error("any_of: first condition met should match")
	}
	if !cf.matches(bodyHello, accountA) {
		t.Error("any_of: second condition met should match")
	}
	if cf.matches(neither, accountA) {
		t.Error("any_of: no condition met should not match")
	}
}

func TestNot(t *testing.T) {
	cf := mustCompile(t, &FilterNode{
		Not: &FilterNode{SenderID: &StringSetFilter{Include: []string{senderB}}},
	})

	fromB := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderB, Timestamp: ts1,
	})
	fromC := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderC, Timestamp: ts1,
	})

	if cf.matches(fromB, accountA) {
		t.Error("not: senderB should be excluded")
	}
	if !cf.matches(fromC, accountA) {
		t.Error("not: senderC should pass the not filter")
	}
}

func TestNestedCombinators(t *testing.T) {
	// any_of: [has_attachment=true, all_of: [body_regex="urgent", sender=@bob]]
	cf := mustCompile(t, &FilterNode{
		AnyOf: []*FilterNode{
			{HasAttachment: boolPtr(true)},
			{
				AllOf: []*FilterNode{
					{BodyRegex: "urgent"},
					{SenderID: &StringSetFilter{Include: []string{senderB}}},
				},
			},
		},
	})

	withAtt := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderC, Timestamp: ts1, Attachment: &bus.InboundAttachment{URL: "mxc://x/y"},
	})
	urgentFromB := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderB, Body: "this is urgent", Timestamp: ts1,
	})
	urgentFromC := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderC, Body: "this is urgent", Timestamp: ts1,
	})
	neitherMatch := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderC, Body: "hello", Timestamp: ts1,
	})

	if !cf.matches(withAtt, accountA) {
		t.Error("nested: attachment path should match")
	}
	if !cf.matches(urgentFromB, accountA) {
		t.Error("nested: urgent from senderB should match via all_of")
	}
	if cf.matches(urgentFromC, accountA) {
		t.Error("nested: urgent from senderC should not match (wrong sender in all_of)")
	}
	if cf.matches(neitherMatch, accountA) {
		t.Error("nested: no path satisfied should not match")
	}
}

func TestNotWithAnyOf(t *testing.T) {
	// not: {any_of: [sender=@bob, body_regex="spam"]}
	cf := mustCompile(t, &FilterNode{
		Not: &FilterNode{
			AnyOf: []*FilterNode{
				{SenderID: &StringSetFilter{Include: []string{senderB}}},
				{BodyRegex: "spam"},
			},
		},
	})

	fromB := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderB, Body: "hi", Timestamp: ts1,
	})
	spamFromC := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderC, Body: "buy spam now", Timestamp: ts1,
	})
	normalFromC := makeEnv(accountA, roomX, bus.EventInboundMessage, bus.InboundMessageEvent{
		SenderID: senderC, Body: "hello", Timestamp: ts1,
	})

	if cf.matches(fromB, accountA) {
		t.Error("not+any_of: senderB should be excluded")
	}
	if cf.matches(spamFromC, accountA) {
		t.Error("not+any_of: spam body from senderC should be excluded")
	}
	if !cf.matches(normalFromC, accountA) {
		t.Error("not+any_of: normal message from senderC should pass")
	}
}

// # newCompiledFilter validation errors

func TestNewCompiledFilterValidationError(t *testing.T) {
	_, err := newCompiledFilter(&FilterNode{BodyRegex: "[invalid"})
	if err == nil {
		t.Error("expected error for invalid body_regex, got nil")
	}

	_, err = newCompiledFilter(&FilterNode{AllOf: []*FilterNode{}})
	if err == nil {
		t.Error("expected error for empty all_of slice, got nil")
	}
}
