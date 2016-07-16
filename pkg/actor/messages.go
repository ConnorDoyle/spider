package actor

type internalMessage uint32

const (
	// PoisonPill cannot be consumed; it eventually kills the target actor.
	// Note that it does not necessarily stop the addressed actor instantly
	// as it will to continue to process its mailbox in order.
	PoisonPill internalMessage = iota
)

// Stopped is sent to supervisors and death-watchers of an actor when it dies.
type Stopped struct {
	address Address
}
