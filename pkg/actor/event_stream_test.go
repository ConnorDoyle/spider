// +build small

package actor

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/topology-io/spider/pkg/promise"
)

func TestEventStreamSubscribe(t *testing.T) {
	Convey("An event stream should create", t, func() {
		sys, err := NewSystem("test", SystemConfig{})
		So(err, ShouldBeNil)
		So(sys, ShouldNotBeNil)
		defer sys.Shutdown()

		events, err := NewEventStream(sys, "events")
		So(err, ShouldBeNil)
		So(events, ShouldNotBeNil)

		received := promise.NewPromise()
		var event interface{}

		Convey("An event stream should handle unsubscriptions", func() {
			s1, err := DefaultStatelessActor(sys, "sub-1",
				func(cx Context, rx ReceiveContext, msg interface{}) {
					event = msg
					received.Complete(nil)
				},
			)
			So(err, ShouldBeNil)
			So(s1, ShouldNotBeNil)

			s2, err := DefaultStatelessActor(sys, "sub-2",
				func(cx Context, rx ReceiveContext, msg interface{}) {})
			So(err, ShouldBeNil)
			So(s2, ShouldNotBeNil)

			events.Subscribe(s1)
			events.Subscribe(s2)
			subs, err := events.Subscribers()
			So(err, ShouldBeNil)
			So(len(subs), ShouldEqual, 2)

			Convey("An event stream should handle unsubscriptions", func() {
				events.Unsubscribe(s2.Address())
				subs, err := events.Subscribers()
				So(err, ShouldBeNil)
				So(subs, ShouldResemble, []Address{s1.Address()})
			})

			Convey("An event stream should broadcast published events to subscribers", func() {
				events.Publish(12345)
				received.Await()
				So(event, ShouldEqual, 12345)
			})
		})
	})
}

func TestEventStreamPublish(t *testing.T) {
	Convey("An event stream should broadcast published events to subscribers", t, func() {
	})
}
