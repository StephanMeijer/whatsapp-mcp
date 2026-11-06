// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// contextInfoField is the field every content sub-message (ExtendedTextMessage,
// ImageMessage, ...) uses for its ContextInfo.
const contextInfoField protoreflect.Name = "contextInfo"

// contextInfo returns the ContextInfo of m's content sub-message, or nil
// when it has none (a plain Conversation, for one). whatsmeow has no
// accessor for this across message types, so it is found by field name.
func contextInfo(m *waE2E.Message) *waE2E.ContextInfo {
	if m == nil {
		return nil
	}
	var ci *waE2E.ContextInfo
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.IsMap() {
			return true
		}
		sub := v.Message()
		f := sub.Descriptor().Fields().ByName(contextInfoField)
		if f == nil || !sub.Has(f) {
			return true
		}
		ci, _ = sub.Get(f).Message().Interface().(*waE2E.ContextInfo)
		return false
	})
	return ci
}

// setContextInfo sets ci on m's content sub-message. It reports false when
// m has no sub-message that takes one (a plain Conversation, for one).
func setContextInfo(m *waE2E.Message, ci *waE2E.ContextInfo) bool {
	set := false
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.IsMap() {
			return true
		}
		sub := v.Message()
		f := sub.Descriptor().Fields().ByName(contextInfoField)
		if f == nil {
			return true
		}
		sub.Set(f, protoreflect.ValueOfMessage(ci.ProtoReflect()))
		set = true
		return false
	})
	return set
}

// forwarding reports whether m was forwarded, and how often (WhatsApp's
// forwarding score; the phone says "Forwarded many times" from 5).
func forwarding(m *waE2E.Message) (bool, int) {
	ci := contextInfo(m)
	return ci.GetIsForwarded(), int(ci.GetForwardingScore())
}
