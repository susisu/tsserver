// Package typeeval handles HTTP requests by evaluating TypeScript types.
//
// An Evaluator holds an in-memory snapshot of an app directory whose entry
// point, index.ts or index.d.ts, must export a generic type `Handle<Req>`.
// For each request
// the evaluator instantiates Handle with the request encoded as a literal
// type, resolves it with the embedded typescript-go compiler, and returns the
// resulting type as a Response.
//
// This package is the only place allowed to import typescript-go internals
// (see go.mod); none of its exported API may leak internal types.
package typeeval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/bundled"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/internal/compiler"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/jsnum"
	"github.com/microsoft/typescript-go/internal/tsoptions"
	"github.com/microsoft/typescript-go/internal/vfs/vfstest"
)

const (
	vfsRoot  = "/app"
	evalFile = vfsRoot + "/__eval.ts"

	maxBodyBytes        = 1 << 20  // 1 MiB; response body extracted from a type
	maxHeaderValueBytes = 16 << 10 // 16 KiB
)

// entryFiles are the accepted entry points, in the order TypeScript resolves
// the "./index" import specifier: index.ts shadows index.d.ts.
var entryFiles = []string{vfsRoot + "/index.ts", vfsRoot + "/index.d.ts"}

// Version reports the TypeScript version of the embedded typescript-go
// compiler (e.g. "7.0.2").
func Version() string {
	return core.Version()
}

// Request is the value handed to the type-level app. Header names are
// lowercased and multiple values are joined with ", " before being embedded
// as string literal types.
type Request struct {
	Method  string
	Path    string
	Query   string
	Headers map[string][]string
	Body    string
}

// Response is the type Handle<Req> resolved to, which must match this
// contract:
//
//	{
//	  status: <number literal>;
//	  headers?: { [name: <string literal>]: <string literal> | [<string literal>, ...] };
//	  body: <string literal>;
//	}
type Response struct {
	Status  int
	Headers map[string][]string
	Body    string
}

// Stats reports program-wide compiler statistics from a single evaluation.
type Stats struct {
	Instantiations int
}

// TypeError reports that instantiating Handle<Req> produced TypeScript
// diagnostics (e.g. the request type does not satisfy Handle's constraint).
type TypeError struct {
	Diagnostics []string
}

func (e *TypeError) Error() string {
	return "typeeval: type error:\n" + strings.Join(e.Diagnostics, "\n")
}

// ContractError reports that Handle<Req> type-checked but did not resolve to
// a type matching the response contract.
type ContractError struct {
	Reason string
}

func (e *ContractError) Error() string {
	return "typeeval: response contract violation: " + e.Reason
}

// Evaluator resolves requests to responses using an immutable snapshot of an
// app. It is safe for concurrent use.
type Evaluator struct {
	files     map[string]string // vfs path -> contents
	entryFile string            // resolved entry point (index.ts or index.d.ts)
}

// New creates an Evaluator from an app snapshot given as vfs-root-relative
// slash-separated paths (e.g. "index.ts", "node_modules/x/index.d.ts").
// It fails fast if the entry point (index.ts or index.d.ts) is missing, has
// type errors, or does not export a type named Handle.
func New(ctx context.Context, files map[string]string) (*Evaluator, error) {
	vfsFiles := make(map[string]string, len(files))
	for rel, contents := range files {
		vfsFiles[vfsRoot+"/"+rel] = contents
	}
	entryFile := ""
	for _, name := range entryFiles {
		if _, ok := vfsFiles[name]; ok {
			entryFile = name
			break
		}
	}
	if entryFile == "" {
		return nil, errors.New("typeeval: app directory has no index.ts or index.d.ts")
	}
	if _, ok := vfsFiles[evalFile]; ok {
		return nil, errors.New("typeeval: app directory must not contain __eval.ts")
	}
	e := &Evaluator{files: vfsFiles, entryFile: entryFile}
	if err := e.probe(ctx); err != nil {
		return nil, err
	}
	return e, nil
}

// probe validates the app without instantiating Handle: the entry point
// itself must be error-free and must export a type named Handle. Constraint
// violations for concrete requests are reported per evaluation instead, since
// an app may legitimately reject some request shapes.
func (e *Evaluator) probe(ctx context.Context) error {
	const probeSrc = "import type { Handle } from \"./index\";\nexport type __Probe = 0;\n"
	program := e.newProgram(probeSrc)
	for _, name := range []string{e.entryFile, evalFile} {
		file := program.GetSourceFile(name)
		if file == nil {
			return fmt.Errorf("typeeval: %s missing from program", name)
		}
		if diags := program.GetSemanticDiagnostics(ctx, file); len(diags) > 0 {
			return &TypeError{Diagnostics: diagStrings(diags)}
		}
	}
	return nil
}

// Evaluate resolves Handle<Req> for the given request. Type-level failures
// are reported as *TypeError or *ContractError. Stats reflects the work done
// up to the point of return, whether or not evaluation succeeded.
func (e *Evaluator) Evaluate(ctx context.Context, req Request) (*Response, Stats, error) {
	program := e.newProgram(buildEvalSource(req))
	stats := func() Stats {
		return Stats{Instantiations: program.InstantiationCount()}
	}
	file := program.GetSourceFile(evalFile)
	if file == nil {
		return nil, stats(), errors.New("typeeval: eval file missing from program")
	}
	if diags := program.GetSemanticDiagnostics(ctx, file); len(diags) > 0 {
		return nil, stats(), &TypeError{Diagnostics: diagStrings(diags)}
	}
	if err := ctx.Err(); err != nil {
		return nil, stats(), err
	}

	c, done := program.GetTypeChecker(ctx)
	defer done()

	for _, stmt := range file.Statements.Nodes {
		if stmt.Kind != ast.KindTypeAliasDeclaration {
			continue
		}
		decl := stmt.AsTypeAliasDeclaration()
		if decl.Name().Text() != "__Res" {
			continue
		}
		res, err := responseFromType(c, c.GetTypeAtLocation(decl.Name()))
		return res, stats(), err
	}
	return nil, stats(), errors.New("typeeval: __Res not found in eval file")
}

func (e *Evaluator) newProgram(evalSrc string) *compiler.Program {
	files := maps.Clone(e.files)
	files[evalFile] = evalSrc
	fs := bundled.WrapFS(vfstest.FromMap(files, true /*useCaseSensitiveFileNames*/))

	opts := core.CompilerOptions{
		Target:            core.ScriptTargetESNext,
		Module:            core.ModuleKindPreserve,
		ModuleResolution:  core.ModuleResolutionKindBundler,
		Strict:            core.TSTrue,
		NoErrorTruncation: core.TSTrue,
	}
	return compiler.NewProgram(compiler.ProgramOptions{
		Config: &tsoptions.ParsedCommandLine{
			ParsedConfig: &core.ParsedOptions{
				FileNames:       []string{evalFile},
				CompilerOptions: &opts,
			},
		},
		Host: compiler.NewCompilerHost(vfsRoot, fs, bundled.LibPath(), nil, nil),
	})
}

// buildEvalSource renders the request as a literal TS type. All dynamic
// strings pass through tsString; nothing request-controlled may reach the
// source unescaped.
func buildEvalSource(req Request) string {
	var b strings.Builder
	b.WriteString("import type { Handle } from \"./index\";\n")
	b.WriteString("type __Req = {\n")
	fmt.Fprintf(&b, "  method: %s;\n", tsString(req.Method))
	fmt.Fprintf(&b, "  path: %s;\n", tsString(req.Path))
	fmt.Fprintf(&b, "  query: %s;\n", tsString(req.Query))
	b.WriteString("  headers: {\n")
	for _, name := range slices.Sorted(maps.Keys(normalizeHeaders(req.Headers))) {
		values := normalizeHeaders(req.Headers)[name]
		fmt.Fprintf(&b, "    %s: %s;\n", tsString(name), tsString(strings.Join(values, ", ")))
	}
	b.WriteString("  };\n")
	fmt.Fprintf(&b, "  body: %s;\n", tsString(req.Body))
	b.WriteString("};\n")
	b.WriteString("export type __Res = Handle<__Req>;\n")
	return b.String()
}

// normalizeHeaders lowercases names, merging values of names that collide.
func normalizeHeaders(headers map[string][]string) map[string][]string {
	normalized := make(map[string][]string, len(headers))
	for _, name := range slices.Sorted(maps.Keys(headers)) {
		lower := strings.ToLower(name)
		normalized[lower] = append(normalized[lower], headers[name]...)
	}
	return normalized
}

// tsString renders s as a TS string literal. JSON string syntax is a subset
// of TS string literal syntax, so json.Marshal is a safe escaper.
func tsString(s string) string {
	out, err := json.Marshal(s)
	if err != nil {
		// json.Marshal of a string never fails; invalid UTF-8 is replaced.
		panic(err)
	}
	return string(out)
}

func diagStrings(diags []*ast.Diagnostic) []string {
	out := make([]string, len(diags))
	for i, d := range diags {
		out[i] = d.String()
	}
	return out
}

func responseFromType(c *checker.Checker, t *checker.Type) (*Response, error) {
	if t.IsUnion() {
		return nil, &ContractError{Reason: "Handle must resolve to a single response object, got a union: " + printType(c, t)}
	}
	res := &Response{}
	seenStatus, seenBody := false, false
	for _, prop := range c.GetPropertiesOfType(t) {
		pt := stripUndefined(c, c.GetTypeOfSymbol(prop))
		switch prop.Name {
		case "status":
			status, err := statusFromType(c, pt)
			if err != nil {
				return nil, err
			}
			res.Status = status
			seenStatus = true
		case "body":
			if !pt.IsStringLiteral() {
				return nil, &ContractError{Reason: "body must be a string literal type, got: " + printType(c, pt)}
			}
			body := pt.AsLiteralType().Value().(string)
			if len(body) > maxBodyBytes {
				return nil, &ContractError{Reason: fmt.Sprintf("body exceeds %d bytes", maxBodyBytes)}
			}
			res.Body = body
			seenBody = true
		case "headers":
			headers, err := headersFromType(c, pt)
			if err != nil {
				return nil, err
			}
			res.Headers = headers
		default:
			return nil, &ContractError{Reason: fmt.Sprintf("unknown response property %q (allowed: status, headers, body)", prop.Name)}
		}
	}
	if !seenStatus || !seenBody {
		return nil, &ContractError{Reason: "response must have status and body properties, got: " + printType(c, t)}
	}
	return res, nil
}

func statusFromType(c *checker.Checker, t *checker.Type) (int, error) {
	if !t.IsNumberLiteral() {
		return 0, &ContractError{Reason: "status must be a number literal type, got: " + printType(c, t)}
	}
	value, ok := t.AsLiteralType().Value().(jsnum.Number)
	if !ok {
		return 0, &ContractError{Reason: "status is not a plain number literal: " + printType(c, t)}
	}
	status := int(value)
	if jsnum.Number(status) != value || status < 100 || status > 599 {
		return 0, &ContractError{Reason: fmt.Sprintf("status must be an integer in 100..599, got %v", value)}
	}
	return status, nil
}

func headersFromType(c *checker.Checker, t *checker.Type) (map[string][]string, error) {
	headers := make(map[string][]string)
	for _, prop := range c.GetPropertiesOfType(t) {
		name := prop.Name
		if !isValidHeaderName(name) {
			return nil, &ContractError{Reason: fmt.Sprintf("invalid header name %q", name)}
		}
		pt := stripUndefined(c, c.GetTypeOfSymbol(prop))
		var values []*checker.Type
		if pt.IsTupleType() {
			values = c.GetTypeArguments(pt)
		} else {
			values = []*checker.Type{pt}
		}
		for _, vt := range values {
			if !vt.IsStringLiteral() {
				return nil, &ContractError{Reason: fmt.Sprintf(
					"header %q must be a string literal or a tuple of string literals, got: %s", name, printType(c, pt))}
			}
			value := vt.AsLiteralType().Value().(string)
			if !isValidHeaderValue(value) {
				return nil, &ContractError{Reason: fmt.Sprintf("invalid value for header %q", name)}
			}
			headers[name] = append(headers[name], value)
		}
	}
	if len(headers) == 0 {
		return nil, nil
	}
	return headers, nil
}

// stripUndefined removes undefined from a union, e.g. the type of an optional
// property under strict mode.
func stripUndefined(c *checker.Checker, t *checker.Type) *checker.Type {
	if !t.IsUnion() {
		return t
	}
	rest := make([]*checker.Type, 0, len(t.Types()))
	for _, u := range t.Types() {
		if u.Flags()&checker.TypeFlagsUndefined == 0 {
			rest = append(rest, u)
		}
	}
	if len(rest) == 1 {
		return rest[0]
	}
	return c.GetUnionType(rest)
}

func printType(c *checker.Checker, t *checker.Type) string {
	return c.TypeToStringEx(t, nil,
		checker.TypeFormatFlagsNoTruncation|checker.TypeFormatFlagsInTypeAlias, nil)
}

func isValidHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r > 0x7f || !(isTokenChar(byte(r))) {
			return false
		}
	}
	return true
}

func isTokenChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	}
	return strings.IndexByte("!#$%&'*+-.^_`|~", b) >= 0
}

func isValidHeaderValue(value string) bool {
	if len(value) > maxHeaderValueBytes {
		return false
	}
	for i := 0; i < len(value); i++ {
		b := value[i]
		if b < 0x20 && b != '\t' || b == 0x7f {
			return false
		}
	}
	return true
}
