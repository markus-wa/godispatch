// Package dispatch provides a general purpose object dispatcher.
// It can be used to asynchronously dispatch queues (channels)
// or synchronously dispatch single objects.
// For example one could use it to dispatch events or messages.
package dispatch

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
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
	handlers       map[reflect.Type]map[HandlerIdentifier]registeredHandler
	cachedHandlers map[reflect.Type][]handler
	nextSeq        uint64 // registration sequence number for the next handler, guarded by handlerLock
	panicHandler   PanicHandler
}

// handler is a registered handler func together with its per-handler options.
type handler struct {
	fn              reflect.Value
	unwrappedPanics bool
}

// registeredHandler is a handler together with its global registration sequence number.
// The sequence number is only needed to build the cache in registration order,
// so it is kept out of handler to keep the cached handler list (iterated on every Dispatch) compact.
type registeredHandler struct {
	handler
	seq uint64
}

// handlersBySeq sorts handlers and their sequence numbers by sequence number.
type handlersBySeq struct {
	handlers []handler
	seqs     []uint64
}

func (s handlersBySeq) Len() int {
	return len(s.handlers)
}

func (s handlersBySeq) Less(i, j int) bool {
	return s.seqs[i] < s.seqs[j]
}

func (s handlersBySeq) Swap(i, j int) {
	s.handlers[i], s.handlers[j] = s.handlers[j], s.handlers[i]
	s.seqs[i], s.seqs[j] = s.seqs[j], s.seqs[i]
}

// HandlerOptions configures a handler registered via RegisterHandlerWithOptions.
// The zero value gives the same behaviour as RegisterHandler.
type HandlerOptions struct {
	// UnwrappedPanics makes panics from this handler propagate as-is instead of as ConsumerCodePanic.
	UnwrappedPanics bool
}

// Config contains configuration parameters for a Dispatcher.
type Config struct {
	// PanicHandler handles panics in goroutines created by Dispatcher.AddQueues().
	// It receives the recovered value: a ConsumerCodePanic for handlers registered via RegisterHandler,
	// or the raw panic value for handlers registered with HandlerOptions.UnwrappedPanics.
	// If it is nil, panics are not recovered and crash the program.
	PanicHandler PanicHandler
}

// NewDispatcherWithConfig creates a new dispatcher with a given config and returns it.
func NewDispatcherWithConfig(cfg Config) *Dispatcher {
	return &Dispatcher{
		panicHandler: cfg.PanicHandler,
	}
}

/*
ConsumerCodePanic is the type which identifies panics in consumer code (e.g. RegisterHandler).
Panics from handlers registered with HandlerOptions.UnwrappedPanics are not wrapped and propagate as-is.

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
// If a handler panics, the remaining handlers are not called and the panic propagates to the caller,
// wrapped in a ConsumerCodePanic unless the handler was registered with HandlerOptions.UnwrappedPanics.
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
		if h.unwrappedPanics {
			h.fn.Call(args)
		} else {
			callConsumerCode(h.fn, args)
		}
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
		d.cachedHandlers = make(map[reflect.Type][]handler)
	}

	n := 0

	for t, m := range d.handlers {
		if objectType.AssignableTo(t) {
			n += len(m)
		}
	}

	// Load handlers into cache
	sorted := handlersBySeq{
		handlers: make([]handler, 0, n),
		seqs:     make([]uint64, 0, n),
	}

	for t, m := range d.handlers {
		if objectType.AssignableTo(t) {
			for _, h := range m {
				sorted.handlers = append(sorted.handlers, h.handler)
				sorted.seqs = append(sorted.seqs, h.seq)
			}
		}
	}

	// Map iteration order is random, restore the global registration order.
	sort.Sort(sorted)

	d.cachedHandlers[objectType] = sorted.handlers
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
// Panics from the handler are wrapped in a ConsumerCodePanic, see RegisterHandlerWithOptions to change this.
func (d *Dispatcher) RegisterHandler(handler interface{}) HandlerIdentifier {
	return d.RegisterHandlerWithOptions(handler, HandlerOptions{})
}

// RegisterHandlerWithOptions is like RegisterHandler but allows configuring the handler via HandlerOptions.
// The returned identifier can be used with UnregisterHandler() like any other.
//
// With HandlerOptions.UnwrappedPanics set, a panic inside the handler propagates out of Dispatch() with its
// original value instead of being wrapped in a ConsumerCodePanic; handlers after it are not called.
// For objects dispatched via queues (see AddQueues()) the raw value is passed to Config.PanicHandler,
// or crashes the program if no PanicHandler is set (same as for wrapped panics).
func (d *Dispatcher) RegisterHandlerWithOptions(handlerFunc interface{}, opts HandlerOptions) HandlerIdentifier {
	fn := reflect.ValueOf(handlerFunc)
	ht := fn.Type()

	if ht.Kind() != reflect.Func {
		panic("Handler isn't a function")
	} else if ht.NumIn() != 1 {
		panic("Handler function has more than one input parameter")
	}

	t := ht.In(0)

	d.handlerLock.Lock()
	defer d.handlerLock.Unlock()

	if d.handlers == nil {
		d.handlers = make(map[reflect.Type]map[HandlerIdentifier]registeredHandler)
	}

	if d.handlers[t] == nil {
		d.handlers[t] = make(map[HandlerIdentifier]registeredHandler)
	}

	h := handler{
		fn:              fn,
		unwrappedPanics: opts.UnwrappedPanics,
	}

	handlerID := new(int)
	d.handlers[t][handlerID] = registeredHandler{handler: h, seq: d.nextSeq}
	d.nextSeq++

	if d.cachedHandlers != nil {
		if t.Kind() == reflect.Interface {
			// Reset cache for interface handlers, they may match any number of cached object types.
			d.cachedHandlers = nil
		} else {
			// The new handler has the highest sequence number, so appending it to every already cached
			// object type it applies to keeps the registration order.
			// Types without a cache entry are left alone and built lazily by initCache(),
			// appending to them here would create an entry that lacks the previously registered handlers.
			// Appending never modifies elements visible to in-flight Dispatch() calls, which hold a shorter slice.
			for objectType, cached := range d.cachedHandlers {
				// objectType is nil for Dispatch(nil)
				if objectType != nil && objectType.AssignableTo(t) {
					d.cachedHandlers[objectType] = append(cached, h)
				}
			}
		}
	}

	return handlerID
}

// UnregisterHandler unregisters a handler by it's identifier (as returned by RegisterHandler() or RegisterHandlerWithOptions()).
// Unregistering is done via identifiers because functions can't be compared in Go.
// It may be called from inside a handler; handlers of an in-progress Dispatch() call are not affected,
// the change applies to subsequent Dispatch() calls.
func (d *Dispatcher) UnregisterHandler(identifier HandlerIdentifier) {
	d.handlerLock.Lock()
	defer d.handlerLock.Unlock()

	for t, m := range d.handlers {
		if _, ok := m[identifier]; !ok {
			continue
		}

		delete(m, identifier)

		if d.cachedHandlers != nil {
			if t.Kind() == reflect.Interface {
				// Reset cache for interface handlers
				d.cachedHandlers = nil
			} else {
				// For anything else we can just clear the cache for the object types the handler applies to
				for objectType := range d.cachedHandlers {
					// objectType is nil for Dispatch(nil)
					if objectType != nil && objectType.AssignableTo(t) {
						delete(d.cachedHandlers, objectType)
					}
				}
			}
		}

		return
	}
}

// UnregisterAllHandlers removes all handlers previously registered via RegisterHandler or RegisterHandlerWithOptions.
func (d *Dispatcher) UnregisterAllHandlers() {
	d.handlerLock.Lock()
	defer d.handlerLock.Unlock()

	d.handlers = nil
	d.cachedHandlers = nil
}
