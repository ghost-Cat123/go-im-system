package GeeRPC

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

// 通过反射实现结构体与服务的映射关系

type methodType struct {
	// 具体方法
	method reflect.Method
	// 参数类型 第一个参数
	ArgType reflect.Type
	// 返回值类型 第二个参数
	ReplyType reflect.Type
	// 方法调用次数
	numCalls uint64
}

func (m *methodType) NumCalls() uint64 {
	// 原子操作
	return atomic.LoadUint64(&m.numCalls)
}

// 创建参数实例
func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value
	// 如果是指针类型
	if m.ArgType.Kind() == reflect.Ptr {
		// .Elem解引用才表示拿到真正的值
		argv = reflect.New(m.ArgType.Elem())
	} else {
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}

// 创建返回值实例
func (m *methodType) newReplyv() reflect.Value {
	// reply必须要是一个指针
	replyv := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		// slice的底层是三个参数
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}
	return replyv
}

type service struct {
	// 映射(服务方法)的结构体名称
	name string
	// 结构体类型
	typ reflect.Type
	// 结构体实例
	rcvr reflect.Value
	// 方法与类型的映射
	method map[string]*methodType
}

func newService(rcvr interface{}) *service {
	s := new(service)
	s.rcvr = reflect.ValueOf(rcvr)
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	s.typ = reflect.TypeOf(rcvr)
	// 判断方法和方法类型是否可导出
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}
	s.registerMethods()
	return s
}

// 注册方法
func (s *service) registerMethods() {
	s.method = make(map[string]*methodType)
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		// 要满足 仅有两个入参(反射加上自身算三个) 和一个返回值
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		// 并且返回值必须是error
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		s.method[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

// 判断参数类型是否可导出
func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

// Call 通过反射值调用方法
func (s *service) call(m *methodType, argv, replyv reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func
	returnValues := f.Call([]reflect.Value{s.rcvr, argv, replyv})
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
