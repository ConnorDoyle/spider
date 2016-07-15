package actor

import (
	"fmt"

	"github.com/nqn/spider/pkg/promise"
)

//////////////////////////////////////////////////////////////////////////////
// Interfaces
//////////////////////////////////////////////////////////////////////////////

// System manages actor lifecycles, message routing and transport, and
// supervision,
type System interface {
	Config() SystemConfig
	NewActor(Spore) (Ref, error)
	Lookup(Address) Ref
	Shutdown()
	GracefulShutdown()
}

// Spore is a description of how to instantiate a concrete actor.
type Spore struct {
	Config
	Factory
}

// Factory is a nullary function that returns a new actor instance.
// Actor systems take this type as input to NewActor to avoid leaking
// references to the underlying actor, which could invalidate concurrency
// assumptions made in the actor body, or prevent dead actors from being
// reaped by the garbage collector after they are discarded by the actor
// system.
type Factory func() Actor

// SystemContext is an interface to the actor runtime.
type SystemContext struct {
	System System
}

// Context is an interface to the actor runtime and local information
// about the current actor.
type Context struct {
	SystemContext
	Self Ref
}

// ReceiveContext is an interface to the actor runtime and local information
// about the current message.
type ReceiveContext struct {
	SystemContext
	ReplyTo Ref
}

//////////////////////////////////////////////////////////////////////////////
// Implementations
//////////////////////////////////////////////////////////////////////////////

// NewSystem returns a newly allocated actor system with the supplied
// configuration.
func NewSystem(config SystemConfig) (System, error) {
	sys := &system{}
	sys.config = config
	sys.actors = make(map[Address]actorRef)
	return sys, nil
}

// implements actor.System
type system struct {
	config SystemConfig
	actors map[Address]actorRef // TODO(CD): refactor flat hierarchy into a tree
}

func (s *system) Config() SystemConfig {
	return s.config
}

func (s *system) NewActor(spore Spore) (Ref, error) {
	underlying := spore.Factory()
	address := Address(fmt.Sprintf("spider:///user/%s", underlying.Name()))

	if _, exists := s.actors[address]; exists {
		return nil, fmt.Errorf("actor with address [%s] already exists", address)
	}

	ref := actorRef{
		address:    address,
		underlying: underlying,
		context:    SystemContext{s},
		mailbox:    make(chan envelope, spore.Config.MailboxSize),
		life:       promise.NewPromise(),
	}

	s.actors[address] = ref

	// Initialize the actor.
	ref.underlying.Init(Context{ref.context, ref})

	// Defer cleanup following actor termination.
	ref.life.AndThen(func(err error) {
		// TODO(CD): log this
		close(ref.mailbox)
		delete(s.actors, ref.address)
	})

	// Start the actor's worker routine.
	go func() {
		for {
			if ref.life.IsComplete() {
				return
			}
			e := <-ref.mailbox

			// Intercept nil and uncatchable lifecycle messages.
			if e.msg == nil {
				// Skip delivery for nil messaes.
				// TODO(CD): log this at warn level
				continue
			}
			if e.msg == PoisonPill {
				// TODO(CD): log this
				ref.life.Complete(nil)
				return
			}

			ref.underlying.Receive(ReceiveContext{ref.context, e.replyTo}, e.msg)
		}
	}()

	// TODO(CD): Log successful actor creation.

	return ref, nil
}

func (s *system) Lookup(address Address) Ref {
	return s.actors[address]
}

func (s *system) Shutdown() {
	for _, ref := range s.actors {
		ref.life.Complete(nil)
	}
}

func (s *system) GracefulShutdown() {
	for _, ref := range s.actors {
		ref.Send(nil, PoisonPill)
	}
}

// implements actor.Ref
type actorRef struct {
	address    Address
	underlying Actor
	context    SystemContext
	mailbox    chan envelope
	life       promise.Promise
}

type envelope struct {
	replyTo Ref
	msg     interface{}
}

func (r actorRef) Address() Address {
	return r.address
}

func (r actorRef) Send(replyTo Ref, msg interface{}) {
	r.mailbox <- envelope{replyTo, msg}
}
