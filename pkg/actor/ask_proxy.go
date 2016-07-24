package actor

import (
	"github.com/nqn/spider/pkg/promise"
)

// askProxy is a proxy actor for non-actor code that wants to block waiting
// for the intended message recipient to reply.
// Implements actor.Actor.
type askProxy struct {
	cx     Context
	reply  promise.Promise
	answer *interface{}
}

func newAskProxy(reply promise.Promise, answer *interface{}) *askProxy {
	return &askProxy{
		reply:  reply,
		answer: answer,
	}
}

func (p *askProxy) Prestart(cx Context) { p.cx = cx }
func (p *askProxy) Receive(rx ReceiveContext, msg interface{}) {
	*(p.answer) = msg
	p.reply.Complete(nil)
	p.cx.Self.Send(nil, PoisonPill)
}
