package vm

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/dalurness/clank/internal/compiler"
)

// ── std.srv — HTTP server ──

// spawnRequestVM creates a child VM for handling an HTTP request.
// Similar to spawnTaskVM but with clean stacks and no task state.
func (vm *VM) spawnRequestVM() *VM {
	root := vm.root()
	// Create a dummy entry word so callBuiltinFn can save frames
	dummyWord := &compiler.BytecodeWord{
		Name:   "__srv_handler__",
		WordID: -1,
		Code:   []byte{compiler.OpNOP},
	}
	child := &VM{
		wordMap:      root.wordMap,
		strings:      root.strings,
		rationals:    root.rationals,
		variantNames: root.variantNames,
		dispatchTbl:  root.dispatchTbl,
		module:       root.module,
		parent:       root,
		ctx:          root.ctx,
		logLevel:     root.logLevel,
		logContext:    root.logContext,
		logJSON:      root.logJSON,
		currentWord:  dummyWord,
	}
	child.topFrame = &CallFrame{
		Locals: []Value{ValUnit()},
	}
	return child
}

func (vm *VM) builtinSrvNew() error {
	vm.push(ValList(nil))
	return nil
}

func (vm *VM) builtinSrvRoute(method string) error {
	handler, _ := vm.pop()
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	routes, err := vm.popList()
	if err != nil {
		return err
	}
	route := ValRecord(map[string]Value{
		"method":  ValStr(method),
		"path":    ValStr(path),
		"handler": handler,
	}, []string{"method", "path", "handler"})
	result := make([]Value, len(routes)+1)
	copy(result, routes)
	result[len(routes)] = route
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinSrvGet() error  { return vm.builtinSrvRoute("GET") }
func (vm *VM) builtinSrvPost() error { return vm.builtinSrvRoute("POST") }
func (vm *VM) builtinSrvPut() error  { return vm.builtinSrvRoute("PUT") }
func (vm *VM) builtinSrvDel() error  { return vm.builtinSrvRoute("DELETE") }

func (vm *VM) builtinSrvStart() error {
	portVal, _ := vm.pop()
	routeList, err := vm.popList()
	if err != nil {
		return err
	}
	port := numVal(portVal)

	// Extract routes from the Clank list of records
	var routes []HttpRoute
	var middleware []Value
	for _, rv := range routeList {
		if rv.Tag != TagHEAP || rv.Heap.Kind != KindRecord {
			continue
		}
		f := rv.Heap.Fields
		// Check if this is a middleware entry
		if _, isMw := f["middleware"]; isMw {
			middleware = append(middleware, f["middleware"])
			continue
		}
		r := HttpRoute{}
		if m, ok := f["method"]; ok && m.Tag == TagSTR {
			r.Method = m.StrVal
		}
		if p, ok := f["path"]; ok && p.Tag == TagSTR {
			r.Path = p.StrVal
		}
		if h, ok := f["handler"]; ok {
			r.Handler = h
		}
		routes = append(routes, r)
	}

	srvState := &HttpServerState{
		Routes:     routes,
		Middleware: middleware,
		RootVM:     vm.root(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		vm.handleRequest(srvState, w, r)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
	srvState.Server = server

	// Start listening
	ln, listenErr := net.Listen("tcp", server.Addr)
	if listenErr != nil {
		return vm.trap("E900", fmt.Sprintf("srv.start: %v", listenErr))
	}

	go func() {
		server.Serve(ln)
	}()

	// Brief pause to let the server start accepting
	time.Sleep(5 * time.Millisecond)

	vm.push(ValServer(srvState))
	return nil
}

func (vm *VM) handleRequest(srvState *HttpServerState, w http.ResponseWriter, r *http.Request) {
	// Find matching route
	var matchedRoute *HttpRoute
	params := make(map[string]Value)

	for i := range srvState.Routes {
		route := &srvState.Routes[i]
		if route.Method != r.Method {
			continue
		}
		if matchPath(route.Path, r.URL.Path, params) {
			matchedRoute = route
			break
		}
	}

	if matchedRoute == nil {
		http.NotFound(w, r)
		return
	}

	// Build request record
	headers := make(map[string]Value)
	headerOrder := make([]string, 0)
	for k, vs := range r.Header {
		headers[k] = ValStr(strings.Join(vs, ", "))
		headerOrder = append(headerOrder, k)
	}

	query := make(map[string]Value)
	queryOrder := make([]string, 0)
	for k, vs := range r.URL.Query() {
		query[k] = ValStr(strings.Join(vs, ", "))
		queryOrder = append(queryOrder, k)
	}

	bodyBytes, _ := io.ReadAll(r.Body)
	paramsOrder := make([]string, 0, len(params))
	for k := range params {
		paramsOrder = append(paramsOrder, k)
	}

	reqRecord := ValRecord(map[string]Value{
		"method":  ValStr(r.Method),
		"path":    ValStr(r.URL.Path),
		"headers": ValRecord(headers, headerOrder),
		"body":    ValStr(string(bodyBytes)),
		"params":  ValRecord(params, paramsOrder),
		"query":   ValRecord(query, queryOrder),
	}, []string{"method", "path", "headers", "body", "params", "query"})

	// Spawn a child VM for this request
	rootVM := srvState.RootVM.(*VM)
	child := rootVM.spawnRequestVM()

	handler := matchedRoute.Handler

	// Apply middleware in reverse order (outer first)
	for i := len(srvState.Middleware) - 1; i >= 0; i-- {
		mw := srvState.Middleware[i]
		result, callErr := child.callBuiltinFn(mw, []Value{reqRecord, handler})
		if callErr == nil {
			writeResponse(w, result)
			return
		}
	}

	// Call handler
	result, callErr := child.callBuiltinFn(handler, []Value{reqRecord})
	if callErr != nil {
		http.Error(w, fmt.Sprintf("handler error: %v", callErr), 500)
		return
	}

	writeResponse(w, result)
}

func writeResponse(w http.ResponseWriter, result Value) {
	if result.Tag != TagHEAP || result.Heap.Kind != KindRecord {
		http.Error(w, "handler returned non-record", 500)
		return
	}

	f := result.Heap.Fields

	// Set headers
	if hdrs, ok := f["headers"]; ok && hdrs.Tag == TagHEAP && hdrs.Heap.Kind == KindRecord {
		for k, v := range hdrs.Heap.Fields {
			if v.Tag == TagSTR {
				w.Header().Set(k, v.StrVal)
			}
		}
	}

	// Status code
	status := 200
	if s, ok := f["status"]; ok {
		status = numVal(s)
	}

	// Body
	body := ""
	if b, ok := f["body"]; ok && b.Tag == TagSTR {
		body = b.StrVal
	}

	w.WriteHeader(status)
	w.Write([]byte(body))
}

// matchPath matches a route pattern against a request path.
// Supports :param segments for path parameter extraction.
func matchPath(pattern, path string, params map[string]Value) bool {
	patParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	if len(patParts) != len(pathParts) {
		return false
	}

	for i, pp := range patParts {
		if strings.HasPrefix(pp, ":") {
			params[pp[1:]] = ValStr(pathParts[i])
		} else if pp != pathParts[i] {
			return false
		}
	}
	return true
}

func (vm *VM) builtinSrvStop() error {
	v, _ := vm.pop()
	if v.Tag != TagHEAP || v.Heap.Kind != KindServer || v.Heap.Server == nil {
		return vm.trap("E002", "srv.stop: expected Server")
	}
	srv := v.Heap.Server.Server.(*http.Server)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return vm.trap("E900", fmt.Sprintf("srv.stop: %v", err))
	}
	vm.push(ValUnit())
	return nil
}

func (vm *VM) builtinSrvRes() error {
	body, err := vm.popStr()
	if err != nil {
		return err
	}
	statusVal, _ := vm.pop()
	status := numVal(statusVal)
	vm.push(ValRecord(map[string]Value{
		"status":  ValInt(status),
		"body":    ValStr(body),
		"headers": ValRecord(make(map[string]Value), nil),
	}, []string{"status", "body", "headers"}))
	return nil
}

func (vm *VM) builtinSrvJSON() error {
	jsonVal, _ := vm.pop()
	statusVal, _ := vm.pop()
	status := numVal(statusVal)
	body := ""
	if jsonVal.Tag == TagSTR {
		body = jsonVal.StrVal
	} else {
		body = valShow(jsonVal)
	}
	hdrs := map[string]Value{
		"Content-Type": ValStr("application/json"),
	}
	vm.push(ValRecord(map[string]Value{
		"status":  ValInt(status),
		"body":    ValStr(body),
		"headers": ValRecord(hdrs, []string{"Content-Type"}),
	}, []string{"status", "body", "headers"}))
	return nil
}

func (vm *VM) builtinSrvHdr() error {
	val, err := vm.popStr()
	if err != nil {
		return err
	}
	key, err := vm.popStr()
	if err != nil {
		return err
	}
	resp, _ := vm.pop()
	if resp.Tag != TagHEAP || resp.Heap.Kind != KindRecord {
		return vm.trap("E002", "srv.hdr: expected response record")
	}
	newFields := make(map[string]Value)
	for k, v := range resp.Heap.Fields {
		newFields[k] = v
	}
	// Get existing headers or create new
	hdrs := make(map[string]Value)
	hdrOrder := []string{}
	if h, ok := newFields["headers"]; ok && h.Tag == TagHEAP && h.Heap.Kind == KindRecord {
		for k, v := range h.Heap.Fields {
			hdrs[k] = v
		}
		hdrOrder = append(hdrOrder, h.Heap.FieldOrder...)
	}
	hdrs[key] = ValStr(val)
	hdrOrder = append(hdrOrder, key)
	newFields["headers"] = ValRecord(hdrs, hdrOrder)
	order := make([]string, len(resp.Heap.FieldOrder))
	copy(order, resp.Heap.FieldOrder)
	vm.push(ValRecord(newFields, order))
	return nil
}

func (vm *VM) builtinSrvMw() error {
	mwFn, _ := vm.pop()
	routes, err := vm.popList()
	if err != nil {
		return err
	}
	// Append middleware marker to the route list
	mwEntry := ValRecord(map[string]Value{
		"middleware": mwFn,
	}, []string{"middleware"})
	result := make([]Value, len(routes)+1)
	copy(result, routes)
	result[len(routes)] = mwEntry
	vm.push(ValList(result))
	return nil
}
