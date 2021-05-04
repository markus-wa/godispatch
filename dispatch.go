// Package dispatch provides a general purpose object dispatcher.
// It can be used to asynchronously dispatch queues (channels)
// or synchronously dispatch single objects.
// For example one could use it to dispatch events or messages.
package dispatch

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

const (
	syncToken   = iota // See Dispatcher.SyncQueues()
	removeToken        // See Dispatcher.RemoveQueues()
)

// HandlerIdentifier uniquely identifies a handler
type HandlerIdentifier *int

// PanicHandler is a function that gets called when a panic occurs during dispatching.
type PanicHandler func(v interface{})

// Dispatcher is used to register handlers and dispatch objects.
type Dispatcher struct {
	handlerLock    sync.Mutex
	tokenLock      sync.Mutex
	queueLock      sync.Mutex
	tokenWg        sync.WaitGroup
	queues         []chan interface{}
	handlers       map[reflect.Type]map[HandlerIdentifier]reflect.Value
	cachedHandlers map[reflect.Type][]reflect.Value
	panicHandler   PanicHandler
}

// Config contains configuration parameters for a Dispatcher.
type Config struct {
	PanicHandler PanicHandler // A function that handles panics in goroutines created by Dispatcher.AddQueues()
}

// NewDispatcherWithConfig creates a new dispatcher with a given config and returns it.
func NewDispatcherWithConfig(cfg Config) *Dispatcher {
	return &Dispatcher{
		panicHandler: cfg.PanicHandler,
	}
}

/*
ConsumerCodePanic is the type which identifies panics in consumer code (e.g. RegisterHandler).

Example:

	d := dp.Dispatcher{}
	d.RegisterHandler(func(a *A) {
		fmt.Println(a.val)
	})

	var err interface{}
	func() {
		defer func() {
			err = recover()
		}()

		var a *A
		d.Dispatch(a)
	}()

	switch err.(type) {
	case dp.ConsumerCodePanic:
		fmt.Println("something went wrong inside consumer code")
	default:
		fmt.Println("something went wrong in the library")
	}
*/
type ConsumerCodePanic interface {
	String() string
	Value() interface{}
}

type consumerCodePanic struct {
	value interface{}
}

func (ucp consumerCodePanic) String() string {
	return fmt.Sprint(ucp.value)
}

func (ucp consumerCodePanic) Value() interface{} {
	return ucp.value
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

	for _, h := range handlers {
		callConsumerCode(h, args)
	}
}

func callConsumerCode(h reflect.Value, args []reflect.Value) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}

		panic(consumerCodePanic{value: r})
	}()

	h.Call(args)
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

			for _, h := range d.handlers[t] {
				handlerList[i] = h
				i++
			}

			d.cachedHandlers[objectType] = append(d.cachedHandlers[objectType], handlerList...)
		}
	}
}

// AddQueues adds channels as 'object-queues'.
// All objects sent to the passed queues will be dispatched to their handlers in a separate go routine per channel.
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
			d.dispatchWithRecover(e)
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

func (d *Dispatcher) dispatchWithRecover(object interface{}) {
	defer func() {
		if d.panicHandler != nil {
			if r := recover(); r != nil {
				d.panicHandler(r)
			}
		}
	}()

	d.Dispatch(object)
}

// RemoveQueues removes the given queues from the Dispatcher without closing them.
func (d *Dispatcher) RemoveQueues(queues ...chan interface{}) error {
	return d.sendToken(queues, removeToken)
}

// RemoveAllQueues removes all queues from the Dispatcher without closing them.
func (d *Dispatcher) RemoveAllQueues() {
	d.RemoveQueues(d.queues...)
}

// See RemoveQueues()
func (d *Dispatcher) removeQueue(q <-chan interface{}) {
	d.queueLock.Lock()
	defer d.queueLock.Unlock()

	// TODO: Seems like this could be done in a nicer way
	// Might have been added multiple times (for whatever reason)
	for r := true; r; {
		r = false

		for i := range d.queues {
			if d.queues[i] == q {
				d.queues = append(d.queues[:i], d.queues[i+1:]...)
				r = true

				break // reset i after changing the slice
			}
		}
	}
}

// SyncQueues syncs the channels dispatch routines to the current go routine.
// This ensures all objects received in the passed channels up to this point will be handled before continuing.
func (d *Dispatcher) SyncQueues(queues ...chan interface{}) error {
	// We can't just check the channel length as that does not tell us whether the last object has been fully dispatched
	return d.sendToken(queues, syncToken)
}

// SyncAllQueues calls SyncQueues() for all queues in this Dispatcher.
func (d *Dispatcher) SyncAllQueues() {
	d.SyncQueues(d.queues...)
}

var ErrQueueNotFound = errors.New("one or more queues not found")

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

			for _, dq := range d.queues {
				if q == dq {
					// Using sync.Cond would be a race against dispatchQueue
					d.tokenWg.Add(1)
					q <- token

					found = true
				}
			}

			if !found {
				err = ErrQueueNotFound
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
	} else if ht.NumIn() != 1 {
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

// UnregisterAllHandlers removes all handlers previously registered via RegisterHandler.
func (d *Dispatcher) UnregisterAllHandlers() {
	d.handlerLock.Lock()
	defer d.handlerLock.Unlock()

	d.handlers = nil
	d.cachedHandlers = nil
}
