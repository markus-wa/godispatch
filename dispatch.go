// Package dispatch provides a general purpose object dispatcher.
// It can be used to asynchronously dispatch queues (channels)
// or synchronously dispatch single objects.
// For example one could use it to dispatch events or messages.
package dispatch

import (
	"errors"
	"reflect"
	"sync"
)

type queueToken int

const (
	syncToken   queueToken = iota // See Dispatcher.SyncQueues()
	removeToken                   // See Dispatcher.RemoveQueues()
	cancelToken                   // See Dispatcher.CancelQueues()
)

// HandlerIdentifier uniquely identifies a handler
type HandlerIdentifier *int

type queue struct {
	data   chan interface{}
	cancel chan struct{}
}

// Dispatcher is used to register handlers and dispatch objects.
type Dispatcher struct {
	handlerLock    sync.Mutex
	tokenLock      sync.Mutex
	queueLock      sync.Mutex
	tokenWg        sync.WaitGroup
	queues         []queue
	handlers       map[reflect.Type]map[HandlerIdentifier]reflect.Value
	cachedHandlers map[reflect.Type][]reflect.Value
}

// Dispatch dispatches an object to all it's handlers in the order in which the handlers were registered.
func (d *Dispatcher) Dispatch(object interface{}) {
	d.handlerLock.Lock()

	t := reflect.TypeOf(object)
	if d.cachedHandlers[t] == nil {
		d.initCache(t)
	}
	handlers := d.cachedHandlers[t]

	// No defer because we already need to unlock here so handlers can be added inside Call()
	// Should be fine as long as we don't panic before this
	d.handlerLock.Unlock()

	args := []reflect.Value{reflect.ValueOf(object)}
	for i := 0; i < len(handlers); i++ {
		handlers[i].Call(args)
	}
}

// We cache the handlers so we don't have to check the type of each handler group for every object of the same type.
// Performance gained >15%, depending on the amount of handlers.
func (d *Dispatcher) initCache(objectType reflect.Type) {
	// Read from nil map is allowed, so we initialize it only now if it's nil
	if d.cachedHandlers == nil {
		d.cachedHandlers = make(map[reflect.Type][]reflect.Value)
	}

	// Load handlers into cache
	d.cachedHandlers[objectType] = make([]reflect.Value, 0)
	for t := range d.handlers {
		if objectType.AssignableTo(t) {
			handlerList := make([]reflect.Value, len(d.handlers[t]))
			i := 0
			for hk := range d.handlers[t] {
				handlerList[i] = d.handlers[t][hk]
				i++
			}
			d.cachedHandlers[objectType] = append(d.cachedHandlers[objectType], handlerList...)
		}
	}
}

// AddQueues adds channels as 'object-queues'.
// All objects sent to the passed queues will be dispatched to their handlers in a separate go routine per channel.
func (d *Dispatcher) AddQueues(dataChans ...chan interface{}) {
	d.queueLock.Lock()
	defer d.queueLock.Unlock()

	for _, dc := range dataChans {
		q := queue{data: dc, cancel: make(chan struct{}, 1)}
		d.queues = append(d.queues, q)
		go d.dispatchQueue(q)
	}
}

// See AddQueue() & Dispatch()
func (d *Dispatcher) dispatchQueue(q queue) {
	var rem bool

Dispatch:
	for e := range q.data {
		switch e {
		case syncToken:
			d.tokenWg.Done()
		case removeToken:
			rem = true
			break Dispatch
		case cancelToken:
			// Just the trigger for idle queues
			// Don't send this to Dispatch()
			// Actual cancelling is done below
		default:
			d.Dispatch(e)
		}

		// Cancellation
		select {
		case <-q.cancel:
			// Drain the data channel
		Drain:
			for {
				select {
				case e = <-q.data:
					switch e {
					case syncToken:
						d.tokenWg.Done()
					case removeToken:
						rem = true
					}
				default:
					break Drain
				}
			}

			break Dispatch
		default:
		}
	}

	// Remove closed or removed channel from queues
	d.removeQueue(q.data)

	// Only when manually removed, not closed
	if rem {
		d.tokenWg.Done()
	}
}

// RemoveQueues removes the given queues from the Dispatcher without closing them.
func (d *Dispatcher) RemoveQueues(queues ...chan interface{}) error {
	return d.sendToken(queues, removeToken)
}

// RemoveAllQueues removes all queues from the Dispatcher without closing them.
func (d *Dispatcher) RemoveAllQueues() {
	d.RemoveQueues(d.allQueues()...)
}

func (d *Dispatcher) allQueues() []chan interface{} {
	var queues []chan interface{}
	for i := 0; i < len(d.queues); i++ {
		queues = append(queues, d.queues[i].data)
	}
	return queues
}

// See RemoveQueues()
func (d *Dispatcher) removeQueue(q <-chan interface{}) {
	d.queueLock.Lock()
	defer d.queueLock.Unlock()

	// TODO: Seems like this could be done in a nicer way
	// Might have been added multiple times (for whatever reason)
	for r := true; r; {
		r = false
		for i := 0; i < len(d.queues); i++ {
			if d.queues[i].data == q {
				d.queues = append(d.queues[:i], d.queues[i+1:]...)
				r = true
				break // reset i after changing the slice
			}
		}
	}
}

// CancelQueues cancels the dispatching of the provided queues.
// Any data sill in the queue at this point will be ignored.
func (d *Dispatcher) CancelQueues(queues ...chan interface{}) {
	for _, q := range queues {
		for i := 0; i < len(d.queues); i++ {
			if d.queues[i].data == q {
				// Send cancellation signal
				d.queues[i].cancel <- struct{}{}

				// Wake up the queue dispatcher if the queue is empty
				select {
				case d.queues[i].data <- cancelToken:
				default:
				}
			}
		}
	}
}

// CancelAllQueues cancels the dispatching of all registered queues.
// See: CancelQueues()
func (d *Dispatcher) CancelAllQueues() {
	d.CancelQueues(d.allQueues()...)
}

// SyncQueues syncs the channels dispatch routines to the current go routine.
// This ensures all objects received in the passed channels up to this point will be handled before continuing.
func (d *Dispatcher) SyncQueues(queues ...chan interface{}) error {
	// We can't just check the channel length as that does not tell us whether the last object has been fully dispatched
	return d.sendToken(queues, syncToken)
}

// SyncAllQueues calls SyncQueues() for all queues in this Dispatcher.
func (d *Dispatcher) SyncAllQueues() {
	d.SyncQueues(d.allQueues()...)
}

// Sends a token to specific queues in the dispatcher and waits until they were handled.
func (d *Dispatcher) sendToken(queues []chan interface{}, token interface{}) error {
	d.tokenLock.Lock()
	defer d.tokenLock.Unlock()

	var err error

	// Avoid race condition by locking queues until all remove tokens are sent
	// Otherwise the first queue could be removed before the second one is read into dq
	func() {
		d.queueLock.Lock()
		defer d.queueLock.Unlock()
		for _, q := range queues {
			found := false
			for i := 0; i < len(d.queues); i++ {
				if q == d.queues[i].data {
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
	}()
	d.tokenWg.Wait()
	return err
}

// RegisterHandler registers an object handler (func) for the type of objects assignable to it's input parameter type.
// If the handler registers a new handler in the function body, the new handler will only be active for new Dispatch() calls.
// Calling this method clears the internal type/handler mapping cache for interface handlers.
// Returns a unique identifier which can be used to remove the handler via UnregisterHandler().
func (d *Dispatcher) RegisterHandler(handler interface{}) HandlerIdentifier {
	h := reflect.ValueOf(handler)
	ht := h.Type()
	if ht.Kind() != reflect.Func {
		panic("Handler isn't a function")
	}
	if ht.NumIn() != 1 {
		panic("Handler function has more than one input parameter")
	}
	t := ht.In(0)

	d.handlerLock.Lock()
	defer d.handlerLock.Unlock()

	if d.handlers == nil {
		d.handlers = make(map[reflect.Type]map[HandlerIdentifier]reflect.Value)
	}
	if d.handlers[t] == nil {
		d.handlers[t] = make(map[HandlerIdentifier]reflect.Value)
	}
	handlerID := new(int)
	d.handlers[t][handlerID] = h

	if d.cachedHandlers != nil {
		// Reset cache for interface handlers
		if t.Kind() == reflect.Interface {
			d.cachedHandlers = nil
		} else {
			// For anything else we should be fine with adding the handler to the cache
			d.cachedHandlers[t] = append(d.cachedHandlers[t], h)
		}
	}

	return handlerID
}

// UnregisterHandler unregisters a handler by it's identifier (as returned by RegisterHandler()).
// Unregistering is done via identifiers because functions can't be compared in Go.
func (d *Dispatcher) UnregisterHandler(identifier HandlerIdentifier) {
	for t, m := range d.handlers {
		for id := range m {
			if id == identifier {
				delete(m, id)
				if d.cachedHandlers != nil {
					// Reset cache for interface handlers
					if t.Kind() == reflect.Interface {
						d.cachedHandlers = nil
					} else {
						// For anything else we can just clear the cache for that type
						delete(d.cachedHandlers, t)
					}
				}
			}
		}
	}
}
