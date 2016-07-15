package actor

type internalMessage uint32

const (
	// PoisonPill cannot be consumed; it eventually kills the target actor.
	// Note that it does not necessarily stop the addressed actor instantly
	// as it will to continue to process its mailbox in order.
	PoisonPill internalMessage = iota
)
