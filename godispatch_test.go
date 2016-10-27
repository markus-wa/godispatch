package godispatch_test

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

func registerHandlers(d *dp.Dispatcher) *result {
	res := &result{}

	// string handler
	d.RegisterHandler(reflect.TypeOf(""), func(msg interface{}) {
		res.strVal = msg.(string)
		fmt.Println("handled string", res.strVal)
	})
	// int handler
	d.RegisterHandler(reflect.TypeOf(0), func(obj interface{}) {
		res.intVal = obj.(int)
		fmt.Println("handled int", res.intVal)
	})
	// float32 handler
	d.RegisterHandler(reflect.TypeOf(float32(0)), func(obj interface{}) {
		res.f32Val = obj.(float32)
		fmt.Println("handled float32", res.f32Val)
	})
	// float64 handler
	d.RegisterHandler(reflect.TypeOf(float64(0)), func(obj interface{}) {
		res.f64Val = obj.(float64)
		fmt.Println("handled float64", res.f64Val)
	})
	// interface handler
	d.RegisterHandler(reflect.TypeOf((*AB)(nil)).Elem(), func(obj interface{}) {
		res.abVal = obj.(AB)
		fmt.Println("handled AB", res.abVal)
	})
	// struct handler
	d.RegisterHandler(reflect.TypeOf((*A)(nil)).Elem(), func(obj interface{}) {
		res.aVal = obj.(A)
		fmt.Println("handled A", res.aVal)
	})
	// pointer handler
	d.RegisterHandler(reflect.TypeOf((*B)(nil)), func(obj interface{}) {
		res.bPtrVal = obj.(*B)
		fmt.Println("handled *B", res.bPtrVal)
	})
	return res
}

func TestString(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	val := "test"
	d.Dispatch(val)
	exp := result{strVal: val}
	if !res.equal(exp) {
		fmt.Println("expected ", exp, "got", res)
		t.Fail()
	}
}

func TestInt(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	val := 13
	d.Dispatch(val)
	exp := result{intVal: val}
	if !res.equal(exp) {
		fmt.Println("expected ", exp, "got", res)
		t.Fail()
	}
}

func TestFloat32(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	val := float32(1.23)
	d.Dispatch(val)
	exp := result{f32Val: val}
	if !res.equal(exp) {
		fmt.Println("expected ", exp, "got", res)
		t.Fail()
	}
}

func TestFloat64(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	val := 9.87
	d.Dispatch(val)
	exp := result{f64Val: val}
	if !res.equal(exp) {
		fmt.Println("expected ", exp, "got", res)
		t.Fail()
	}
}

func TestStructA(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	val := A{val: 17}
	d.Dispatch(val)
	exp := result{abVal: val, aVal: val}
	if !res.equal(exp) {
		fmt.Println("expected ", exp, "got", res)
		t.Fail()
	}
}

func TestStructB(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	val := B{val: "bVal"}
	d.Dispatch(val)
	exp := result{abVal: val}
	if !res.equal(exp) {
		fmt.Println("expected ", exp, "got", res)
		t.Fail()
	}
}

func TestStructPointer(t *testing.T) {
	d := dp.Dispatcher{}
	res := registerHandlers(&d)

	val := &B{val: "bPtrVal"}
	d.Dispatch(val)
	exp := result{abVal: val, bPtrVal: val}
	if !res.equal(exp) {
		fmt.Println("expected ", exp, "got", res)
		t.Fail()
	}
}
