// +build small

package actor

import (
	"testing"

	"github.com/nqn/spider/pkg/promise"
	. "github.com/smartystreets/goconvey/convey"
)

var (
	promises           = make(map[Address]promise.Promise)
	lastReceiveContext ReceiveContext
	lastMessage        interface{}
)

// implements actor.Actor
type testActor struct {
	name string
	cx   Context
	t    *testing.T
}

func (a *testActor) Name() string {
	a.t.Logf("actor [%s] in Name", a.name)
	return a.name
}
func (a *testActor) Init(cx Context) {
	a.t.Logf("actor [%s] in Init", cx.Self.Address())
	promises[cx.Self.Address()] = promise.NewPromise()
	a.cx = cx
}
func (a *testActor) Receive(rx ReceiveContext, msg interface{}) {
	a.t.Logf("actor [%s] in receive", a.cx.Self.Address())
	a.t.Logf("received message [%v]", msg)
	lastReceiveContext = rx
	lastMessage = msg
	promises[a.cx.Self.Address()].Complete(nil)
}

func TestActorSystem(t *testing.T) {
	Convey(`An actor system should create new actors,
	the returned refs should have the right address,
	the system should remember creating the actors,
	and the underlying actor should receive messages`,
		t, func() {
			sys, err := NewSystem(SystemConfig{})
			So(err, ShouldBeNil)
			So(sys, ShouldNotBeNil)
			defer sys.Shutdown()

			foo, err := sys.NewActor(Spore{
				DefaultConfig(),
				func() Actor { return &testActor{name: "foo", t: t} },
			})
			So(err, ShouldBeNil)
			So(foo, ShouldNotBeNil)

			bar, err := sys.NewActor(Spore{
				DefaultConfig(),
				func() Actor { return &testActor{name: "bar", t: t} },
			})
			So(err, ShouldBeNil)
			So(bar, ShouldNotBeNil)

			So(foo.Address(), ShouldEqual, Address("spider:///user/foo"))
			So(bar.Address(), ShouldEqual, Address("spider:///user/bar"))

			So(sys.Lookup(Address("spider:///user/foo")), ShouldNotBeNil)
			So(sys.Lookup(Address("spider:///user/bar")), ShouldNotBeNil)

			foo.Send(bar, "ping")
			promises[foo.Address()].Await()
			So(lastReceiveContext.ReplyTo.Address(), ShouldEqual, bar.Address())
			So(lastMessage, ShouldEqual, "ping")

			bar.Send(foo, "pong")
			promises[bar.Address()].Await()
			So(lastReceiveContext.ReplyTo.Address(), ShouldEqual, foo.Address())
			So(lastMessage, ShouldEqual, "pong")
		})
}
