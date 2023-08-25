package grouter

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var _instance *Server
var once sync.Once

type TLSConfig struct {
	CertFilePath string
	KeyFilePath  string
}

type Server struct {
	router            *Router
	tls               *TLSConfig
	httpServer        *http.Server
	serving           bool
	shuttingDown      bool
	context           context.Context
	additionalCleanup []func(context.Context) error
}

func setup() []func(context.Context) error {
	// Tracing
	tracingCleanup, err := startTracing()
	if err != nil {
		log.Fatal(err)
	}
	return []func(context.Context) error{
		tracingCleanup,
	}
}

func GetServer(ctx *context.Context, tls *TLSConfig) *Server {
	once.Do(func() {
		additionalCleanup := setup()
		_instance = &Server{
			router:            NewRouter(*ctx),
			tls:               tls,
			httpServer:        nil,
			serving:           false,
			shuttingDown:      false,
			context:           *ctx,
			additionalCleanup: additionalCleanup,
		}
		// If the context is nil, create a new one with the default background context
		if _instance.context == nil {
			_instance.SetTracingContext(context.Background())
		}
	})
	// When testing the server, we need to be able to change the TLS config on the fly
	if _instance.tls != tls {
		return _instance.SetTLSConfig(tls)
	}
	return _instance
}

func (instance *Server) SetTracingContext(ctx context.Context) *Server {
	instance.context = ctx
	instance.router.context = ctx
	return instance
}

func (instance *Server) SetRouter(router *Router) *Server {
	if instance.router == router {
		return instance
	}
	if instance.serving {
		fmt.Println("Changing the router while the server is running is not supported, shuting down the server...")
		err := instance.Shutdown(true)
		if err != nil {
			log.Fatal(err)
		}
	}
	instance.router = router
	return instance
}

func (instance *Server) SetTLSConfig(tls *TLSConfig) *Server {
	if instance.tls == tls {
		return instance
	}
	if instance.serving {
		fmt.Println("Changing the TLS config while the server is running is not supported, shuting down the server...")
		err := instance.Shutdown(true)
		if err != nil {
			log.Fatal(err)
		}
	}
	if err := validatePath(tls.CertFilePath); err != nil {
		log.Fatal(err)
	}
	if err := validatePath(tls.KeyFilePath); err != nil {
		log.Fatal(err)
	}
	instance.tls = tls
	return instance
}

func (instance *Server) UseGlobal(handler RequestHandler, options *GlobalRouteOptions) {
	instance.router.UseGlobal(handler, options)
}

func (instance *Server) Use(path string, method HTTPMethod, handler RequestHandler) {
	instance.router.Use(path, method, handler)
}

func (instance *Server) Get(path string, handler RequestHandler) {
	instance.Use(path, GET, handler)
}

func (instance *Server) Post(path string, handler RequestHandler) {
	instance.Use(path, POST, handler)
}

func (instance *Server) Put(path string, handler RequestHandler) {
	instance.Use(path, PUT, handler)
}

func (instance *Server) Delete(path string, handler RequestHandler) {
	instance.Use(path, DELETE, handler)
}

func (instance *Server) Patch(path string, handler RequestHandler) {
	instance.Use(path, PATCH, handler)
}

func (instance *Server) Options(path string, handler RequestHandler) {
	instance.Use(path, OPTIONS, handler)
}

func (instance *Server) Head(path string, handler RequestHandler) {
	instance.Use(path, HEAD, handler)
}

func (instance *Server) Listen(port int, observer chan struct{}) error {
	// Start tracing
	_, span := otel.Tracer(traceProviderName).Start(instance.context, "Listen")
	// End tracing

	if instance.serving {
		log.Fatal("Server is already running, called Listen() twice")
	}
	instance.serving = true
	mux := http.NewServeMux()
	for path := range instance.router.paths {
		localPath := path // Create a local copy of the path variable
		mux.HandleFunc(localPath, func(w http.ResponseWriter, r *http.Request) {
			// Create a span for the request trace
			c, requestSpan := otel.Tracer(traceProviderName).Start(
				context.Background(), // New context because the request traces should be separate from the server management trace
				fmt.Sprintf("%s %s", r.Method, r.URL.Path),
				trace.WithAttributes(
					attribute.Bool("tls", instance.tls != nil),
					attribute.String("http.request.method", r.Method),
					attribute.Int("http.request.body.size", int(r.ContentLength)),
				),
			)

			wrapper := NewResponseWriter(w)
			instance.runHandlersForPath(c, localPath, wrapper, r)

			// If the response is 1xx, 2xx, or 3xx, set the span status to Error
			if wrapper.StatusCode != nil {
				requestSpan.SetAttributes(attribute.Int("http.response.status_code", *wrapper.StatusCode))
			}
			if *wrapper.StatusCode >= 500 {
				requestSpan.SetStatus(2, "HTTP status code >= 500") // 2 = OLTP Error
			}
			requestSpan.End()
		})
	}
	// Convert the port number to a string and prepend the colon
	portStr := fmt.Sprintf(":%d", port)
	// Start the HTTP(s) server on the specified port
	instance.httpServer = &http.Server{
		Addr:    portStr,
		Handler: mux,
	}
	// Server is about to start listening, close trace span and close any observers
	span.End()
	close(observer)
	var err error
	if instance.tls != nil {
		err = instance.httpServer.ListenAndServeTLS(instance.tls.CertFilePath, instance.tls.KeyFilePath)
	} else {
		err = instance.httpServer.ListenAndServe()
	}
	instance.serving = false
	return err
}

func (instance *Server) Shutdown(willRestart bool) error {
	// Start tracing
	c, span := otel.Tracer(traceProviderName).Start(instance.context, "Shutdown")

	if !instance.serving {
		fmt.Println("Server is not running, called Shutdown() on a stopped server")
		span.End()
		return nil
	}
	if instance.shuttingDown {
		fmt.Println("Server is already shutting down, called Shutdown() twice")
		span.End()
		return nil
	}
	instance.shuttingDown = true
	if instance.httpServer != nil {
		fmt.Println("...Shutting down server...")
		err := instance.httpServer.Shutdown(c)
		if err != nil && err != http.ErrServerClosed {
			span.End()
			return err
		}
	}
	instance.shuttingDown = false
	span.End()
	if !willRestart {
		// If the server is not going to be restarted, run any additional cleanup functions
		for _, cleanup := range instance.additionalCleanup {
			err := cleanup(instance.context)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (instance *Server) runHandlersForPath(ctx context.Context, path string, w *ResponseWriter, r *http.Request) {
	// Start tracing
	c, span := otel.Tracer(traceProviderName).Start(ctx, "runHandlersForPath")
	defer span.End()
	// End tracing

	// Run global handlers before the route handlers
	err := instance.runGlobalHandlers(c, path, w, r, true)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Printf("GlobalHandlers:Before:Error: %v", err)
		return
	}
	// Get the route for the path and method
	route := instance.router.trie.Get(path)
	if route == nil || route.(Route)[HTTPMethod(r.Method)] == nil || len(route.(Route)[HTTPMethod(r.Method)]) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	// Run the route handlers
	nextCalled := false
	for _, handler := range route.(Route)[HTTPMethod(r.Method)] {
		err := handler(c, w, r, func() {
			nextCalled = true
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Printf("RouteHandlers:Error: %v", err)
			return
		}
		if !nextCalled {
			break
		}
	}
	// Run global handlers after the route handlers
	err = instance.runGlobalHandlers(c, path, w, r, false)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Printf("GlobalHandlers:After:Error %v", err)
		return
	}
	// If no response was sent, send a default response
	if w.StatusCode == nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Printf("Error: Server did not send response for path %s", path)
	}
}

func (instance *Server) runGlobalHandlers(ctx context.Context, path string, w *ResponseWriter, r *http.Request, before bool) error {
	// Start tracing
	_, span := otel.Tracer(traceProviderName).Start(ctx, "runGlobalHandlers")
	defer span.End()
	// End tracing

	var handlers []GlobalHandler
	if before {
		handlers = instance.router.globalHandlers.beforeAll
	} else {
		handlers = instance.router.globalHandlers.afterAll
	}

	nextCalled := false
	for _, handler := range handlers {
		ignored := false
		if handler.options != nil {
			for _, regex := range handler.options.ignoredPathRegexes {
				if regex.MatchString(path) {
					ignored = true
					break
				}
			}
			if ignored {
				continue
			}
		}
		err := handler.handler(ctx, w, r, func() {
			nextCalled = true
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Printf("Error: %v", err)
			return err
		}
		if !nextCalled {
			break
		}
	}
	return nil
}

func validatePath(path string) error {
	fileInfo, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", path)
		}
		return fmt.Errorf("error accessing file: %s, %v", path, err)
	}

	// Check if path is a regular file (not a directory or other type of file)
	if !fileInfo.Mode().IsRegular() {
		return fmt.Errorf("path is not a regular file: %s", path)
	}

	return nil
}
