package actor

import (
	"fmt"
	re "regexp"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/pborman/uuid"

	"github.com/topology-io/spider/pkg/promise"
)

//////////////////////////////////////////////////////////////////////////////
// Interfaces
//////////////////////////////////////////////////////////////////////////////

// System manages actor lifecycles, message routing and transport, and
// supervision,
type System interface {
	Name() string
	Config() SystemConfig
	State() SystemState
	NewActor(name string, info Info) (Ref, error)
	NewDefaultActor(name string, factory Factory) (Ref, error)
	Lookup(Address) Ref
	Shutdown()
	GracefulShutdown()
	AddProbe(probe Ref)
	RemoveProbe(probe Ref)
}

// SystemState represents the state of an actor system.
type SystemState uint32

const (
	// SystemRunning means that actors in the system are processing
	// messages.
	SystemRunning SystemState = iota

	// SystemStopped means that  actors in the system are stopped and no new
	// actors can be created. This is a terminal state.
	SystemStopped
)

// Info is a description of how to instantiate a concrete actor.
type Info struct {
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
func NewSystem(name string, config SystemConfig) (System, error) {
	sys := &system{}
	sys.name = name
	sys.config = config
	sys.state = SystemRunning
	sys.actors = make(map[Address]actorRef)
	sys.probes = make(map[Address]Ref)
	return sys, nil
}

// implements actor.System
type system struct {
	name   string
	config SystemConfig

	state SystemState

	actors      map[Address]actorRef // TODO(CD): refactor flat hierarchy into a tree
	actorsMutex sync.RWMutex

	probes      map[Address]Ref
	probesMutex sync.RWMutex
}

func (s *system) Name() string {
	return s.name
}

func (s *system) Config() SystemConfig {
	return s.config
}

func (s *system) State() SystemState {
	return s.state
}

func (s *system) NewDefaultActor(name string, factory Factory) (Ref, error) {
	return s.NewActor(name, Info{DefaultConfig(), factory})
}

func (s *system) NewActor(name string, info Info) (Ref, error) {
	return s.newActor("user", name, info)
}

func (s *system) newSystemActor(name string, info Info) (Ref, error) {
	return s.newActor("system", name, info)
}

func (s *system) newActor(namespace string, name string, info Info) (Ref, error) {
	// Validate that the supplied actor name is sane.
	namePattern := re.MustCompile("[a-z0-9][a-z0-9-]*")
	if !namePattern.MatchString(name) {
		return nil, fmt.Errorf("name must conform to %s", namePattern.String())
	}

	address := Address(fmt.Sprintf("spider:///%s/%s/%s", s.name, namespace, name))

	/////////////////////////////////////////////////////////////////////////////
	// Begin critical section
	//
	// We acquire the writers' lock associated with the  actors collection
	// before mutating the actors data structure.
	s.actorsMutex.Lock()

	if s.state != SystemRunning {
		s.actorsMutex.Unlock()
		return nil, fmt.Errorf("failed to create actor [%s] because actor system [%s] is not running", address, s.name)
	}
	if _, exists := s.actors[address]; exists {
		s.actorsMutex.Unlock()
		return nil, fmt.Errorf("actor with address [%s] already exists", address)
	}

	ref := actorRef{
		address:    address,
		underlying: info.Factory(),
		system:     s,
		mailbox:    make(chan envelope, info.Config.MailboxSize),
		life:       promise.NewPromise(),
	}
	s.actors[address] = ref
	s.actorsMutex.Unlock()
	// End critical section
	/////////////////////////////////////////////////////////////////////////////

	// Defer cleanup following actor termination.
	ref.life.AndThen(func(err error) {
		log.WithField("address", ref.address).Debug("Cleaning up actor state")
		///////////////////////////////////////////////////////////////////////////
		// Begin critical section
		//
		// We acquire the writers' lock associated with the  actors collection
		// before mutating the actors data structure.
		s.actorsMutex.Lock()
		defer s.actorsMutex.Unlock()

		// We acquire the writers' lock for the probes collection to ensure that:
		//   a) no two writers concurrently mutate it
		//   b) no readers attempt to iterate over it while we mutate it
		s.probesMutex.Lock()
		defer s.probesMutex.Unlock()

		close(ref.mailbox)
		delete(s.actors, ref.address)
		delete(s.probes, ref.address)
		// End critical section
		///////////////////////////////////////////////////////////////////////////
	})

	// Start the actor's worker routine.
	ref.start()

	log.WithField("address", ref.address).Info("Actor created")

	return ref, nil
}

func (s *system) Lookup(address Address) Ref {
	/////////////////////////////////////////////////////////////////////////////
	// Begin critical section
	//
	// We acquire the readers' lock for the actors collection to avoid indexing
	// the map while it is being concurrently modified via NewActor or a dead
	// actor's cleanup continuation.
	s.actorsMutex.RLock()
	defer s.actorsMutex.RUnlock()
	return s.actors[address]
	// End critical section
	/////////////////////////////////////////////////////////////////////////////
}

func (s *system) Shutdown() {
	log.WithField("name", s.name).Info("Actor system shutting down")
	s.shutdown(func(r actorRef) { r.life.Complete(nil) })
}

func (s *system) GracefulShutdown() {
	log.Info("Actor system shutting down \"gracefully\"")
	s.shutdown(func(r actorRef) { r.Send(nil, PoisonPill) })
}

func (s *system) shutdown(strategy func(actorRef)) {
	/////////////////////////////////////////////////////////////////////////////
	// Begin critical section
	//
	// We acquire the writers' lock associated with the actors collection to
	// prevent more actors from being created underneath us (via NewActor).
	// While we have the lock, we set the system state to SystemStopped, which
	// will cause subsequent calls to NewActor to return an error once we have
	// released the lock.
	s.actorsMutex.Lock()
	defer s.actorsMutex.Unlock()

	s.state = SystemStopped

	// Stop all actors in the system using the supplied strategy.
	for _, ref := range s.actors {
		strategy(ref)
	}
	// End critical section
	/////////////////////////////////////////////////////////////////////////////
}

func (s *system) AddProbe(probe Ref) {
	log.WithField("probe", probe.Address()).Info("Adding probe")
	/////////////////////////////////////////////////////////////////////////////
	// Begin critical section
	//
	// See: actor.actorRef.Send
	//
	// We acquire the writers' lock for the probes collection to ensure that:
	//   a) no two writers concurrently mutate it
	//   b) no readers attempt to iterate over it while we mutate it
	s.probesMutex.Lock()
	defer s.probesMutex.Unlock()
	s.probes[probe.Address()] = probe
	// End critical section
	/////////////////////////////////////////////////////////////////////////////
}

func (s *system) RemoveProbe(probe Ref) {
	log.WithField("probe", probe.Address()).Info("Removing probe")
	/////////////////////////////////////////////////////////////////////////////
	// Begin critical section
	//
	// See: actor.actorRef.Send
	//
	// We acquire the writers' lock for the probes collection to ensure that:
	//   a) no two writers concurrently mutate it
	//   b) no readers attempt to iterate over it while we mutate it
	s.probesMutex.Lock()
	defer s.probesMutex.Unlock()
	delete(s.probes, probe.Address())
	// End critical section
	/////////////////////////////////////////////////////////////////////////////
}

// Implements actor.Ref
type actorRef struct {
	address    Address
	underlying Actor
	system     *system
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
	replyAddress := Address("<none>")
	if replyTo != nil {
		replyAddress = replyTo.Address()
	}
	log.WithFields(log.Fields{
		"replyTo": replyAddress,
		"address": r.address,
		"message": msg,
	}).Debug("Sending message")
	if r.life.IsComplete() {
		log.WithFields(log.Fields{
			"replyTo": replyAddress,
			"address": r.address,
			"message": msg,
		}).Warn("Message sent to stopped actor, re-routing to dead letters")
		// TODO(CD): set up dead letters actor and actually re-route.
		return
	}

	/////////////////////////////////////////////////////////////////////////////
	// Begin critical section
	//
	// See: actor.system.AddProbe
	// See: actor.system.RemoveProbe
	//
	// We acquire the readers' lock to prevent the probes collection from being
	// concurrently modified while we are iterating over it.
	r.system.probesMutex.RLock()

	// Construct a local snapshot of installed probes, so we can release the
	// lock before the potentially blocking sends below.
	probesSnapshot := map[Address]Ref{}
	for a, p := range r.system.probes {
		probesSnapshot[a] = p
	}
	r.system.probesMutex.RUnlock()
	// End critical section
	/////////////////////////////////////////////////////////////////////////////

	// Do not broadcast probe notifications again to probes!!!
	if _, receiverIsProbe := probesSnapshot[r.address]; !receiverIsProbe {
		for _, probe := range probesSnapshot {
			probe.Send(nil, Event{SendEvent, SendData{Recipient: r, ReplyTo: replyTo, Message: msg}})
		}
	}
	r.mailbox <- envelope{replyTo, msg}
}

func (r actorRef) Ask(msg interface{}, timeout time.Duration) (interface{}, error) {
	reply := promise.NewPromise()
	var answer interface{}
	workerName := fmt.Sprintf("ask-%s", uuid.NewRandom())
	proxy, err := r.system.newSystemActor(workerName, Info{
		Config{MailboxSize: 2},
		func() Actor { return newAskProxy(reply, &answer) },
	})
	if err != nil {
		return nil, err
	}

	// Send the message, specifying the new proxy actor as the reply-to
	// address.
	r.Send(proxy, msg)

	// Now, wait up to the supplied duration the proxy to signal receipt of
	// a reply message.
	err = reply.AwaitUntil(timeout)

	// Either the ask proxy filled in the answer and completed the promise,
	// or we have timed out waiting for a response. In either case, clean up the
	// proxy and return whatever we got (answer or error).
	proxy.Send(nil, PoisonPill)
	return answer, err
}

func (r actorRef) AddWatcher(watcher Ref) {
	log.WithFields(log.Fields{
		"watcher": watcher.Address(),
		"address": r.address,
	}).Info("Configuring death watch")
	r.life.AndThen(func(err error) {
		log.WithFields(log.Fields{
			"watcher": watcher.Address(),
			"address": r.address,
		}).Debug("Sending death notification")
		watcher.Send(nil, Stopped{r.address})
	})
}

func (r actorRef) Life() promise.ReadOnlyPromise {
	return r.life
}

func (r actorRef) start() {
	go func() {
		log.WithField("address", r.address).Info("Starting actor")

		// Invoke the actor PreStart hook.
		r.underlying.Prestart(Context{SystemContext{r.system}, r})

		for !r.life.IsComplete() {
			// Take a message from the mailbox
			e := <-r.mailbox

			// Skip nil messages.
			if e.msg == nil {
				log.WithFields(log.Fields{
					"address": r.address,
					"replyTo": e.replyTo,
				}).Debug("Dropping nil message")
				continue
			}
			// Intercept uncatchable lifecycle messages.
			if e.msg == PoisonPill {
				log.WithField("address", r.address).Info("Actor received PoisonPill")
				r.stop()
				continue
			}

			// Hand off control to user-defined message handler.
			r.underlying.Receive(
				ReceiveContext{SystemContext{r.system}, e.replyTo},
				e.msg,
			)
		}
	}()
}

func (r actorRef) stop() {
	log.WithField("address", r.address).Info("Stopping actor")
	r.life.Complete(nil)
}
