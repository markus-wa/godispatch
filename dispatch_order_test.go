package dispatch_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	dp "github.com/markus-wa/godispatch"
)

const orderIterations = 100

func seq(n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = i
	}

	return s
}

// Handlers of the same type registered before the first Dispatch must be called in registration order.
func TestDispatch_HandlerOrder_SameType(t *testing.T) {
	t.Parallel()

	const n = 20

	for it := 0; it < orderIterations; it++ {
		d := dp.Dispatcher{}

		var calls []int

		for i := 0; i < n; i++ {
			i := i
			d.RegisterHandler(func(Event) {
				calls = append(calls, i)
			})
		}

		d.Dispatch(Event{})

		if !assert.Equal(t, seq(n), calls, "iteration %d", it) {
			return
		}
	}
}

// Interface and concrete handlers matching the same object must be called in global registration order.
func TestDispatch_HandlerOrder_MixedInterfaceAndConcrete(t *testing.T) {
	t.Parallel()

	const n = 30

	for it := 0; it < orderIterations; it++ {
		d := dp.Dispatcher{}

		var calls []int

		for i := 0; i < n; i++ {
			i := i

			switch i % 3 {
			case 0:
				d.RegisterHandler(func(A) { calls = append(calls, i) })
			case 1:
				d.RegisterHandler(func(AB) { calls = append(calls, i) })
			default:
				d.RegisterHandler(func(interface{}) { calls = append(calls, i) })
			}
		}

		d.Dispatch(A{})

		if !assert.Equal(t, seq(n), calls, "iteration %d", it) {
			return
		}
	}
}

// Registering a concrete handler after the cache map exists but before the type was dispatched
// must not drop the previously registered handlers of that type.
func TestDispatch_RegisterAfterCacheCreatedForOtherType(t *testing.T) {
	t.Parallel()

	d := dp.Dispatcher{}

	var calls []string

	d.RegisterHandler(func(int) { calls = append(calls, "first") })

	d.Dispatch("x") // creates the cache map, but no entry for int

	d.RegisterHandler(func(int) { calls = append(calls, "second") })

	d.Dispatch(1)

	assert.Equal(t, []string{"first", "second"}, calls)
}

// Handlers registered after a type was already dispatched (cached) must be called after the existing ones.
func TestDispatch_RegisterAfterDispatch(t *testing.T) {
	t.Parallel()

	for it := 0; it < orderIterations; it++ {
		d := dp.Dispatcher{}

		var calls []string

		d.RegisterHandler(func(A) { calls = append(calls, "A1") })
		d.RegisterHandler(func(AB) { calls = append(calls, "AB1") })
		d.RegisterHandler(func(A) { calls = append(calls, "A2") })

		d.Dispatch(A{})
		assert.Equal(t, []string{"A1", "AB1", "A2"}, calls)

		// concrete handler after the cache for A contains interface handlers
		calls = nil
		d.RegisterHandler(func(A) { calls = append(calls, "A3") })
		d.Dispatch(A{})
		assert.Equal(t, []string{"A1", "AB1", "A2", "A3"}, calls)

		// interface handler invalidates the cache
		calls = nil
		d.RegisterHandler(func(AB) { calls = append(calls, "AB2") })
		d.Dispatch(A{})
		assert.Equal(t, []string{"A1", "AB1", "A2", "A3", "AB2"}, calls)

		// and another concrete one after the cache has been rebuilt
		calls = nil
		d.RegisterHandler(func(A) { calls = append(calls, "A4") })
		d.Dispatch(A{})

		if !assert.Equal(t, []string{"A1", "AB1", "A2", "A3", "AB2", "A4"}, calls, "iteration %d", it) {
			return
		}
	}
}

// Unregistering keeps the order of the remaining handlers, including after a cache rebuild.
func TestDispatch_UnregisterKeepsOrder(t *testing.T) {
	t.Parallel()

	d := dp.Dispatcher{}

	var calls []int

	ids := make([]dp.HandlerIdentifier, 10)

	for i := range ids {
		i := i
		if i%2 == 0 {
			ids[i] = d.RegisterHandler(func(A) { calls = append(calls, i) })
		} else {
			ids[i] = d.RegisterHandler(func(AB) { calls = append(calls, i) })
		}
	}

	d.Dispatch(A{})
	assert.Equal(t, seq(10), calls)

	d.UnregisterHandler(ids[2]) // concrete
	d.UnregisterHandler(ids[5]) // interface

	calls = nil
	d.Dispatch(A{})
	assert.Equal(t, []int{0, 1, 3, 4, 6, 7, 8, 9}, calls)
}

// A handler may unregister itself (or others) while being dispatched without deadlocking.
// The in-progress Dispatch is unaffected, subsequent ones are.
func TestUnregisterHandler_InsideHandler(t *testing.T) {
	t.Parallel()

	d := dp.Dispatcher{}

	var (
		calls  []string
		selfID dp.HandlerIdentifier
	)

	selfID = d.RegisterHandler(func(string) {
		calls = append(calls, "self")
		d.UnregisterHandler(selfID)
	})
	d.RegisterHandler(func(string) {
		calls = append(calls, "other")
	})

	d.Dispatch("first")
	assert.Equal(t, []string{"self", "other"}, calls)

	calls = nil
	d.Dispatch("second")
	assert.Equal(t, []string{"other"}, calls)
}

// UnregisterHandler must be safe to call concurrently with Dispatch and RegisterHandler (run with -race).
func TestUnregisterHandler_Concurrent(t *testing.T) {
	t.Parallel()

	d := dp.Dispatcher{}

	done := make(chan struct{})

	go func() {
		defer close(done)

		for i := 0; i < 200; i++ {
			id := d.RegisterHandler(func(int) {})
			d.UnregisterHandler(id)
		}
	}()

	for i := 0; i < 200; i++ {
		d.Dispatch(2)
		d.Dispatch(A{})
	}

	<-done
}

// Dispatch(nil) without handlers leaves a cache entry for the nil type, registering afterwards must not panic.
func TestDispatch_NilThenRegister(t *testing.T) {
	t.Parallel()

	d := dp.Dispatcher{}
	d.Dispatch(nil)

	called := false
	id := d.RegisterHandler(func(int) { called = true })
	d.Dispatch(1)
	d.UnregisterHandler(id)

	assert.True(t, called)
}
