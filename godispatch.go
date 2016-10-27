// Package godispatch provides a general purpose object dispatcher
// It can be used to asynchronously dispatch queues (channels)
// or synchronously dispatch single objects
// For example one could use it to dispatch events or messages
package godispatch

import (
	"reflect"
	"sync"
)

// Handler handles an object
type Handler func(interface{})

// See Dispatcher.SyncQueue()
var syncToken *struct{} = &struct{}{}

// Dispatcher is used to register Handlers and dispatch objects
type Dispatcher struct {
	handlerLock    sync.RWMutex
	syncLock       sync.Mutex
	syncWg         sync.WaitGroup
	handlers       map[reflect.Type][]Handler
	cachedHandlers map[reflect.Type][]Handler
}

// Dispatch dispatches an object to all it's handlers in the order in which the handlers were registered
func (d *Dispatcher) Dispatch(object interface{}) {
	d.handlerLock.RLock()
	defer d.handlerLock.RUnlock()

	t := reflect.TypeOf(object)

	if d.cachedHandlers[t] == nil {
		// We'll need a write lock inside
		d.handlerLock.RUnlock()
		d.initCache(t)
		d.handlerLock.RLock()
	}

	for _, h := range d.cachedHandlers[t] {
		if h != nil {
			h(object)
		}
	}
}

// We cache the handlers so we don't have to check the type of each handler group for every object of the same type
// Performance gained >15%, depending on the amount of handlers
func (d *Dispatcher) initCache(handlerType reflect.Type) {
	d.handlerLock.Lock()
	defer d.handlerLock.Unlock()

	// Read from nil map is allowed, so we initialize it only now if it's nil
	if d.cachedHandlers == nil {
		d.cachedHandlers = make(map[reflect.Type][]Handler)
	}

	// Load handlers into cache
	d.cachedHandlers[handlerType] = make([]Handler, 0)
	for k := range d.handlers {
		if handlerType.AssignableTo(k) {
			d.cachedHandlers[handlerType] = append(d.cachedHandlers[handlerType], d.handlers[k]...)
		}
	}
}

func (d *Dispatcher) dispatchQueue(q <-chan interface{}) {
	for e := range q {
		if e == syncToken {
			d.syncWg.Done()
			continue
		}
		d.Dispatch(e)
	}
}

// AddQueue adds a channel as 'object-queue'
// All objects sent to the passed channel will be dispatched in a seperate go routine
func (d *Dispatcher) AddQueue(q <-chan interface{}) {
	go d.dispatchQueue(q)
}

// SyncQueues syncs the channels dispatch routines to the current go routine
// This ensures all objects received in the passed channel up to this point will be handled before continuing
func (d *Dispatcher) SyncQueues(queues ...chan<- interface{}) {
	for _, q := range queues {
		d.syncQueue(q)
	}
}

// See SyncQueues
func (d *Dispatcher) syncQueue(queue chan<- interface{}) {
	// We can not check the channel length as that does not tell us whether the last object has been fully dispatched
	d.syncLock.Lock()
	defer d.syncLock.Unlock()
	// Using sync.Cond would be a race against dispatchQueue
	d.syncWg.Add(1)
	queue <- syncToken
	d.syncWg.Wait()
}

// RegisterHandler registers an object handler for a type of objects
// If an object's type is assignable to objectType (reflect.Type.assignableTo), the handler will receive the object
func (d *Dispatcher) RegisterHandler(objectType reflect.Type, handler Handler) {
	d.handlerLock.Lock()
	defer d.handlerLock.Unlock()

	if d.handlers == nil {
		d.handlers = make(map[reflect.Type][]Handler)
	}
	d.handlers[objectType] = append(d.handlers[objectType], handler)

	// Add handler to cache if already initialized
	for k := range d.cachedHandlers {
		if objectType.AssignableTo(k) {
			d.cachedHandlers[k] = append(d.cachedHandlers[k], handler)
		}
	}
}
