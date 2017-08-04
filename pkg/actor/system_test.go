// +build small

package actor

import (
	"fmt"
	"sync"
	"testing"
	"time"

	log "github.com/Sirupsen/logrus"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/topology-io/spider/pkg/promise"
)

var (
	promises           = make(map[Address]promise.Promise)
	lastReceiveContext ReceiveContext
	lastMessage        interface{}
)

func init() {
	log.SetLevel(log.WarnLevel)
}

// implements actor.Actor
type testActor struct{ cx Context }

func (a *testActor) Prestart(cx Context) {
	promises[cx.Self.Address()] = promise.NewPromise()
	a.cx = cx
}
func (a *testActor) Receive(rx ReceiveContext, msg interface{}) {
	lastReceiveContext = rx
	lastMessage = msg
	promises[a.cx.Self.Address()].Complete(nil)
}

func DefaultStatelessActor(sys System,
	name string,
	handler func(Context, ReceiveContext, interface{})) (Ref, error) {
	return sys.NewActor(name, Info{
		DefaultConfig(),
		func() Actor { return &statelessActor{handler: handler} },
	})
}

// implements actor.Actor
type statelessActor struct {
	cx      Context
	handler func(Context, ReceiveContext, interface{})
}

func (a *statelessActor) Prestart(cx Context) { a.cx = cx }
func (a *statelessActor) Receive(rx ReceiveContext, msg interface{}) {
	a.handler(a.cx, rx, msg)
}

func TestActorSystemBasics(t *testing.T) {
	Convey("An actor system should create new actors", t, func() {
		sys, err := NewSystem("test", SystemConfig{})
		So(err, ShouldBeNil)
		So(sys, ShouldNotBeNil)
		defer sys.Shutdown()

		foo, err := sys.NewDefaultActor("foo", func() Actor { return &testActor{} })
		So(err, ShouldBeNil)
		So(foo, ShouldNotBeNil)

		bar, err := sys.NewDefaultActor("bar", func() Actor { return &testActor{} })
		So(err, ShouldBeNil)
		So(bar, ShouldNotBeNil)

		Convey("and the returned refs should have the right address", func() {
			So(foo.Address(), ShouldEqual, Address("spider:///test/user/foo"))
			So(bar.Address(), ShouldEqual, Address("spider:///test/user/bar"))

			Convey("and the system should remember creating the actors", func() {
				So(sys.Lookup(Address("spider:///test/user/foo")), ShouldNotBeNil)
				So(sys.Lookup(Address("spider:///test/user/bar")), ShouldNotBeNil)

				Convey("and the underlying actor should receive messages", func() {
					foo.Send(bar, "ping")
					promises[foo.Address()].Await()
					So(lastReceiveContext.ReplyTo.Address(), ShouldEqual, bar.Address())
					So(lastMessage, ShouldEqual, "ping")

					bar.Send(foo, "pong")
					promises[bar.Address()].Await()
					So(lastReceiveContext.ReplyTo.Address(), ShouldEqual, foo.Address())
					So(lastMessage, ShouldEqual, "pong")
				})

				Convey("and death watch should work", func() {
					rv := promise.NewRendezVous()
					// The probe handler closes over this identifier.
					sendData := []SendData{}

					p, err := DefaultStatelessActor(sys, "probe",
						func(_ Context, _ ReceiveContext, msg interface{}) {
							d := msg.(Event).Data.(SendData)
							sendData = append(sendData, d)
							switch d.Message.(type) {
							case Stopped:
								// Complete the probe's half of the rendez-vous
								rv.A()
							}
						},
					)

					So(err, ShouldBeNil)
					So(p, ShouldNotBeNil)

					sys.AddProbe(p)

					foo.AddWatcher(bar)
					foo.Send(nil, PoisonPill)

					// Complete the test thread half of the rendez-vous
					rv.B()

					So(len(sendData), ShouldEqual, 2)
					So(sendData[0].Message, ShouldEqual, PoisonPill)
					So(sendData[0].Recipient.Address(), ShouldEqual, foo.Address())
					So(sendData[0].ReplyTo, ShouldBeNil)

					So(sendData[1].Message, ShouldResemble, Stopped{foo.Address()})
					So(sendData[1].Recipient.Address(), ShouldEqual, bar.Address())
					So(sendData[1].ReplyTo, ShouldBeNil)

					Convey("and shutdown should stop remaining actors", func() {
						So(sys.State(), ShouldEqual, SystemRunning)
						sys.Shutdown()
						bar.Life().Await()
						So(bar.Life().IsComplete(), ShouldBeTrue)
						So(sys.State(), ShouldEqual, SystemStopped)
					})

					Convey("and graceful shutdown should stop remaining actors", func() {
						So(sys.State(), ShouldEqual, SystemRunning)
						sys.GracefulShutdown()
						bar.Life().Await()
						So(bar.Life().IsComplete(), ShouldBeTrue)
						So(sys.State(), ShouldEqual, SystemStopped)
					})
				})
			})
		})
	})
}

func TestActorSystemVerticalScaling(t *testing.T) {
	Convey("An actor system should scale vertically", t, func() {
		sys, err := NewSystem("test", SystemConfig{})
		So(err, ShouldBeNil)
		So(sys, ShouldNotBeNil)
		defer sys.GracefulShutdown()

		numActors := 10000
		var wg sync.WaitGroup
		wg.Add(numActors)

		// Create lots of actors.
		for i := 0; i < numActors; i++ {
			a, err := DefaultStatelessActor(sys, fmt.Sprintf("a-%d", i),
				func(cx Context, _ ReceiveContext, msg interface{}) {
					wg.Done()
					cx.Self.Send(cx.Self, PoisonPill)
				},
			)
			So(err, ShouldBeNil)
			So(a, ShouldNotBeNil)
		}

		// Send each one a message.
		for i := 0; i < numActors; i++ {
			r := sys.Lookup(Address(fmt.Sprintf("spider:///test/user/a-%d", i)))
			So(r, ShouldNotBeNil)
			r.Send(nil, "xyzzy")
		}

		// Wait for all of the actors to receive and signal.
		wg.Wait()
	})
}

func TestActorSystemAsk(t *testing.T) {
	Convey("An actor system should implement ask", t, func() {
		sys, err := NewSystem("test", SystemConfig{})
		So(err, ShouldBeNil)
		So(sys, ShouldNotBeNil)
		defer sys.Shutdown()

		// Create an actor.
		adder, err := DefaultStatelessActor(sys, "adder",
			func(cx Context, rx ReceiveContext, msg interface{}) {
				switch msg.(type) {
				case int:
					// Reply with the next larger integer value.
					rx.ReplyTo.Send(cx.Self, msg.(int)+1)
				}
			},
		)
		So(err, ShouldBeNil)
		So(adder, ShouldNotBeNil)

		// Send a message and await reply using Actor.Ask.
		answer, err := adder.Ask(5, time.Minute)
		So(err, ShouldBeNil)
		So(answer, ShouldEqual, 6)
	})
}
