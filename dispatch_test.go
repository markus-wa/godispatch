package dispatch_test

import (
	"fmt"
	"strconv"
	"testing"

	dp "github.com/markus-wa/godispatch"
)

type AB interface {
	foo() string
}

type A struct {
	val int
}

func (a A) foo() string {
	return strconv.Itoa(a.val)
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
		t.Error("expected ", exp, "got", res)
	}
}

func registerHandlers(d *dp.Dispatcher) *result {
	res := &result{}

	// string handler
	d.RegisterHandler(func(s string) {
		res.strVal = s
		fmt.Println("Handled string", res.strVal)
	})
	// int handler
	d.RegisterHandler(func(i int) {
		res.intVal = i
		fmt.Println("Handled int", res.intVal)
	})
	// float32 handler
	d.RegisterHandler(func(f float32) {
		res.f32Val = f
		fmt.Println("Handled float32", res.f32Val)
	})
	// float64 handler
	d.RegisterHandler(func(f float64) {
		res.f64Val = f
		fmt.Println("Handled float64", res.f64Val)
	})
	// interface handler
	d.RegisterHandler(func(ab AB) {
		res.abVal = ab
		fmt.Println("Handled AB", res.abVal)
	})
	// struct handler
	d.RegisterHandler(func(a A) {
		res.aVal = a
		fmt.Println("Handled A", res.aVal)
	})
	// pointer handler (no reflect.Type.Elem() call)
	d.RegisterHandler(func(bPtr *B) {
		res.bPtrVal = bPtr
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

// Should only be handled by float64 handler, not float32
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

// Tests if the handler cache is cleared after registering an interface handler
func TestStructB(t *testing.T) {
	d := dp.Dispatcher{}

	val := B{val: "bVal"}
	// Make Dispatcher initalize the cache
	d.Dispatch(val)

	res := registerHandlers(&d)

	d.Dispatch(val)
	assertResult(t, res, result{abVal: val})
}

// Should work with pointers just as well
func TestStructPointer(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	val := &B{val: "bPtrVal"}
	d.Dispatch(val)
	assertResult(t, res, result{abVal: val, bPtrVal: val})
}

// Tests queue functionality
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
		t.Error("Data sent to channel and still received after RemoveQueues()")
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

// Tests how handlers behave if added dynamically by other handlers
func TestAddHandlerInHandler(t *testing.T) {
	d := dp.Dispatcher{}
	h1 := 0
	h2 := 0
	h3 := 0
	d.RegisterHandler(func(i int) {
		fmt.Println("Handled", i, "in h1")
		if h1 == 0 {
			d.RegisterHandler(func(i2 int) {
				fmt.Println("Handled", i2, "in h2")
				if h2 == 0 {
					d.RegisterHandler(func(i3 int) {
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
		t.Errorf("Handler call counts should be h1=3, h2=2 & h3=1 but are h1=%d, h2=%d, h3=%d", h1, h2, h3)
	}
}

// Tests if unregistering handlers works
func TestUnregisterHandler(t *testing.T) {
	d := dp.Dispatcher{}

	iInt := 0
	d.RegisterHandler(func(int) {
		iInt++
	})

	iRemovedInt := 0
	id := d.RegisterHandler(func(int) {
		iRemovedInt++
	})

	d.Dispatch(1)

	d.UnregisterHandler(id)

	d.Dispatch(1)

	if iInt != 2 {
		t.Error("The handler that wasn't removed wasn't triggered after removal")
	}
	if iRemovedInt != 1 {
		t.Error("The removed handler was triggered after removal")
	}

	iInterface := 0
	d.RegisterHandler(func(AB) {
		iInterface++
	})

	iRemovedInterface := 0
	id = d.RegisterHandler(func(AB) {
		iRemovedInterface++
	})

	d.Dispatch(A{})

	d.UnregisterHandler(id)

	d.Dispatch(A{})

	if iInterface != 2 {
		t.Error("The interface handler that wasn't removed wasn't triggered after removal")
	}
	if iRemovedInterface != 1 {
		t.Error("The removed interface handler was triggered after removal")
	}
}

// Tests what happens for illegal arguments
func TestIllegalArguments(t *testing.T) {
	d := dp.Dispatcher{}

	err := d.RemoveQueues(make(chan interface{}))
	if err == nil {
		t.Error("RemoveQueues() returned no error for non-registered queue")
	}
	err = d.SyncQueues(make(chan interface{}))
	if err == nil {
		t.Error("SyncQueues() returned no error for non-registered queue")
	}

	func() {
		defer func() {
			e := recover()
			if e == nil {
				t.Error("RegisterHandler() didn't panic for wrong handler type")
			}
		}()
		d.RegisterHandler(5)
	}()

	func() {
		defer func() {
			e := recover()
			if e == nil {
				t.Error("RegisterHandler() didn't panic for wrong input parameter count")
			}
		}()
		d.RegisterHandler(func() {})
	}()
}

func TestConsumerCodePanic(t *testing.T) {
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

	switch ccp := err.(type) {
	case dp.ConsumerCodePanic:
		if ccp.String() != "runtime error: invalid memory address or nil pointer dereference" {
			t.Error("ConsumerCodePanic.String() is not \"runtime error: invalid memory address or nil pointer dereference\"")
		}
		if fmt.Sprint(ccp.Value()) != "runtime error: invalid memory address or nil pointer dereference" {
			t.Error("ConsumerCodePanic.Value() is not \"runtime error: invalid memory address or nil pointer dereference\"")
		}
	default:
		t.Error("recovered type != ConsumerCodePanic")
	}
}

// Just a compile test for the README example
func TestExample(t *testing.T) {
	d := dp.Dispatcher{}
	// Register a handler for string (not *string!)
	// We get the string Type by calling Elem() on reflect.Type *string)
	// This is faster than doing reflect.TypeOf("")
	d.RegisterHandler(func(s string) {
		fmt.Println("Handled string", s)
	})
	d.RegisterHandler(func(obj interface{}) {
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

// Just a compile test for the README example
func TestQueueExample(t *testing.T) {
	d := dp.Dispatcher{}
	// If you wanted to handle pointers of the Event just remove .Elem(),
	// use *Event for the type assertion and send pointers
	d.RegisterHandler(func(e Event) {
		fmt.Println("Handled Event", e)
		// Handle event
	})
	d.RegisterHandler(func(te TriggerEvent) {
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

// Tests queue functionality
func TestPanicHandler(t *testing.T) {
	var recovered interface{}

	d := dp.NewDispatcherWithConfig(dp.Config{
		PanicHandler: func(v interface{}) {
			recovered = v
		},
	})

	d.RegisterHandler(func(string) {
		panic("test")
	})

	q := make(chan interface{}, 2)

	d.AddQueues(q)
	q <- "txt"

	d.SyncAllQueues()

	if recovered.(dp.ConsumerCodePanic).Value() != "test" {
		t.Error("panic value not recovered")
	}
}
