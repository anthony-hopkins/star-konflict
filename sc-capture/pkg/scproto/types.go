package scproto

// MessageType is the wire command type carried in the header (0..38).
type MessageType uint16

// The message types this code reasons about by name. The full table of 39 is
// embedded; these are the ones with behaviour attached, so a typo in one is a
// compile error rather than a silent misclassification.
const (
	SCMDAssignedShard          MessageType = 0
	CCMDAuthRequest            MessageType = 4
	SCMDConnectDedicatedServer MessageType = 11
	CSCMDAsyncReq              MessageType = 13
	SCMDNotification           MessageType = 14
)

// String returns the type's name, or a numeric placeholder if it is unknown to
// this build's tables.
func (t MessageType) String() string {
	if n, ok := Name(KindMessageType, uint16(t)); ok {
		return n
	}
	return "MESSAGE_TYPE_" + itoa(int(t))
}

// Known reports whether this type appears in the embedded universe.
func (t MessageType) Known() bool {
	_, ok := Name(KindMessageType, uint16(t))
	return ok
}

// AsyncReqType is an AC_* opcode carried inside CSCMD_ASYNC_REQ.
type AsyncReqType uint16

// ACPlayerCredentials carries authentication material. Observing it is what
// triggers the credential warning on a session.
const ACPlayerCredentials AsyncReqType = 9

func (t AsyncReqType) String() string {
	if n, ok := Name(KindAsyncRequest, uint16(t)); ok {
		return n
	}
	return "AC_UNKNOWN_" + itoa(int(t))
}

func (t AsyncReqType) Known() bool {
	_, ok := Name(KindAsyncRequest, uint16(t))
	return ok
}

// NotificationType is an SN_* opcode carried inside SCMD_NOTIFICATION.
type NotificationType uint16

func (t NotificationType) String() string {
	if n, ok := Name(KindNotification, uint16(t)); ok {
		return n
	}
	return "SN_UNKNOWN_" + itoa(int(t))
}

func (t NotificationType) Known() bool {
	_, ok := Name(KindNotification, uint16(t))
	return ok
}

// IsContainer reports whether a message type wraps a sub-element. The two
// containers carry most of the traffic, and their inner opcode is the thing
// coverage actually tracks.
func (t MessageType) IsContainer() bool {
	return t == CSCMDAsyncReq || t == SCMDNotification
}

// CarriesCredentials reports whether a message type is known to carry
// authentication material in the clear. The master-server protocol has no
// transport encryption, so this is a real exposure, not a theoretical one.
func (t MessageType) CarriesCredentials() bool {
	return t == CCMDAuthRequest
}
