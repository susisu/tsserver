# tsserver

An HTTP server for web applications implemented in the TypeScript type system.

The app is a directory of type definitions whose entry point (`index.ts`, or `index.d.ts`)
exports `type Handle<Req>`. For every request, tsserver encodes the request as a literal type,
resolves `Handle<Req>` with the embedded [typescript-go](https://github.com/microsoft/typescript-go)
compiler, and sends the resolved type as the HTTP response.

## Usage

```console
$ make build
$ ./tsserver ./example          # serve the app in ./example on :8080
$ PORT=3000 ./tsserver ./example
$ LOG_LEVEL=debug ./tsserver ./example
```

Environment variables: `PORT` (default `8080`), `LOG_LEVEL` (default `info`; `debug`, `warn`, `error`).

### Writing an app

```ts
export type Handle<Req> = Req extends { method: "GET"; path: "/" }
  ? { status: 200; headers: { "content-type": "text/plain; charset=utf-8" }; body: "hello\n" }
  : { status: 404; body: "not found\n" };
```

`Req` has the shape

```ts
{
  method: string;
  path: string;
  query: string;                       // raw query string, without "?"
  headers: { [name: string]: string }; // names lowercased, repeated values joined with ", "
  body: string;
}
```

`Handle<Req>` must resolve to a single object type (no unions) of the shape

```ts
{
  status: number;                                   // integer literal in 100..599
  headers?: { [name: string]: string | string[] };  // tuple values emit the header once per element
  body: string;                                     // string literal
}
```

Type errors and contract violations are logged and answered with `500 Internal Server Error`.
`Content-Type` defaults to `text/plain; charset=utf-8` when the app does not set one.

Only `*.ts` / `*.mts` / `*.cts` and `package.json` files are loaded from the app directory
(symlinks followed, dotfiles skipped), so `node_modules` type packages work. The app directory
must not contain a file named `__eval.ts`.

## Development

```console
$ make build  # builds the tsserver binary
$ make test   # runs tests for both Go modules (./ and ./typeeval)
$ make fmt
```

`typeeval/` is a separate Go module deliberately named
`github.com/microsoft/typescript-go/_tsserver/typeeval` so that it may import typescript-go's
internal compiler API; the root module wires it in with a `replace` directive.

## License

[MIT License](http://opensource.org/licenses/mit-license.php)

## Author

Susisu ([GitHub](https://github.com/susisu), [Twitter](https://twitter.com/susisu2413))
