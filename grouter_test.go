package grouter

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"testing"
)

const (
	port    = 8080
	tlsPort = 8443
)

var testingContext = context.Background()

func TestSingletonServer(t *testing.T) {
	server := GetServer(&testingContext, nil)
	anotherServer := GetServer(&testingContext, nil)

	if server != anotherServer {
		t.Errorf("Expected server to be a singleton")
	}
	oldRouter := server.router
	anotherServer = server.SetRouter(NewRouter(testingContext))

	if oldRouter == anotherServer.router || server != anotherServer {
		t.Errorf("Expected router change to not change server instance")
	}
}

func TestServerUse(t *testing.T) {
	server := GetServer(&testingContext, nil).SetRouter(NewRouter(testingContext))
	server.Use("/test", GET, func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		next()
		return nil
	})

	route := server.router.trie.Get("/test")
	if route == nil || len(route.(Route)[GET]) != 1 {
		t.Errorf("Expected GET \"/test\" to be initialized")
	}

	server.Use("/test", GET, func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		next()
		return nil
	})

	route = server.router.trie.Get("/test")
	if route == nil || len(route.(Route)[GET]) != 2 {
		t.Errorf("Expected \"/test\" to be initialized")
	}

	if len(route.(Route)[POST]) > 0 {
		t.Errorf("Expected POST \"/test\" to not be initialized")
	}
}

func TestRoot(t *testing.T) {
	server := GetServer(&testingContext, nil).SetRouter(NewRouter(testingContext))
	server.Use("/", GET, func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		next()
		return nil
	})

	route := server.router.trie.Get("/")
	if route == nil || len(route.(Route)[GET]) != 1 {
		t.Errorf("Expected GET \"/\" to be initialized")
	}
}

func TestHandlersForPath(t *testing.T) {
	server := GetServer(&testingContext, nil).SetRouter(NewRouter(testingContext))
	tracker := make(map[string]struct{})
	server.Use("/test", GET, func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		tracker["first"] = struct{}{}
		next()
		return nil
	})
	server.Use("/test", GET, func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		tracker["second"] = struct{}{}
		w.WriteHeader(http.StatusOK)
		next()
		return nil
	})

	GetClient(server, port, false, true, func(client *http.Client) {
		res, err := client.Get(fmt.Sprintf("http://localhost:%d/test", port))
		if err != nil {
			t.Fatal(err)
		}

		if res.StatusCode != http.StatusOK {
			t.Errorf("Expected status code 200, got %d", res.StatusCode)
		}
		if _, exists := tracker["first"]; !exists {
			t.Errorf("Expected first handler to be called")
		}
		if _, exists := tracker["second"]; !exists {
			t.Errorf("Expected second handler to be called")
		}
	})
}

func TestHandlersForPathNoHandlers(t *testing.T) {
	server := GetServer(&testingContext, nil).SetRouter(NewRouter(testingContext))

	GetClient(server, port, false, true, func(client *http.Client) {
		res, err := client.Get(fmt.Sprintf("http://localhost:%d/test", port))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status code 404, got %d", res.StatusCode)
		}
	})
}

func TestHandlersForPathNoHandlersForMethod(t *testing.T) {
	server := GetServer(&testingContext, nil).SetRouter(NewRouter(testingContext))
	server.Use("/test", GET, func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		next()
		return nil
	})

	GetClient(server, port, false, true, func(client *http.Client) {
		res, err := client.Post(fmt.Sprintf("http://localhost:%d/test", port), "text/plain", nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status code 404, got %d", res.StatusCode)
		}
	})
}

func TestHandlersForPathNoHandlersForPath(t *testing.T) {
	server := GetServer(&testingContext, nil).SetRouter(NewRouter(testingContext))
	server.Use("/test", GET, func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		next()
		return nil
	})

	GetClient(server, port, false, true, func(client *http.Client) {
		res, err := client.Get(fmt.Sprintf("http://localhost:%d/test2", port))
		if err != nil {
			t.Fatal(err)
		}

		if res.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status code 404, got %d", res.StatusCode)
		}
	})
}

func TestHandlersForPathNoHandlersForPathNoHandlersForMethod(t *testing.T) {
	server := GetServer(&testingContext, nil).SetRouter(NewRouter(testingContext))
	server.Use("/test", GET, func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		next()
		return nil
	})

	GetClient(server, port, false, true, func(client *http.Client) {
		res, err := client.Post(fmt.Sprintf("http://localhost:%d/test2", port), "text/plain", nil)
		if err != nil {
			t.Fatal(err)
		}

		if res.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status code 404, got %d", res.StatusCode)
		}
	})
}

func TestHandlersForPathNoResponse(t *testing.T) {
	server := GetServer(&testingContext, nil).SetRouter(NewRouter(testingContext))
	server.Use("/test", GET, func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		next()
		return nil
	})

	GetClient(server, port, false, true, func(client *http.Client) {
		res, err := client.Get(fmt.Sprintf("http://localhost:%d/test", port))
		if err != nil {
			t.Fatal(err)
		}

		if res.StatusCode != http.StatusInternalServerError {
			t.Errorf("Expected status code 500, got %d", res.StatusCode)
		}
	})
}

func TestGlobalHandlers(t *testing.T) {
	server := GetServer(&testingContext, nil).SetRouter(NewRouter(testingContext))
	tracker := make(map[string]int)
	server.UseGlobal(func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		tracker["before"] = 1
		next()
		return nil
	}, nil)
	server.UseGlobal(func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		tracker["before"]++
		next()
		return nil
	}, &GlobalRouteOptions{
		afterAll:           false,
		ignoredPathRegexes: []string{"/test2"},
	})
	server.Use("/test", GET, func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		tracker["test"] = 1
		next()
		return nil
	})
	server.UseGlobal(func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		tracker["after"] = 1
		w.WriteHeader(http.StatusOK)
		next()
		return nil
	}, &GlobalRouteOptions{
		afterAll:           true,
		ignoredPathRegexes: nil,
	})

	GetClient(server, port, false, true, func(client *http.Client) {
		res, err := client.Get(fmt.Sprintf("http://localhost:%d/test", port))
		if err != nil {
			t.Fatal(err)
		}

		if res.StatusCode != http.StatusOK {
			t.Errorf("Expected status code 200, got %d", res.StatusCode)
		}
		if val := tracker["before"]; val != 2 {
			t.Errorf("Expected before handler to be called twice")
		}
		if val := tracker["test"]; val != 1 {
			t.Errorf("Expected test handler to be called")
		}
		if val := tracker["after"]; val != 1 {
			t.Errorf("Expected after handler to be called")
		}
	})
}

func TestGlobalHandlersIgnoredPaths(t *testing.T) {
	server := GetServer(&testingContext, nil).SetRouter(NewRouter(testingContext))
	tracker := make(map[string]int)
	server.UseGlobal(func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		tracker["before"] = 1
		next()
		return nil
	}, nil)
	server.UseGlobal(func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		tracker["before"]++
		next()
		return nil
	}, &GlobalRouteOptions{
		afterAll:           false,
		ignoredPathRegexes: []string{"/test"},
	})
	server.Use("/test", GET, func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		tracker["test"] = 1
		next()
		return nil
	})
	server.UseGlobal(func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		tracker["after"] = 1
		w.WriteHeader(http.StatusOK)
		next()
		return nil
	}, &GlobalRouteOptions{
		afterAll:           true,
		ignoredPathRegexes: nil,
	})

	GetClient(server, port, false, true, func(client *http.Client) {
		res, err := client.Get(fmt.Sprintf("http://localhost:%d/test", port))
		if err != nil {
			t.Fatal(err)
		}

		if res.StatusCode != http.StatusOK {
			t.Errorf("Expected status code 200, got %d", res.StatusCode)
		}
		if val := tracker["before"]; val != 1 {
			t.Errorf("Expected before handler to be called once")
		}
		if val := tracker["test"]; val != 1 {
			t.Errorf("Expected test handler to be called")
		}
		if val := tracker["after"]; val != 1 {
			t.Errorf("Expected after handler to be called")
		}
	})
}

func TestGlobalHandlersCorrectOrder(t *testing.T) {
	server := GetServer(&testingContext, nil).SetRouter(NewRouter(testingContext))
	tracker := []string{}
	server.UseGlobal(func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		tracker = append(tracker, "before")
		next()
		return nil
	}, nil)
	server.Use("/test", GET, func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		tracker = append(tracker, "test")
		next()
		return nil
	})
	server.UseGlobal(func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		tracker = append(tracker, "after")
		w.WriteHeader(http.StatusOK)
		next()
		return nil
	}, &GlobalRouteOptions{
		afterAll:           true,
		ignoredPathRegexes: nil,
	})

	GetClient(server, port, false, true, func(client *http.Client) {
		res, err := client.Get(fmt.Sprintf("http://localhost:%d/test", port))
		if err != nil {
			t.Fatal(err)
		}

		if res.StatusCode != http.StatusOK {
			t.Errorf("Expected status code 200, got %d", res.StatusCode)
		}
		if len(tracker) != 3 {
			t.Errorf("Expected 3 handlers to be called")
		}
		expected := []string{"before", "test", "after"}
		for i := 0; i < len(tracker); i++ {
			if tracker[i] != expected[i] {
				t.Errorf("Expected %s handler to be called", expected[i])
			}
		}
	})
}

func TestTLSServer(t *testing.T) {
	server := GetServer(&testingContext, &TLSConfig{
		CertFilePath: "test.cert.pem",
		KeyFilePath:  "test.key.pem",
	}).SetRouter(NewRouter(testingContext))
	server.Use("/test", GET, func(ctx context.Context, w *ResponseWriter, r *http.Request, next func()) error {
		_, err := w.Write([]byte("Hello, world!"))
		if err != nil {
			return err
		}
		next()
		return nil
	})

	GetClient(server, tlsPort, true, false, func(client *http.Client) {
		res, err := client.Get(fmt.Sprintf("https://localhost:%d/test", tlsPort))
		if err != nil {
			t.Fatalf("Failed to connect to server: %v", err)
		}

		if res.StatusCode != 200 {
			t.Errorf("Expected status code 200, got %d", res.StatusCode)
		}
	})
}

func GetClient(server *Server, p int, useTLS bool, willRestart bool, testFunc func(*http.Client)) {
	// Channel to signal when the server has started
	started, ended := make(chan struct{}), make(chan struct{})
	go func() {
		// Start the server in a separate goroutine
		err := server.Listen(p, started)
		if err != nil && err != http.ErrServerClosed {
			fmt.Println(err)
		}
		// Signal that the server has stopped
		close(ended)
	}()
	// Wait for the server to start
	<-started

	var client *http.Client
	if useTLS {
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client = &http.Client{Transport: transport}
	} else {
		client = &http.Client{}
	}
	testFunc(client)

	if err := server.Shutdown(willRestart); err != nil {
		fmt.Println(err)
	}
	<-ended // Wait for the server to stop
}
