package tango

import "testing"

func TestPathBuildsRouteWithMethodPatternAndView(t *testing.T) {
	called := false
	view := func(*Context) error {
		called = true
		return nil
	}

	route := Path("GET", "/users/{id}/", view)

	if route.Method() != "GET" {
		t.Fatalf("Method() = %q, want %q", route.Method(), "GET")
	}
	if route.Pattern() != "/users/{id}/" {
		t.Fatalf("Pattern() = %q, want %q", route.Pattern(), "/users/{id}/")
	}
	if route.View() == nil {
		t.Fatal("View() is nil")
	}
	if err := route.View()(&Context{}); err != nil {
		t.Fatalf("View() returned error: %v", err)
	}
	if !called {
		t.Fatal("View() did not return the route's view")
	}
}

func TestNameSetsRouteName(t *testing.T) {
	route := Path("GET", "/", func(*Context) error {
		return nil
	}, Name("detail"))

	if route.Name() != "detail" {
		t.Fatalf("Name() = %q, want %q", route.Name(), "detail")
	}
}

func TestPathWithoutNameHasNoName(t *testing.T) {
	route := Path("GET", "/", func(*Context) error {
		return nil
	})

	if route.Name() != "" {
		t.Fatalf("Name() = %q, want no name", route.Name())
	}
}

func TestPathDoesNotInvokeView(t *testing.T) {
	called := false

	Path("GET", "/", func(*Context) error {
		called = true
		return nil
	})

	if called {
		t.Fatal("Path invoked the view")
	}
}

func TestURLsIsPlainRouteList(t *testing.T) {
	urls := URLs{
		Path("GET", "/", func(*Context) error {
			return nil
		}),
	}

	if len(urls) != 1 {
		t.Fatalf("len(urls) = %d, want 1", len(urls))
	}
}
