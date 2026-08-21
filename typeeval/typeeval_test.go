package typeeval_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/microsoft/typescript-go/_tsserver/typeeval"
)

func load(t *testing.T, dir string) *typeeval.Evaluator {
	t.Helper()
	ev, err := typeeval.Load(context.Background(), "testdata/"+dir)
	if err != nil {
		t.Fatalf("Load(%q): %v", dir, err)
	}
	return ev
}

func evaluate(t *testing.T, ev *typeeval.Evaluator, req typeeval.Request) *typeeval.Response {
	t.Helper()
	res, stats, err := ev.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("Evaluate(%+v): %v", req, err)
	}
	if stats.Instantiations <= 0 {
		t.Errorf("stats.Instantiations = %d, want > 0", stats.Instantiations)
	}
	return res
}

func get(path string) typeeval.Request {
	return typeeval.Request{Method: "GET", Path: path}
}

func TestRouting(t *testing.T) {
	ev := load(t, "basic")

	res := evaluate(t, ev, get("/nowhere"))
	if res.Status != 404 || res.Body != "not found\n" {
		t.Errorf("got %d %q, want 404 %q", res.Status, res.Body, "not found\n")
	}

	res = evaluate(t, ev, get("/greet/alice"))
	if res.Status != 200 || res.Body != "Hello, alice!" {
		t.Errorf("got %d %q, want 200 %q", res.Status, res.Body, "Hello, alice!")
	}
	if got := res.Headers["content-type"]; !reflect.DeepEqual(got, []string{"text/plain; charset=utf-8"}) {
		t.Errorf("content-type = %v", got)
	}
}

func TestPathEscaping(t *testing.T) {
	ev := load(t, "basic")
	res := evaluate(t, ev, get("/greet/a\"b\\c\nd`e${f}"))
	if want := "Hello, a\"b\\c\nd`e${f}!"; res.Body != want {
		t.Errorf("body = %q, want %q", res.Body, want)
	}
}

func TestRepeatedResponseHeader(t *testing.T) {
	ev := load(t, "basic")
	res := evaluate(t, ev, typeeval.Request{Method: "POST", Path: "/login"})
	if want := []string{"session=abc", "visited=true"}; !reflect.DeepEqual(res.Headers["set-cookie"], want) {
		t.Errorf("set-cookie = %v, want %v", res.Headers["set-cookie"], want)
	}
}

func TestBodyEcho(t *testing.T) {
	ev := load(t, "basic")
	res := evaluate(t, ev, typeeval.Request{Method: "POST", Path: "/echo", Body: "hello type system"})
	if res.Body != "hello type system" {
		t.Errorf("body = %q", res.Body)
	}
}

func TestRequestHeaders(t *testing.T) {
	ev := load(t, "basic")

	// Names are lowercased before matching.
	res := evaluate(t, ev, typeeval.Request{
		Method:  "GET",
		Path:    "/",
		Headers: map[string][]string{"X-Magic": {"wand"}},
	})
	if want := "magic: wand"; res.Body != want {
		t.Errorf("body = %q, want %q", res.Body, want)
	}

	// Repeated request headers are joined with ", ".
	res = evaluate(t, ev, typeeval.Request{
		Method:  "GET",
		Path:    "/",
		Headers: map[string][]string{"X-Magic": {"wand", "hat"}},
	})
	if want := "magic: wand, hat"; res.Body != want {
		t.Errorf("body = %q, want %q", res.Body, want)
	}
}

func TestConstraintViolationIsTypeError(t *testing.T) {
	ev := load(t, "strict")
	_, stats, err := ev.Evaluate(context.Background(), get("/"))
	var typeErr *typeeval.TypeError
	if stats.Instantiations <= 0 {
		t.Errorf("stats.Instantiations = %d, want > 0 even on failure", stats.Instantiations)
	}
	if !errors.As(err, &typeErr) {
		t.Fatalf("got %v, want *TypeError", err)
	}
}

func TestNonLiteralBodyIsContractError(t *testing.T) {
	ev := load(t, "badcontract")
	_, _, err := ev.Evaluate(context.Background(), get("/"))
	var contractErr *typeeval.ContractError
	if !errors.As(err, &contractErr) {
		t.Fatalf("got %v, want *ContractError", err)
	}
}

func TestLoadRequiresEntry(t *testing.T) {
	if _, err := typeeval.Load(context.Background(), t.TempDir()); err == nil {
		t.Fatal("Load of empty dir should fail")
	}
}
