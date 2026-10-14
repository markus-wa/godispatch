package dispatch_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dp "github.com/markus-wa/godispatch"
)

var errRaw = errors.New("raw panic value")

func TestRegisterHandlerWithOptions_UnwrappedPanicPropagatesRawValue(t *testing.T) {
	t.Parallel()

	d := dp.Dispatcher{}
	d.RegisterHandlerWithOptions(func(string) {
		panic(errRaw)
	}, dp.HandlerOptions{UnwrappedPanics: true})

	assert.PanicsWithValue(t, errRaw, func() {
		d.Dispatch("txt")
	})
}

func TestRegisterHandlerWithOptions_UnwrappedPanicSkipsLaterHandlers(t *testing.T) {
	t.Parallel()

	d := dp.Dispatcher{}

	shouldPanic := false

	d.RegisterHandlerWithOptions(func(string) {
		if shouldPanic {
			panic(errRaw)
		}
	}, dp.HandlerOptions{UnwrappedPanics: true})

	// Warm the cache for string before registering the second handler so the handler order is deterministic.
	d.Dispatch("warm-up")

	laterCalled := 0

	d.RegisterHandler(func(string) {
		laterCalled++
	})

	d.Dispatch("no panic")
	require.Equal(t, 1, laterCalled, "sanity check: later handler should run when nothing panics")

	shouldPanic = true

	assert.PanicsWithValue(t, errRaw, func() {
		d.Dispatch("panic")
	})
	assert.Equal(t, 1, laterCalled, "later handler must not run after an unwrapped panic")
}

func TestRegisterHandlerWithOptions_NormalHandlerStillWrapped(t *testing.T) {
	t.Parallel()

	d := dp.Dispatcher{}

	// An unwrapped handler for another type must not affect wrapping of normal handlers.
	d.RegisterHandlerWithOptions(func(int) {
		panic(errRaw)
	}, dp.HandlerOptions{UnwrappedPanics: true})

	d.RegisterHandler(func(string) {
		panic(errRaw)
	})

	// Zero-value options must behave like RegisterHandler.
	d.RegisterHandlerWithOptions(func(float64) {
		panic(errRaw)
	}, dp.HandlerOptions{})

	for _, obj := range []interface{}{"txt", 1.5} {
		func() {
			defer func() {
				r := recover()

				ccp, ok := r.(dp.ConsumerCodePanic)
				if assert.True(t, ok, "expected ConsumerCodePanic, got %T", r) {
					assert.Equal(t, errRaw, ccp.Value())
				}
			}()

			d.Dispatch(obj)
		}()
	}
}

func TestRegisterHandlerWithOptions_QueuePanicHandler(t *testing.T) {
	t.Parallel()

	var (
		mu        sync.Mutex
		recovered []interface{}
	)

	d := dp.NewDispatcherWithConfig(dp.Config{
		PanicHandler: func(v interface{}) {
			mu.Lock()
			defer mu.Unlock()

			recovered = append(recovered, v)
		},
	})

	d.RegisterHandlerWithOptions(func(string) {
		panic(errRaw)
	}, dp.HandlerOptions{UnwrappedPanics: true})

	// Note: not int, the queue's internal sync/remove tokens are the ints 0 and 1.
	d.RegisterHandler(func(float64) {
		panic("wrapped")
	})

	q := make(chan interface{}, 2)
	d.AddQueues(q)

	q <- "unwrapped"
	q <- 1.5

	d.SyncAllQueues()

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, recovered, 2)

	assert.Equal(t, errRaw, recovered[0], "PanicHandler should receive the raw value for an unwrapped handler")

	ccp, ok := recovered[1].(dp.ConsumerCodePanic)
	if assert.True(t, ok, "expected ConsumerCodePanic, got %T", recovered[1]) {
		assert.Equal(t, "wrapped", ccp.Value())
	}
}

func TestRegisterHandlerWithOptions_Unregister(t *testing.T) {
	t.Parallel()

	d := dp.Dispatcher{}

	called := 0
	id := d.RegisterHandlerWithOptions(func(int) {
		called++
	}, dp.HandlerOptions{UnwrappedPanics: true})

	calledAB := 0
	idAB := d.RegisterHandlerWithOptions(func(AB) {
		calledAB++
	}, dp.HandlerOptions{UnwrappedPanics: true})

	d.Dispatch(1)
	d.Dispatch(A{})

	d.UnregisterHandler(id)
	d.UnregisterHandler(idAB)

	d.Dispatch(1)
	d.Dispatch(A{})

	assert.Equal(t, 1, called)
	assert.Equal(t, 1, calledAB)
}
