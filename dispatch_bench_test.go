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
