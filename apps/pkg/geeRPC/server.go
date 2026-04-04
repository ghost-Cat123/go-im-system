package GeeRPC

import (
	"GeeRPC/codec"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
)

const MagicNumber = 0x3bef5c

// Option Option固定在报文开始 后面可接多个header和body
// option 使用JSON解码 之后使用CodecType解码剩余的头和消息体
type Option struct {
	// 标记这是一个RPC请求
	MagicNumber int
	// 用户可选的编码格式
	CodecType codec.Type
	// 连接超时时间
	ConnectTimeout time.Duration
	// 处理超时时间
	HandleTimeout time.Duration
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	// 默认编码方式Gob
	CodecType: codec.GobType,
	// 默认10s
	ConnectTimeout: time.Second * 10,
}

// Server RPC服务器
type Server struct {
	// 注册服务列表
	serviceMap sync.Map
	// 请求中间件列表
	interceptors []Interceptor
}

// NewServer Server构造函数
func NewServer() *Server {
	return &Server{}
}

// DefaultServer Server实例
var DefaultServer = NewServer()

// Use 服务使用中间件
func (server *Server) Use(interceptors ...Interceptor) {
	server.interceptors = append(server.interceptors, interceptors...)
}

// Use 默认实例调用
func Use(interceptors ...Interceptor) {
	DefaultServer.interceptors = append(DefaultServer.interceptors, interceptors...)
}

func Accept(lis net.Listener) { DefaultServer.Accept(lis) }

// Accept 监听服务器请求时接收链接
func (server *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		// 使用多线程链接多个服务
		go server.ServeConn(conn)
	}
}

// ServeConn 服务连接
func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() { _ = conn.Close() }()
	// 使用json反序列化得到option实例
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error: ", err)
		return
	}
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}
	// 寻找相应编码格式的解析函数
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}
	server.serveCodec(f(conn))
}

// 错误发生时响应参数的占位符
var invalidRequest = struct{}{}

// 服务处理
func (server *Server) serveCodec(cc codec.Codec) {
	// 互斥锁
	sending := new(sync.Mutex)
	// 加锁 直到所有请求被处理 等待组
	wg := new(sync.WaitGroup)
	for {
		// 读取请求 一次连接 允许接收多个请求
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				// 不可能恢复 则关闭连接
				break
			}
			req.h.Error = err.Error()
			// 回复请求 逐个发送 使用锁保证
			server.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		// 计数
		wg.Add(1)
		// 处理请求
		go server.handleRequest(cc, req, sending, wg, DefaultOption.ConnectTimeout)
	}
	wg.Wait()
	_ = cc.Close()
}

type Request struct {
	// 请求头
	h *codec.Header
	// 请求参数和响应值
	argv, replyv reflect.Value
	// 方法类型
	mtype *methodType
	// 服务
	svc *service
}

// CallInfo 中间件相关
// CallInfo 暴露给中间件的只读参数
type CallInfo struct {
	ServiceMethod string
	Header        *codec.Header
	ReqArgs       interface{}
}

// HandlerFunc 基本类型 只传请求
type HandlerFunc func(i *CallInfo) error

// Interceptor 中间件类型
type Interceptor func(next HandlerFunc) HandlerFunc

func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && !errors.Is(err, io.ErrUnexpectedEOF) {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}

func (server *Server) readRequest(cc codec.Codec) (*Request, error) {
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &Request{h: h}
	req.svc, req.mtype, err = server.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}
	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()

	// 确保argvi是一个指针
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		// 使用地址
		argvi = req.argv.Addr().Interface()
	}
	// 反序列化请求报文 转化成argv
	if err = cc.ReadBody(argvi); err != nil {
		log.Println("rpc server: read body err:", err)
		return req, nil
	}
	return req, nil
}

func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	// 加锁
	sending.Lock()
	// 解锁
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

// 正常情况 由处理方法调用的goroutine发送响应
// 超时情况 由主goroutine发送超时响应
// 使用拦截器处理请求
func (server *Server) handleRequest(cc codec.Codec, req *Request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	// 需要注册rpc方法到正确的响应中
	// 计数器-1 表示已处理完成一个响应
	defer wg.Done()
	// 拆分处理过程
	called := make(chan struct{})
	sent := make(chan struct{})
	go func() {
		// 方法调用
		// 添加拦截器拦截
		info := &CallInfo{
			ServiceMethod: req.h.ServiceMethod,
			Header:        req.h,
			ReqArgs:       req.argv,
		}

		var handler HandlerFunc = func(i *CallInfo) error {
			// 真正调用的方法
			return req.svc.call(req.mtype, req.argv, req.replyv)
		}

		// 组装中间件
		for i := len(server.interceptors) - 1; i >= 0; i-- {
			handler = server.interceptors[i](handler)
		}

		// 调用最后的方法
		err := handler(info)

		// 标记调用完成
		called <- struct{}{}
		// 错误分支
		if err != nil {
			req.h.Error = err.Error()
			// 发生错误 响应错误
			server.sendResponse(cc, req.h, invalidRequest, sending)
			sent <- struct{}{}
			return
		}
		// 将reply序列化为字节流 构造响应报文
		// 成功分支
		server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
		// 通过管道发送
		// 标记响应完成
		sent <- struct{}{}
	}()

	// 没有设置超时时间 就一直阻塞
	if timeout == 0 {
		<-called
		<-sent
		return
	}
	select {
	// 超时时间大于0 调用优先与超时完成
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		server.sendResponse(cc, req.h, invalidRequest, sending)
	// 正常执行或发生异常
	case <-called:
		<-sent
	}
}

// Register 注册服务
func (server *Server) Register(rcvr interface{}) error {
	s := newService(rcvr)
	// 判断服务是否已经注册
	if _, dup := server.serviceMap.LoadOrStore(s.name, s); dup {
		return errors.New("rpc: service already defined: " + s.name)
	}
	return nil
}

func Register(rcvr interface{}) error {
	return DefaultServer.Register(rcvr)
}

// 发现服务
func (server *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed: " + serviceMethod)
		return
	}
	// Service.Method 拆成两部分
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	// 在map中找到对应service实例
	svci, ok := server.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can't find service " + serviceName)
		return
	}
	// 断言转成*service类型
	svc = svci.(*service)
	// 从method中找到对应methodType
	mtype = svc.method[methodName]
	if mtype == nil {
		err = errors.New("rpc server: can't find method " + methodName)
	}
	return
}

// 使服务端支持HTTP协议
// 接收CONNECT请求 返回了200状态码 HTTP/1.0 200 Connected to Gee RPC

const (
	// 隧道建立成功的响应内容
	connected = "200 Connected to Gee RPC"
	// RPC请求的默认HTTP路径 区分普通HTTP请求
	defaultRPCPath = "/_geerpc_"
	// 调试路径
	defaultDebugPath = "/debug/geerpc"
)

// 实现Handler接口中的ServeHTTP方法 即可处理HTTP请求
func (server *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// 1. 只有连接请求才被允许
	if req.Method != "CONNECT" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		// 返回方法不被允许
		_, _ = io.WriteString(w, "405 must CONNECT\n")
		return
	}
	// 2. 劫持HTTP底层TCP连接（关键步骤）
	// http.Hijacker是ResponseWriter的扩展接口，允许“劫持”连接脱离HTTP协议控制
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Print("rpc hijacking ", req.RemoteAddr, ": ", err.Error())
		return
	}
	// 3. 向客户端发送隧道建立成功响应
	_, _ = io.WriteString(conn, "HTTP/1.0 "+connected+"\n\n")

	// 4. 将劫持的TCP连接交给RPC服务器处理（后续通信脱离HTTP）
	server.ServeConn(conn)
}

func (server *Server) HandleHTTP() {
	http.Handle(defaultRPCPath, server)
	http.Handle(defaultDebugPath, debugHTTP{server})
	log.Println("rpc server debug path:", defaultDebugPath)
}

func HandleHTTP() {
	DefaultServer.HandleHTTP()
}
