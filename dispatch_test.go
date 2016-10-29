package dispatch_test

import (
	"fmt"
	dp "github.com/markus-wa/godispatch"
	"reflect"
	"testing"
)

type AB interface {
	foo() string
}

type A struct {
	val int
}

func (a A) foo() string {
	return string(a.val)
}

type B struct {
	val string
}

func (b B) foo() string {
	return b.val
}

type result struct {
	strVal  string
	intVal  int
	f32Val  float32
	f64Val  float64
	abVal   AB
	aVal    A
	bPtrVal *B
}

func (r result) equal(r2 result) bool {
	b := true
	b = b && r.strVal == r2.strVal
	b = b && r.intVal == r2.intVal
	b = b && r.f32Val == r2.f32Val
	b = b && r.f64Val == r2.f64Val
	if r.abVal != nil {
		if r2.abVal == nil {
			b = false
		} else {
			b = b && r.abVal.foo() == r2.abVal.foo()
		}
	}
	b = b && r.aVal.val == r2.aVal.val
	if r.bPtrVal != nil {
		b = b && r.bPtrVal == r2.bPtrVal
	}
	return b
}

func assertResult(t *testing.T, res *result, exp result) {
	if !res.equal(exp) {
		fmt.Println("expected ", exp, "got", res)
		t.Fail()
	}
}

func registerHandlers(d *dp.Dispatcher) *result {
	res := &result{}

	// string handler
	d.RegisterHandler(reflect.TypeOf((*string)(nil)).Elem(), func(msg interface{}) {
		res.strVal = msg.(string)
		fmt.Println("Handled string", res.strVal)
	})
	// int handler
	d.RegisterHandler(reflect.TypeOf((*int)(nil)).Elem(), func(obj interface{}) {
		res.intVal = obj.(int)
		fmt.Println("Handled int", res.intVal)
	})
	// float32 handler
	d.RegisterHandler(reflect.TypeOf((*float32)(nil)).Elem(), func(obj interface{}) {
		res.f32Val = obj.(float32)
		fmt.Println("Handled float32", res.f32Val)
	})
	// float64 handler
	d.RegisterHandler(reflect.TypeOf((*float64)(nil)).Elem(), func(obj interface{}) {
		res.f64Val = obj.(float64)
		fmt.Println("Handled float64", res.f64Val)
	})
	// interface handler
	d.RegisterHandler(reflect.TypeOf((*AB)(nil)).Elem(), func(obj interface{}) {
		res.abVal = obj.(AB)
		fmt.Println("Handled AB", res.abVal)
	})
	// struct handler
	d.RegisterHandler(reflect.TypeOf((*A)(nil)).Elem(), func(obj interface{}) {
		res.aVal = obj.(A)
		fmt.Println("Handled A", res.aVal)
	})
	// pointer handler (no reflect.Type.Elem() call)
	d.RegisterHandler(reflect.TypeOf((*B)(nil)), func(obj interface{}) {
		res.bPtrVal = obj.(*B)
		fmt.Println("Handled *B", res.bPtrVal)
	})
	return res
}

func TestString(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	val := "test"
	d.Dispatch(val)
	assertResult(t, res, result{strVal: val})
}

func TestInt(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	val := 13
	d.Dispatch(val)
	assertResult(t, res, result{intVal: val})
}

func TestFloat32(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	val := float32(1.23)
	d.Dispatch(val)
	assertResult(t, res, result{f32Val: val})
}

func TestFloat64(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	val := 9.87
	d.Dispatch(val)
	assertResult(t, res, result{f64Val: val})
}

func TestStructA(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	val := A{val: 17}
	d.Dispatch(val)
	assertResult(t, res, result{abVal: val, aVal: val})
}

func TestStructB(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	val := B{val: "bVal"}
	d.Dispatch(val)
	assertResult(t, res, result{abVal: val})
}

func TestStructPointer(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	val := &B{val: "bPtrVal"}
	d.Dispatch(val)
	assertResult(t, res, result{abVal: val, bPtrVal: val})
}

func TestQueues(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	q := make(chan interface{})

	d.AddQueues(q)
	strVal := "txt"
	q <- strVal
	exp := result{strVal: strVal}
	d.SyncAllQueues()
	assertResult(t, res, exp)

	d.RemoveQueues(q)
	select {
	case q <- 10:
		// Nobody should be receiving on this channel after removal
		t.Fail()
	default:
		// nop
	}

	d.AddQueues(q)
	f32Val := float32(0.5)
	q <- f32Val
	exp.f32Val = f32Val
	d.SyncAllQueues()
	assertResult(t, res, exp)
}

func TestAddHandlerInHandler(t *testing.T) {
	d := dp.Dispatcher{}
	h1 := 0
	h2 := 0
	h3 := 0
	d.RegisterHandler(reflect.TypeOf((*int)(nil)).Elem(), func(i interface{}) {
		fmt.Println("Handled", i, "in h1")
		if h1 == 0 {
			d.RegisterHandler(reflect.TypeOf((*int)(nil)).Elem(), func(i2 interface{}) {
				fmt.Println("Handled", i2, "in h2")
				if h2 == 0 {
					d.RegisterHandler(reflect.TypeOf((*int)(nil)).Elem(), func(i3 interface{}) {
						fmt.Println("Handled", i3, "in h3")
						h3++
					})
				}
				h2++
			})
		}
		h1++
	})

	d.Dispatch(1)
	d.Dispatch(2)
	d.Dispatch(3)

	// h2 & h3 should only be increased by new dispatches, not the one which registered it
	if h1 != 3 || h2 != 2 || h3 != 1 {
		t.Fail()
	}
}

// Just a compile test
func TestExample(t *testing.T) {
	d := dp.Dispatcher{}
	// Register a handler for string (not *string!)
	// We get the string Type by calling Elem() on reflect.Type *string)
	// This is faster than doing reflect.TypeOf("")
	d.RegisterHandler(reflect.TypeOf((*string)(nil)).Elem(), func(obj interface{}) {
		s := obj.(string)
		fmt.Println("Handled string", s)
	})
	d.RegisterHandler(reflect.TypeOf((*interface{})(nil)).Elem(), func(obj interface{}) {
		fmt.Println("Handled object", obj)
	})

	d.Dispatch("Hello")
	// Prints (in this order - as the object handler was registered after the string handler)
	// "Handled string Hello"
	// "Handled object Hello"
	d.Dispatch(123)
	// Prints "Handled object 123"
}

type Event struct {
	reference int
	message   string
}

type TriggerEvent struct{}

// Just a compile test
func TestQueueExample(t *testing.T) {
	d := dp.Dispatcher{}
	// If you wanted to handle pointers of the Event just remove .Elem(),
	// use *Event for the type assertion and send pointers
	d.RegisterHandler(reflect.TypeOf((*Event)(nil)).Elem(), func(obj interface{}) {
		e := obj.(Event)
		fmt.Println("Handled Event", e)
		// Handle event
	})
	d.RegisterHandler(reflect.TypeOf((*TriggerEvent)(nil)).Elem(), func(obj interface{}) {
		// Do stuff when we receive a 'TriggerEvent'
	})

	// Buffered to improve performance by avoiding locking
	q := make(chan interface{}, 5)
	q2 := make(chan interface{}, 5)
	q3 := make(chan interface{}, 5)

	// Add queues to dispatcher
	d.AddQueues(q, q2, q3)

	// Send some events
	for i := 0; i < 10; i++ {
		q <- Event{i, "abc"}
		q <- TriggerEvent{}
		q2 <- Event{i, "def"}
		q3 <- Event{i, "geh"}
		// Events that are not in the same queue will be handled concurrently
		d.SyncQueues(q)
		// Do stuff that requires events in q (but not q2 & q3) to be handled
	}
	d.SyncAllQueues()
	// Do stuff that requires events of q, q2 & q3 to be handled

	// Maybe send some more events . . .
	q <- TriggerEvent{}

	// Remove queues q & q2
	d.RemoveQueues(q, q2)

	q3 <- Event{}

	// Also remove q3
	d.RemoveAllQueues()
}

// Benchmarks showing that you should always use reflect.TypeOf((*type)(nil)).Elem() instead of creating instances
// It gives 2x-3x performance

func BenchmarkTypeOfEmptyString(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = reflect.TypeOf("")
	}
}

func BenchmarkTypeOfStringPtrElem(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = reflect.TypeOf((*string)(nil)).Elem()
	}
}

func BenchmarkTypeOfZeroInt(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = reflect.TypeOf(0)
	}
}

func BenchmarkTypeOfIntPtrElem(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = reflect.TypeOf((*int)(nil)).Elem()
	}
}

func BenchmarkTypeOfEmptyStruct(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = reflect.TypeOf(A{})
	}
}

func BenchmarkTypeOfStructPtrElem(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = reflect.TypeOf((*A)(nil)).Elem()
	}
}
