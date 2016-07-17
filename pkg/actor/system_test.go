// +build small

package actor

import (
	"testing"

	log "github.com/Sirupsen/logrus"

	"github.com/nqn/spider/pkg/promise"
	. "github.com/smartystreets/goconvey/convey"
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

// implements actor.Actor
type probe struct {
	cx      Context
	handler func(Event)
}

func (p *probe) Prestart(cx Context) { p.cx = cx }
func (p *probe) Receive(rx ReceiveContext, msg interface{}) {
	p.handler(msg.(Event))
}

func TestActorSystemBasics(t *testing.T) {
	Convey("An actor system should create new actors", t, func() {
		sys, err := NewSystem("test", SystemConfig{})
		So(err, ShouldBeNil)
		So(sys, ShouldNotBeNil)
		defer sys.Shutdown()

		foo, err := sys.NewActor("foo", Info{
			DefaultConfig(),
			func() Actor { return &testActor{} },
		})
		So(err, ShouldBeNil)
		So(foo, ShouldNotBeNil)

		bar, err := sys.NewActor("bar", Info{
			DefaultConfig(),
			func() Actor { return &testActor{} },
		})
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

					p, err := sys.NewActor("probe", Info{
						DefaultConfig(),
						func() Actor {
							return &probe{
								handler: func(ev Event) {
									d := ev.Data.(SendData)
									sendData = append(sendData, d)
									switch d.Message.(type) {
									case Stopped:
										// Complete the probe's half of the rendez-vous
										rv.A()
									}
								},
							}
						},
					})

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
