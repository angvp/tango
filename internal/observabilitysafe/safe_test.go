package observabilitysafe

import "testing"

func TestCallSwallowsOnlyDependencyPanic(t *testing.T) {
	called := false
	Call(func() {
		called = true
		panic("broken recorder")
	})
	if !called {
		t.Fatal("Call did not invoke dependency")
	}
}
