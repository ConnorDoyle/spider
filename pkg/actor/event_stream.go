package actor

import (
	"time"
)

// EventStream broadcasts published messages to a set of subscriber actors.
type EventStream interface {
	Subscribers() ([]Address, error)
	Subscribe(Ref)
	Unsubscribe(Address)
	Publish(msg interface{})
	Shutdown()
}

// NewEventStream returns a newly allocated event stream.
func NewEventStream(sys System, name string) (EventStream, error) {
	worker, err := sys.NewActor(name, Info{
		DefaultConfig(),
		func() Actor { return &esWorker{subs: map[Address]Ref{}} },
	})
	if err != nil {
		return nil, err
	}
	return eventStream{worker}, nil
}

type eventStream struct {
	worker Ref
}

func (s eventStream) Subscribers() ([]Address, error) {
	// TODO(CD): Make timeout configurable somehow.
	subs, err := s.worker.Ask(subscribers{}, 3*time.Second)
	if err != nil {
		return nil, err
	}
	return subs.([]Address), nil
}

func (s eventStream) Subscribe(r Ref) {
	s.worker.Send(nil, subscribe{r})
}

func (s eventStream) Unsubscribe(a Address) {
	s.worker.Send(nil, unsubscribe{a})
}

func (s eventStream) Publish(event interface{}) {
	s.worker.Send(nil, publish{event})
}

func (s eventStream) Shutdown() {
	s.worker.Send(nil, PoisonPill)
}

// Event stream worker messages.
type subscribers struct{}
type subscribe struct{ Ref }
type unsubscribe struct{ Address }
type publish struct{ event interface{} }

// Implements actor.Actor.
type esWorker struct {
	cx   Context
	subs map[Address]Ref
}

func (w *esWorker) Prestart(cx Context) { w.cx = cx }

func (w *esWorker) Receive(rx ReceiveContext, msg interface{}) {
	switch msg.(type) {
	case subscribers:
		reply := []Address{}
		for _, s := range w.subs {
			reply = append(reply, s.Address())
		}
		rx.ReplyTo.Send(w.cx.Self, reply)
	case subscribe:
		s := msg.(subscribe).Ref
		w.subs[s.Address()] = s
	case unsubscribe:
		delete(w.subs, msg.(unsubscribe).Address)
	case publish:
		ev := msg.(publish).event
		for _, s := range w.subs {
			s.Send(w.cx.Self, ev)
		}
	}
}
