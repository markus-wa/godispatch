package dispatch_test

import (
	"testing"

	dp "github.com/markus-wa/godispatch"
)

func benchmarkDispatch(b *testing.B, nHandlers int) {
	b.Helper()

	d := dp.Dispatcher{}

	sum := 0

	for i := 0; i < nHandlers; i++ {
		d.RegisterHandler(func(e Event) {
			sum += e.reference
		})
	}

	e := Event{reference: 1}
	d.Dispatch(e) // warm up the handler cache

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		d.Dispatch(e)
	}
}

func BenchmarkDispatch_1Handler(b *testing.B) {
	benchmarkDispatch(b, 1)
}

func BenchmarkDispatch_10Handlers(b *testing.B) {
	benchmarkDispatch(b, 10)
}

// 5 concrete and 5 interface handlers, interleaved, matching the same object.
func BenchmarkDispatch_MixedInterfaceAndConcrete(b *testing.B) {
	d := dp.Dispatcher{}

	sum := 0

	for i := 0; i < 5; i++ {
		d.RegisterHandler(func(a A) {
			sum += a.val
		})
		d.RegisterHandler(func(AB) {
			sum++
		})
	}

	a := A{val: 1}
	d.Dispatch(a) // warm up the handler cache

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		d.Dispatch(a)
	}
}

// Cost of the first Dispatch of a type, i.e. building the handler cache, with 50 handlers.
func BenchmarkDispatch_FirstDispatch_50Handlers(b *testing.B) {
	const nHandlers = 50

	sum := 0
	e := Event{reference: 1}

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()

		d := &dp.Dispatcher{}

		for j := 0; j < nHandlers; j++ {
			d.RegisterHandler(func(e Event) {
				sum += e.reference
			})
		}

		b.StartTimer()

		d.Dispatch(e)
	}
}

// Cost of registering a handler while no handler cache exists.
func BenchmarkRegisterHandler(b *testing.B) {
	d := dp.Dispatcher{}

	sum := 0

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		d.RegisterHandler(func(e Event) {
			sum += e.reference
		})
	}
}

// Cost of registering a handler for a type that has already been dispatched (cache entry exists).
func BenchmarkRegisterHandler_CacheWarm(b *testing.B) {
	d := dp.Dispatcher{}

	sum := 0

	d.RegisterHandler(func(e Event) {
		sum += e.reference
	})
	d.Dispatch(Event{})

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		d.RegisterHandler(func(e Event) {
			sum += e.reference
		})
	}
}

// The benchmarks below use the options API (v1.5.0+).

func BenchmarkDispatch_Unwrapped(b *testing.B) {
	d := dp.Dispatcher{}

	sum := 0

	for i := 0; i < 10; i++ {
		d.RegisterHandlerWithOptions(func(e Event) {
			sum += e.reference
		}, dp.HandlerOptions{UnwrappedPanics: true})
	}

	e := Event{reference: 1}
	d.Dispatch(e) // warm up the handler cache

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		d.Dispatch(e)
	}
}
