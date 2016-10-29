# godispatch
This is a general purpose object dispatcher for go.

It can be used to either asynchronously dispatch queues of objects or synchronously dispatch single objects to multiple handlers.

## Go Get

	go get github.com/markus-wa/godispatch

## Example
```go
import (
	"fmt"
	"reflect"
	dp "github.com/markus-wa/godispatch"
)

func main() {
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
```

## Queue Example
```go
type Event struct {
	reference int
	message   string
}

type TriggerEvent struct{}

func main() {
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
```

## License
Copyright &copy; 2016 Markus Walther
Licensed [MIT][mit]
[MIT]: http://www.opensource.org/licenses/mit-license.php