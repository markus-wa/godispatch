// Package godispatch provides a general purpose object dispatcher
// It can be used to asynchronously dispatch queues (channels)
// or synchronously dispatch single objects
// For example one could use it to dispatch events or messages
package godispatch

import (
	"errors"
	"reflect"
	"sync"
)

const (
	syncToken   = iota // See Dispatcher.SyncQueues()
	removeToken        // See Dispatcher.RemoveQueues()
)

// Handler handles an object
type Handler func(interface{})

// Dispatcher is used to register Handlers and dispatch objects
type Dispatcher struct {
	handlerLock    sync.Mutex
	tokenLock      sync.Mutex
	queueLock      sync.Mutex
	tokenWg        sync.WaitGroup
	queues         []chan interface{}
	handlers       map[reflect.Type][]Handler
	cachedHandlers map[reflect.Type][]Handler
}

// Dispatch dispatches an object to all it's handlers in the order in which the handlers were registered
func (d *Dispatcher) Dispatch(object interface{}) {
	d.handlerLock.Lock()
	defer d.handlerLock.Unlock()

	t := reflect.TypeOf(object)

	if d.cachedHandlers[t] == nil {
		d.initCache(t)
	}

	for _, h := range d.cachedHandlers[t] {
		if h != nil {
			h(object)
		}
	}
}

// We cache the handlers so we don't have to check the type of each handler group for every object of the same type
// Performance gained >15%, depending on the amount of handlers
func (d *Dispatcher) initCache(objectType reflect.Type) {
	// Read from nil map is allowed, so we initialize it only now if it's nil
	if d.cachedHandlers == nil {
		d.cachedHandlers = make(map[reflect.Type][]Handler)
	}

	// Load handlers into cache
	d.cachedHandlers[objectType] = make([]Handler, 0)
	for k := range d.handlers {
		if objectType.AssignableTo(k) {
			d.cachedHandlers[objectType] = append(d.cachedHandlers[objectType], d.handlers[k]...)
		}
	}
}

// AddQueue adds a channel as 'object-queue'
// All objects sent to the passed channel will be dispatched in a seperate go routine
func (d *Dispatcher) AddQueues(queues ...chan interface{}) {
	d.queueLock.Lock()
	defer d.queueLock.Unlock()

	d.queues = append(d.queues, queues...)
	for _, q := range queues {
		go d.dispatchQueue(q)
	}
}

// See AddQueue() & Dispatch()
func (d *Dispatcher) dispatchQueue(q <-chan interface{}) {
	var rem bool
	for e := range q {
		switch e {
		case syncToken:
			d.tokenWg.Done()
		case removeToken:
			rem = true
		default:
			d.Dispatch(e)
		}
		if rem {
			break
		}
	}
	// Remove closed or removed channel from queues
	d.removeQueue(q)

	// Only when manually removed, not closed
	if rem {
		d.tokenWg.Done()
	}
}

// RemoveQueues removes the given queues from the Dispatcher without closing them
func (d *Dispatcher) RemoveQueues(queues ...chan interface{}) error {
	return d.sendToken(queues, removeToken)
}

// RemoveAllQueues removes all queues from the Dispatcher without closing them
func (d *Dispatcher) RemoveAllQueues() {
	d.RemoveQueues(d.queues...)
}

// See RemoveQueues()
func (d *Dispatcher) removeQueue(q <-chan interface{}) {
	d.queueLock.Lock()
	defer d.queueLock.Unlock()

	// Might have been added multiple times (for whatever reason)
	for r := true; r; {
		r = false
		for i, _ := range d.queues {
			if d.queues[i] == q {
				d.queues = append(d.queues[:i], d.queues[i+1:]...)
				r = true
				break
			}
		}
	}
}

// SyncQueues syncs the channels dispatch routines to the current go routine
// This ensures all objects received in the passed channel up to this point will be handled before continuing
func (d *Dispatcher) SyncQueues(queues ...chan interface{}) error {
	// We can't just check the channel length as that does not tell us whether the last object has been fully dispatched
	return d.sendToken(queues, syncToken)
}

// SyncAllQueues calls SyncQueues for all queues in this Dispatcher
func (d *Dispatcher) SyncAllQueues() {
	d.SyncQueues(d.queues...)
}

// Sends a token to specific queues in the dispatcher and waits until they were handled
func (d *Dispatcher) sendToken(queues []chan interface{}, token interface{}) error {
	d.tokenLock.Lock()
	defer d.tokenLock.Unlock()

	var err error
	for _, q := range queues {
		found := false
		for _, dq := range d.queues {
			if q == dq {
				// Using sync.Cond would be a race against dispatchQueue
				d.tokenWg.Add(1)
				q <- token
				found = true
			}
		}
		if !found {
			err = errors.New("One or more queues not found")
		}
	}
	d.tokenWg.Wait()
	return err
}

// RegisterHandler registers an object handler for a type of objects
// If an object's type is assignable to objectType (reflect.Type.assignableTo), the handler will receive the object
// Calling this method clears the internal type/handler mapping cache for interface types
func (d *Dispatcher) RegisterHandler(objectType reflect.Type, handler Handler) {
	d.handlerLock.Lock()
	defer d.handlerLock.Unlock()

	if d.handlers == nil {
		d.handlers = make(map[reflect.Type][]Handler)
	}
	d.handlers[objectType] = append(d.handlers[objectType], handler)

	if d.cachedHandlers != nil {
		// Reset cache for interface handlers
		if objectType.Kind() == reflect.Interface {
			d.cachedHandlers = nil
		} else {
			// For anything else we should be fine with adding the handler to the cache
			d.cachedHandlers[objectType] = append(d.cachedHandlers[objectType], handler)
		}
	}
}
