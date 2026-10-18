# HTTP client

Send external HTTP requests using small, tagged operation structs. The client
builds URLs, encodes bodies and runs middleware over native `net/http`. It works
independently of application configuration, modules, databases and workers.

## Send your first request

Define the operation once. Put it in the integration that owns the external API.

```go
type GetProductRequest struct {
    httpclient.Request `method:"GET" path:"products/{id}"`

    ID     string   `path:"id"`
    Fields []string `query:"fields,omitempty"`
}

type ProductResponse struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}
```

Create and reuse a client:

```go
client, err := httpclient.New(httpclient.Config{
    BaseURL: "https://api.example.com/v1/",
    Timeout: 15 * time.Second,
    Header: http.Header{
        "Accept": {"application/json"},
    },
})
if err != nil {
    return err
}
defer client.CloseIdleConnections()

response, err := client.Do(ctx, GetProductRequest{
    ID: "123", Fields: []string{"name", "price"},
})
if err != nil {
    return err
}
if err := response.CheckStatus(); err != nil {
    return err
}

var product ProductResponse
if err := response.DecodeJSON(&product); err != nil {
    return err
}
```

Try the compiled local example in `examples/httpclient/main.go`:

```sh
go run ./examples/httpclient
```

It starts a disposable local provider with sample catalog data. No real
credentials, internet service or application infrastructure are needed.

## Define paths, query parameters and headers

| Declaration | Meaning |
| --- | --- |
| Embedded `httpclient.Request` | Required operation marker; declares `method` and `path` |
| `path:"id"` | Substitute `{id}` with one escaped value |
| `query:"tag"` | Encode a query parameter; slices become repeated keys |
| `header:"Accept-Language"` | Set a request header; replaces matching client defaults |
| `query:"tag,omitempty"` | Omit empty values |
| `header:"Accept-Language,omitempty"` | Omit empty values and retain any client default |
| `body:"json"` | Encode this field as the JSON body |

Tags belong on direct, exported fields. Embed one marker; no registration or
request methods are required. Untagged fields are ignored. Parameter groups are
not recursively flattened. Missing placeholders, duplicate parameter declarations,
conflicting tags and unsupported encodings fail before sending. Tag spelling is
not compiler-checked; exercise new operations with a local test server.

The base URL is an API prefix. Both `products` and `/products` append to
`https://api.example.com/v1/`. Request paths cannot switch hosts, contain query
strings or fragments, or escape the prefix using dot segments. Use query tags
for query parameters and `DoRequest` for a fully specified native URL.

```go
type ListProductsRequest struct {
    httpclient.Request `method:"GET" path:"products"`

    Tags     []string `query:"tag,omitempty"`
    Active   *bool   `query:"active,omitempty"`
    Language string  `header:"Accept-Language,omitempty"`
}
```

`Tags: []string{"sale", "new"}` produces `tag=sale&tag=new`. A nil `Active`
omits the parameter; a pointer to `false` sends `active=false`. Non-pointer zero
values are sent unless `omitempty` is present. Parameters support scalar values,
slices and `encoding.TextMarshaler`, including `time.Time`.

Path values must be nonempty; slashes inside values are escaped. Path, query and
header tags describe serialization, not business validation. Keep validation in
the integration or calling use case. Incoming [HTTP validation](../../docs/requests.md)
remains owned by Huma; this package does not depend on it.

## Send JSON and XML

Put the payload in a separate field, conventionally named `Body`. Only that field
is serialized. Ordinary `json` and `xml` tags and custom marshalers retain their
standard library behavior.

```go
type UpdateProductRequest struct {
    httpclient.Request `method:"PATCH" path:"products/{id}"`

    ID      string `path:"id"`
    IfMatch string `header:"If-Match,omitempty"`
    Body    struct {
        Name      *string `json:"name,omitempty"`
        Published *bool   `json:"published,omitempty"`
    } `body:"json"`
}
```

For partial updates, a nil pointer omits a field, while a pointer to `false`, zero
or an empty string explicitly sends that value. A named payload struct works too.

```go
type SubmitOrderRequest struct {
    httpclient.Request `method:"POST" path:"orders"`
    Body struct {
        XMLName xml.Name `xml:"order"`
        ID      string   `xml:"id"`
    } `body:"xml"`
}
```

The encoder supplies `Content-Type`. Explicit compatible types are accepted,
including `application/merge-patch+json` and UTF-8 charset parameters. Conflicting
types fail before sending. Set `Accept` separately to describe the response you want.

## Submit forms

```go
type CreateContactRequest struct {
    httpclient.Request `method:"POST" path:"contacts"`
    Body struct {
        Email string   `form:"email"`
        Tags  []string `form:"tag,omitempty"`
    } `body:"form"`
}
```

The content type is `application/x-www-form-urlencoded`. Use `url.Values` as the
body for dynamically named fields. Form payloads use `form` tags; untagged fields
are ignored. `form:"-"` excludes a field explicitly.

## Upload files with multipart

```go
type UploadDocumentRequest struct {
    httpclient.Request `method:"POST" path:"documents"`
    Body struct {
        Title string          `form:"title"`
        File  httpclient.File `form:"file"`
    } `body:"multipart"`
}

file, err := os.Open("invoice.pdf")
if err != nil {
    return err
}
defer file.Close() // Also covers a validation failure before the client owns it.

request := UploadDocumentRequest{}
request.Body.Title = "Invoice"
request.Body.File = httpclient.File{
    Filename: "invoice.pdf", ContentType: "application/pdf", Content: file,
}
response, err := client.Do(ctx, request)
```

`File`, `*File` and `[]File` support one, optional or multiple file parts. The client
streams file contents and generates the multipart boundary. Do not supply your own
boundary. `Filename` is metadata; the client never opens filesystem paths for you.

`Build` does not read files or start a producer. Sending starts the stream. Closing
the request body releases the pipe and closable file readers. Readers without a
`Close` method must arrange to unblock their own reads on cancellation. Supply fresh
readers for another upload; multipart streams are not automatically replayable.

## Send text, bytes or a stream

```go
type ImportCSVRequest struct {
    httpclient.Request `method:"POST" path:"imports"`

    ContentType string    `header:"Content-Type"`
    Body        io.Reader `body:"raw"`
}

response, err := client.Do(ctx, ImportCSVRequest{
    ContentType: "text/csv",
    Body: strings.NewReader("name,email\nAlex,alex@example.com\n"),
})
```

Raw bodies accept `string`, `[]byte` or `io.Reader`. Set the content type explicitly
for text, CSV, protobuf or another format; otherwise it defaults to
`application/octet-stream`. Closable raw readers follow native HTTP body ownership.
Known byte/string readers retain native content length and replay support. The
package adds no retry loop; provider idempotency and stream replay remain explicit.

## Write middleware

Middleware is one function receiving the native request and `next`. Headers are
already cloned, so ordinary header edits need no transport wrapper or cloning code.

```go
func addRequestID(request *http.Request, next httpclient.Next) (*http.Response, error) {
    request.Header.Set("X-Request-ID", "example-request")
    return next(request)
}

client, err := httpclient.New(httpclient.Config{
    BaseURL: "https://api.example.com/v1/",
    Middleware: []httpclient.Middleware{addRequestID},
})
```

Run code before and after `next` for timing, logging or response-header inspection:

```go
func observe(request *http.Request, next httpclient.Next) (*http.Response, error) {
    started := time.Now()
    response, err := next(request)
    logging.FromContext(request.Context()).DebugContext(request.Context(),
        "outbound HTTP exchange",
        "headers_duration", time.Since(started),
        "failed", err != nil,
    )
    return response, err
}
```

First registered is first entered and last exited. The chain is assembled on
configuration and shared by concurrent callers. Middleware runs for each exchange,
including redirect hops when you explicitly enable them. Call `next` synchronously,
normally once. Middleware state must be concurrency-safe. Static headers belong in
`Config.Header`; dynamic authentication must restrict credentials to its intended origin.

Both `Do` and `DoRequest` run middleware; `Build` does not. Middleware surrounds
transport execution through response headers. Body consumption and JSON decoding
happen later. Do not read/close a successful response body in middleware unless you
replace it with an equivalent readable body. Replacing a request body also makes
the middleware responsible for the old body and correct replay/length metadata.

## Read responses and handle failures

```go
response.StatusCode                    // Numeric status
response.Successful()                  // Is the HTTP status 2xx?
response.Header.Get("Content-Type")    // Response header
response.ContentType()                 // Media type without charset or other parameters
response.Trailer.Get("X-Checksum")      // Complete after a successful body read
response.Body                          // Retained bytes
response.Text()                        // Retained bytes as text
response.BodyError()                   // Read/limit/close error, also returned by Do
response.DecodeJSON(&result)           // Decode without consuming Body
response.DecodeXML(&result)            // Decode XML without consuming Body
```

`Do` closes the network response body and retains at most `MaxResponseBytes`.
Decoding can be repeated. No JSON envelope or content-type assumption is imposed
on responses. Both decoders reject extra trailing values and refuse bodies whose
read, size limit or close failed, even if the retained prefix looks valid. XML
allows trailing whitespace, comments and processing instructions.

`Successful` and `CheckStatus` classify only the HTTP status. Always check the error
from `Do` first: HTTP 200 with an interrupted body is still an incomplete response.

| Result | Meaning |
| --- | --- |
| Build/transport error | No usable buffered response; preserve cancellation with `errors.Is` |
| HTTP 4xx/5xx | `Do` succeeds; call `CheckStatus` to classify the status |
| `*httpclient.StatusError` | `Response` remains available; error text omits private provider details |
| `httpclient.ErrResponseTooLarge` | Partial response returned; body contains only the configured limit |
| Body read/close error | Response returned with the retained bytes; decoders reject it |
| `*httpclient.DecodeError` | Format and original cause available through `errors.As` / `errors.Is` |
| Empty body, including HTTP 204 | Skip decoding when no payload is expected; decoding returns an error wrapping `io.EOF` |
| Provider error inside HTTP 200 | Integration interprets it, for example GraphQL's `errors` |

### Decode typed or dynamic JSON

Typed response structs are the usual choice. For a dynamic payload, numbers remain
`json.Number` instead of becoming floating-point values that could round a large ID:

```go
var payload map[string]any
if err := response.DecodeJSON(&payload); err != nil {
    return err
}
if number, ok := payload["id"].(json.Number); ok {
    id := number.String() // Keeps the original numeric text, e.g. 9007199254740993.
    _ = id
}
```

Typed `int64`, `float64` and other fields retain normal `encoding/json` behavior.
Unknown JSON fields are accepted for forward compatibility. Use the retained bytes
with a native decoder when an integration needs stricter rules or another format.
Ignore the destination value when decoding fails; standard decoders may partially
populate it before reporting an error.

### Inspect failures without losing the response

```go
response, err := client.Do(ctx, GetProductRequest{ID: "123"})
if err != nil {
    // response can be non-nil after a body read or size-limit failure.
    // Its bytes remain available, but DecodeJSON/DecodeXML will refuse them.
    return err
}
if err := response.CheckStatus(); err != nil {
    var status *httpclient.StatusError
    if errors.As(err, &status) && status.Response.StatusCode == http.StatusNotFound {
        return ErrProductNotFound // Your integration's error.
    }
    return err
}
var product ProductResponse
if err := response.DecodeJSON(&product); err != nil {
    var mismatch *json.UnmarshalTypeError
    if errors.As(err, &mismatch) {
        // The provider changed a field's type; the original cause is preserved.
    }
    return err
}
```

`StatusError` and `DecodeError` messages omit response content. Their inspectable
response and underlying cause can still contain private provider data.

### Read a retry hint

```go
if response.StatusCode == http.StatusTooManyRequests {
    if delay, ok := response.RetryAfter(); ok {
        // Supply delay to your queue or retry policy, if the operation is safe to retry.
        _ = delay
    }
}
```

`RetryAfter` supports seconds and HTTP dates. A valid past date returns zero; the
boolean distinguishes that from a missing, malformed or unrepresentable value.
The client never sleeps or retries automatically.

A transport error after sending does not prove the provider rejected the operation.
Use provider idempotency support for operations you may retry. Headers and bodies
can contain secrets; logging and tracing are opt-in and should record selected fields.

## Stream responses and customize native requests

```go
request, err := client.Build(ctx, GetProductRequest{ID: "123"})
if err != nil {
    return err
}
request.Header.Set("X-Custom", "value")

response, err := client.DoRequest(request)
if err != nil {
    return err
}
defer response.Body.Close()
if response.StatusCode != http.StatusOK {
    return fmt.Errorf("unexpected download status: %d", response.StatusCode)
}
_, err = io.Copy(destination, response.Body)
```

`DoRequest` returns native `*http.Response`. It applies middleware, timeout, cookie
and redirect policies, but does not add default headers or resolve the URL again.
It also accepts a request created directly with `http.NewRequestWithContext`.
Use native fields for Host, ContentLength, trailers and unusual protocol controls;
these cannot be set through header tags. If you abandon a built request, close its
body yourself. After sending successfully, close the returned response body.

## Configure timeouts, transports and runtime changes

| Setting | Default and behavior |
| --- | --- |
| `BaseURL` | Required for tagged requests; optional for native-only use |
| `Header` | Copied defaults for tagged requests |
| `Timeout` | Zero selects 30 seconds, including response-body reading; `-1` disables the client timeout |
| `MaxResponseBytes` | Zero selects 10 MiB; positive limit for `Do`; `DoRequest` streams without this cap |
| `Middleware` | Ordered functions shared by both execution paths |
| `Transport` | Private clone of the standard default transport; explicit custom transports are borrowed |
| `CheckRedirect` | Redirects disabled; nil restores this default |
| `Jar` | Optional native cookie jar |

Configure TLS, proxies, dialing and connection limits on an `http.Transport` before
using it. Reuse clients and transports. The default pool is kept across configuration
updates. Custom transport owners must retire replaced pools after active users and
response bodies finish; Configure does not close borrowed infrastructure.

```go
cfg := client.Config()
cfg.Timeout = 45 * time.Second
cfg.Header.Set("Authorization", "Bearer " + newToken)
if err := client.Configure(cfg); err != nil {
    return err
}
```

`Config` returns copied headers and a copied middleware slice. `Configure` validates
the entire replacement before publishing it; failures retain the last good settings.
Each operation captures one snapshot, including its response limit. In-flight requests
keep their original settings. Serialize concurrent read-modify-write configuration
updates to avoid overwriting another administrator's changes. Borrowed collaborators
and closure state are not deep-copied.

Use a request context for an individual operation's deadline. For long-lived response
streams, choose a suitable timeout or `Timeout: -1` and manage cancellation yourself.
`CloseIdleConnections` releases idle pools without interrupting active requests. It
can also affect other clients sharing an explicitly supplied transport.

### Why the native client is private

Embedding `*http.Client` would expose mutable `Timeout`, `Transport` and `Jar` fields
outside `Configure`. Editing those during requests could race and bypass validated
configuration snapshots. Its promoted `Get` and `Post` methods would also return
native responses without our tagged request building or buffered-body limit: Go
embedding promotes methods but does not make their internal calls dispatch to the
outer `Do` method.

Use `Config` for native transport, cookie and redirect controls, and `DoRequest`
when you want the full native request/response API. An integration can embed our
`*httpclient.Client` directly, as the catalog example below does.

Reuse the existing tracing transport without a second middleware API:

```go
client, err := httpclient.New(httpclient.Config{
    BaseURL: "https://trusted-provider.example/v1/",
    Transport: tracing.NewHTTPTransport(nil, tracer),
})
```

The tracing wrapper propagates context to this deliberately trusted destination.
See [observability](../../docs/observability.md) for provider ownership and shutdown.

## Build an integration

```text
internal/modules/<module>/integrations/catalog/
    client.go       Client construction and credentials
    requests.go     Tagged operations and payloads
    responses.go    Provider response types
```

The compiled REST example in `examples/httpclient/catalog/` has just these three
production files. It demonstrates a fictional catalog API, with no external account
or credentials required to run the local example.

In `client.go`, embed the framework client and set provider defaults once:

```go
type Client struct{ *httpclient.Client }

func New(baseURL, token string) (*Client, error) {
    if baseURL == "" {
        return nil, errors.New("catalog: base URL is required")
    }
    header := http.Header{"Accept": {"application/json"}}
    if token != "" {
        header.Set("Authorization", "Bearer "+token)
    }
    client, err := httpclient.New(httpclient.Config{
        BaseURL: baseURL,
        Header:  header,
    })
    if err != nil {
        return nil, err
    }
    return &Client{Client: client}, nil
}
```

`Do`, `Build`, `Config`, `Configure`, `DoRequest` and `CloseIdleConnections` are
available directly. No forwarding methods or second configuration type are needed.
Embedding fits this example because it adds defaults, with no extra execution
policy. If a provider needs mandatory signing or custom error interpretation,
enforce that in middleware or explicit integration methods.

Keep operations in `requests.go` and payload types in `responses.go`:

```go
// requests.go
type GetProductRequest struct {
    httpclient.Request `method:"GET" path:"products/{id}"`
    ID                 string `path:"id"`
}

type CreateProductRequest struct {
    httpclient.Request `method:"POST" path:"products"`
    Body               CreateProductBody `body:"json"`
}

type CreateProductBody struct {
    Name string `json:"name"`
}

// responses.go
type ProductResponse struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}
```

The call site stays the same for get and create operations:

```go
client, err := catalog.New(baseURL, token)
if err != nil {
    return err
}
defer client.CloseIdleConnections()

response, err := client.Do(ctx, catalog.CreateProductRequest{
    Body: catalog.CreateProductBody{Name: "Keyboard"},
})
if err != nil {
    return err
}
if err := response.CheckStatus(); err != nil {
    return err
}
var product catalog.ProductResponse
if err := response.DecodeJSON(&product); err != nil {
    return err
}
```

Construct integrations in their owning module and pass them explicitly to consumers.
There is no global client registry or app resource entry. Keep maintained provider
SDKs when they already handle specialized protocols correctly, such as Google's
identity-token validation.

## Verify an integration

Use `httptest.NewServer` to inspect the actual URL, headers, body and error handling.
Inject a native transport for a provider whose hostname must stay fixed. Package
tests cover encoding, middleware order, concurrent configuration, cancellation,
bounded bodies and multipart cleanup:

```sh
go test -race ./pkg/httpclient ./examples/httpclient/...
```

Continue with [module construction](../../docs/module-registration.md),
[dependency ownership](../../docs/dependency-injection.md),
[logging](../../docs/logging.md) and [testing HTTP](../../docs/testing-http.md).
