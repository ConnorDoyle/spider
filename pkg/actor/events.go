package actor

// Event describes control-flow within an actor system.
type Event struct {
	Type string
	Data interface{}
}

const (
	// SendEvent denotes message send events.
	SendEvent = "send"
)

// SendData describes message send events.
type SendData struct {
	Recipient Ref
	ReplyTo   Ref
	Message   interface{}
}
