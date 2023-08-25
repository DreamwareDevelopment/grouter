package grouter

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"

	"github.com/dghubble/trie"
	"go.opentelemetry.io/otel"
)

type ProxyTarget struct {
	Host string `json:"url"`
	Port string `json:"port"`
}

type ProxyConfig struct {
	Path   string       `json:"path"`
	Method string       `json:"method"`
	Proxy  *ProxyTarget `json:"proxy"`
	Params map[string]string
}

type HTTPMethod string

const (
	GET     HTTPMethod = "GET"
	POST    HTTPMethod = "POST"
	PUT     HTTPMethod = "PUT"
	DELETE  HTTPMethod = "DELETE"
	PATCH   HTTPMethod = "PATCH"
	OPTIONS HTTPMethod = "OPTIONS"
	HEAD    HTTPMethod = "HEAD"
)

type RequestHandler func(context.Context, *ResponseWriter, *http.Request, func()) error
type Route map[HTTPMethod][]RequestHandler
type GlobalRouteOptions struct {
	afterAll           bool
	ignoredPathRegexes []string
}
type concreteGlobalRouteOptions struct {
	afterAll           bool
	ignoredPathRegexes []regexp.Regexp
}
type GlobalHandler struct {
	options *concreteGlobalRouteOptions
	handler RequestHandler
}
type internalGlobalHandlers struct {
	beforeAll []GlobalHandler
	afterAll  []GlobalHandler
}

type Router struct {
	trie           *trie.PathTrie
	paths          map[string]struct{}
	globalHandlers internalGlobalHandlers
	context        context.Context
}

func NewRouter(context context.Context) *Router {
	return &Router{
		trie:  trie.NewPathTrie(),
		paths: make(map[string]struct{}),
		globalHandlers: internalGlobalHandlers{
			beforeAll: []GlobalHandler{},
			afterAll:  []GlobalHandler{},
		},
		context: context,
	}
}

func (instance *Router) UseGlobal(handler RequestHandler, options *GlobalRouteOptions) {
	// Tracing
	var spanName string
	if options != nil && options.afterAll {
		spanName = "UseGlobal:AfterAll"
	} else {
		spanName = "UseGlobal:BeforeAll"
	}
	_, span := otel.Tracer(traceProviderName).Start(instance.context, spanName)
	defer span.End()
	// End tracing

	concreteOptions := convertToConcreteGlobalRouteOptions(options)
	if concreteOptions != nil && concreteOptions.afterAll {
		instance.globalHandlers.afterAll = append(instance.globalHandlers.afterAll, GlobalHandler{
			options: concreteOptions,
			handler: handler,
		})
	} else {
		instance.globalHandlers.beforeAll = append(instance.globalHandlers.beforeAll, GlobalHandler{
			options: concreteOptions,
			handler: handler,
		})
	}
}

func (instance *Router) Use(path string, method HTTPMethod, handler RequestHandler) {
	// Tracing
	_, span := otel.Tracer(traceProviderName).Start(instance.context, fmt.Sprintf("Use %s %s", method, path))
	defer span.End()
	// End tracing

	instance.paths[path] = struct{}{}
	value := instance.trie.Get(path)
	if value == nil {
		value = make(Route)
	}
	if value.(Route)[method] == nil || len(value.(Route)[method]) == 0 {
		value.(Route)[method] = []RequestHandler{}
	}
	value.(Route)[method] = append(value.(Route)[method], handler)
	instance.trie.Put(path, value)
}

// Convenience methods for each HTTP method
func (instance *Router) Get(path string, handler RequestHandler) {
	instance.Use(path, GET, handler)
}

func (instance *Router) Post(path string, handler RequestHandler) {
	instance.Use(path, POST, handler)
}

func (instance *Router) Put(path string, handler RequestHandler) {
	instance.Use(path, PUT, handler)
}

func (instance *Router) Del(path string, handler RequestHandler) {
	instance.Use(path, DELETE, handler)
}

func (instance *Router) Patch(path string, handler RequestHandler) {
	instance.Use(path, PATCH, handler)
}

func (instance *Router) Options(path string, handler RequestHandler) {
	instance.Use(path, OPTIONS, handler)
}

func (instance *Router) Head(path string, handler RequestHandler) {
	instance.Use(path, HEAD, handler)
}

func convertToConcreteGlobalRouteOptions(options *GlobalRouteOptions) *concreteGlobalRouteOptions {
	concrete := concreteGlobalRouteOptions{
		afterAll:           false,
		ignoredPathRegexes: []regexp.Regexp{},
	}
	if options == nil {
		return &concrete
	}
	if options.ignoredPathRegexes != nil && len(options.ignoredPathRegexes) > 0 {
		for _, regex := range options.ignoredPathRegexes {
			compiled, err := regexp.Compile(regex)
			if err != nil {
				log.Fatal(err)
			}
			concrete.ignoredPathRegexes = append(concrete.ignoredPathRegexes, *compiled)
		}
	}
	concrete.afterAll = options.afterAll
	return &concrete
}
