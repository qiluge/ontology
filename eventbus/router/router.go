package router

import "github.com/Ontology/eventbus/actor"

// A type that satisfies router.Interface can be used as a router
type Interface interface {
	RouteMessage(message interface{})
	SetRoutees(routees *actor.PIDSet)
	GetRoutees() *actor.PIDSet
}