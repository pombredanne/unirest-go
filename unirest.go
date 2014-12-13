// Copyright 2014 Nijiko Yonskai, Mashape Inc, All rights reserved.
// Source code is governed by MIT
//
// Source based on native http and goreq

package unirest

import (
  "bufio"
  "bytes"
  "compress/flate"
  "compress/gzip"
  "compress/zlib"
  "crypto/tls"
  "encoding/json"
  "errors"
  "fmt"
  "io"
  "io/ioutil"
  "net"
  "net/http"
  "net/url"
  "reflect"
  "strings"
  "time"
)

// Unirest Request represents the HTTP request being sent by
// a client, or a request to be recieved by the server.
type Request struct {
  // HTTP Request Method (e.g. GET, POST, PUT, PATCH, DELETE, etc...)
  Method            string

  // HTTP Request Url
  Url               string

  // HTTP Request Headers
  //
  // HTTP defines that header names are case-insensitive.
  // The request parser implements this by canonicalizing the
  // name, making the first character and any characters
  // following a hyphen uppercase and the rest lowercase.
  //
  // For client requests certain headers are automatically
  // added and may override values in Header.
  //
  // See the documentation for the Request.end method
  Headers           []Header

  // HTTP Request Header Sugar
  // During Request.end the value is appended to the http.Client headers list.
  ContentType       string

  // HTTP Request Header Sugar
  // During Request.end the value is appended to the http.Client headers list.
  Accept            string

  // HTTP Request Header Sugar
  // During Request.end the value is appended to the http.Client headers list.
  Host              string

  // HTTP Request Header Sugar
  // During Request.end the value is appended to the http.Client headers list.
  UserAgent         string

  // HTTP Request body.
  //
  // Supports string, Reader, or interface{}
  //
  // For requests a nil body means the request has no body, such as
  // GET requests. The HTTP Client is responsible for closing the body.
  Body              interface{}

  // HTTP Compression
  //
  // Transparent decompression of the request and response provided they
  // have the matching Content-Encoding header to the passed Compression
  // type.
  //
  // See unirest.Gzip, unirest.Deflate, and unirest.Zlib
  Compression       *Compression

  // HTTP Request Form Body
  //
  // The request form body contains form-data which is transformed and
  // placed on the Body
  Form              url.Values

  // HTTP Request Multipart Form Body
  //
  // Contains both form-data and stream information. The HTTP Client is
  // responsible for closing these fields.
  //
  //MultipartForm     *Multipartform

  // HTTP Request Querystring
  //
  // Contains key-value data which is converted to string during paramParse
  Querystring       url.Values

  // HTTP Request Timeout
  //
  // By default there is no timeout, which means it will wait forever.
  Timeout           time.Duration

  // HTTP Request TLS Insecure
  //
  // Controls whether the TLS transport should verify the server's certificate
  // chain and host name or not.
  //
  // Should the transport be insecure, setting the Insecure flag to true on the
  // Request changes the transport TLS config to set the flag insecureSkipVerify
  // to false skipping any verification in the TLS transport, accepting any
  // certificate presented by the server and any host name in that certificate.
  //
  // In this mode, TLS is susceptible to man-in-the-middle attacks.
  Insecure          bool

  // HTTP Request Maximum Redirects to Follow
  //
  // The maximum number of allowed redirects the Request is able to follow before
  // exiting.
  MaxRedirects      int

  // Http Request Copy Headers on Redirect
  //
  // When enabled this means the Request will copy the Headers for each request when
  // redirecting.
  RedirectHeaders   bool

  // HTTP Request Proxy
  //
  // URI for Proxy location, url authentication also supported (e.g. http://user:pass@proxy:port)
  Proxy             string

  // HTTP Request Basic Authentication Username
  BasicAuthUsername string

  // HTTP Request Basic Authentication Password
  BasicAuthPassword string
}

type Compression struct {
  writer          func(buffer io.Writer) (io.WriteCloser, error)
  reader          func(buffer io.Reader) (io.ReadCloser, error)
  ContentEncoding string
}

type Response struct {
  StatusCode    int
  ContentLength int64
  Body          *Body
  Header        http.Header
}

type Header struct {
  name  string
  value string
}

type Body struct {
  reader           io.ReadCloser
  compressedReader io.ReadCloser
}

type Error struct {
  timeout bool
  Err     error
}

func (e *Error) Timeout() bool {
  return e.timeout
}

func (e *Error) Error() string {
  return e.Err.Error()
}

func (b *Body) Read(p []byte) (int, error) {
  if b.compressedReader != nil {
    return b.compressedReader.Read(p)
  }

  return b.reader.Read(p)
}

func (b *Body) Close() error {
  err := b.reader.Close()

  if b.compressedReader != nil {
    return b.compressedReader.Close()
  }

  return err
}

func (b *Body) FromJsonTo(o interface{}) error {
  if body, err := ioutil.ReadAll(b); err != nil {
    return err
  } else if err := json.Unmarshal(body, o); err != nil {
    return err
  }

  return nil
}

func (b *Body) String() (string, error) {
  body, err := ioutil.ReadAll(b)
  if err != nil {
    return "", err
  }

  return string(body), nil
}

func Gzip() *Compression {
  reader := func(buffer io.Reader) (io.ReadCloser, error) {
    return gzip.NewReader(buffer)
  }

  writer := func(buffer io.Writer) (io.WriteCloser, error) {
    return gzip.NewWriter(buffer), nil
  }

  return &Compression{writer: writer, reader: reader, ContentEncoding: "gzip"}
}

func Deflate() *Compression {
  reader := func(buffer io.Reader) (io.ReadCloser, error) {
    return flate.NewReader(buffer), nil
  }

  writer := func(buffer io.Writer) (io.WriteCloser, error) {
    return flate.NewWriter(buffer, -1)
  }

  return &Compression{writer: writer, reader: reader, ContentEncoding: "deflate"}
}

func Zlib() *Compression {
  reader := func(buffer io.Reader) (io.ReadCloser, error) {
    return zlib.NewReader(buffer)
  }

  writer := func(buffer io.Writer) (io.WriteCloser, error) {
    return zlib.NewWriter(buffer), nil
  }

  return &Compression{writer: writer, reader: reader, ContentEncoding: "deflate"}
}

func parseStructToUrlValue(query interface{}) (url.Value, error) {
  var (
    v = &url.Values{}
    s = reflect.ValueOf(query)
    t = reflect.TypeOf(query)
  )

  for i := 0; i < s.NumField(); i++ {
    v.Add(strings.ToLower(t.Field(i).Name), fmt.Sprintf("%v", s.Field(i).Interface()))
  }

  return v, nil
}

func paramParse(query url.Value) (string, error) {
  return query.Encode(), nil
}

func prepareRequestBody(b interface{}) (io.Reader, error) {
  switch b.(type) {

  // String
  case string:
    return strings.NewReader(b.(string)), nil

  // Text
  case io.Reader:
    return b.(io.Reader), nil

  // Byte array
  case []byte:
    return bytes.NewReader(b.([]byte)), nil

  // Empty
  case nil:
    return nil, nil

  // Attempt to parse as JSON
  default:
    j, err := json.Marshal(b)
    if err == nil {
      return bytes.NewReader(j), nil
    }

    return nil, err
  }
}

var defaultDialer = &net.Dialer{Timeout: 1000 * time.Millisecond}
var defaultTransport = &http.Transport{Dial: defaultDialer.Dial, Proxy: http.ProxyFromEnvironment}
var defaultClient = &http.Client{Transport: defaultTransport}
var proxyTransport *http.Transport
var proxyClient *http.Client

func SetConnectTimeout(duration time.Duration) {
  defaultDialer.Timeout = duration
}

func (r *Request) Header(name string, value string) {
  if r.Headers == nil {
    r.Headers = []Header{}
  }

  r.Headers = append(r.Headers, Header{name: name, value: value})
}

func (r *Request) HeaderStruct(header Header) {
  if r.Headers == nil {
    r.Headers = []Header{}
  }

  r.Headers = append(r.Headers, header)
}

func (r Request) End() (*Response, error) {
  var req *http.Request
  var er error
  var transport = defaultTransport
  var client = defaultClient
  var redirectFailed bool

  // Retrieve method value, or fallback to GET
  r.Method = fallbackValue(r.Method, "GET")

  // Setup client Proxy
  if r.Proxy != "" {
    proxyUrl, err := url.Parse(r.Proxy)
    if err != nil {
      // Proxy address incorrect format
      return nil, &Error{Err: err}
    }

    if proxyTransport == nil {
      proxyTransport = &http.Transport{Dial: defaultDialer.Dial, Proxy: http.ProxyURL(proxyUrl)}
      proxyClient = &http.Client{Transport: proxyTransport}
    } else {
      proxyTransport.Proxy = http.ProxyURL(proxyUrl)
    }

    transport = proxyTransport
    client = proxyClient
  }

  // Determine redirect
  client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
    if len(via) > r.MaxRedirects {
      redirectFailed = true
      return errors.New("Error redirecting. MaxRedirects reached")
    }

    // By default Go will not redirect request headers
    // https://code.google.com/p/go/issues/detail?id=4800&q=request%20header
    if r.RedirectHeaders {
      for key, val := range via[0].Header {
        req.Header[key] = val
      }
    }

    return nil
  }

  // Check transport to determine skipping verification check
  if r.Insecure {
    transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
  } else if transport.TLSClientConfig != nil {
    transport.TLSClientConfig.InsecureSkipVerify = false
  }

  // Parse request body
  b, e := prepareRequestBody(r.Body)
  if e != nil {
    // Error parsing request body
    return nil, &Error{Err: e}
  }

  // Parse query parameters
  if r.Querystring != nil {
    param, e := paramParse(r.Querystring)
    if e != nil {
      return nil, &Error{Err: e}
    }

    r.Url = r.Url + "?" + param
  }

  // Read the body
  var bodyReader io.Reader
  if b != nil && r.Compression != nil {
    buffer := bytes.NewBuffer([]byte{})
    readBuffer := bufio.NewReader(b)
    writer, err := r.Compression.writer(buffer)
    if err != nil {
      return nil, &Error{Err: err}
    }

    _, e = readBuffer.WriteTo(writer)
    writer.Close()
    if e != nil {
      return nil, &Error{Err: e}
    }

    bodyReader = buffer
  } else {
    bodyReader = b
  }

  // Initialize request
  req, er = http.NewRequest(r.Method, r.Url, bodyReader)
  if er != nil {
    // Error parsing URI
    return nil, &Error{Err: er}
  }

  // Add headers to the request
  req.Host = r.Host
  req.Header.Add("User-Agent", r.UserAgent)
  req.Header.Add("Content-Type", r.ContentType)
  req.Header.Add("Accept", r.Accept)

  if r.Compression != nil {
    req.Header.Add("Content-Encoding", r.Compression.ContentEncoding)
    req.Header.Add("Accept-Encoding", r.Compression.ContentEncoding)
  }

  if r.headers != nil {
    for _, header := range r.Headers {
      req.Header.Add(header.name, header.value)
    }
  }

  if r.BasicAuthUsername != "" {
    req.SetBasicAuth(r.BasicAuthUsername, r.BasicAuthPassword)
  }

  timeout := false
  var timer *time.Timer
  if r.Timeout > 0 {
    timer = time.AfterFunc(r.Timeout, func() {
      transport.CancelRequest(req)
      timeout = true
    })
  }

  res, err := client.Do(req)
  if timer != nil {
    timer.Stop()
  }

  if err != nil {
    if !timeout {
      switch err := err.(type) {
      case *net.OpError:
        timeout = err.Timeout()
      case *url.Error:
        if op, ok := err.Err.(*net.OpError); ok {
          timeout = op.Timeout()
        }
      }
    }

    var response *Response
    if redirectFailed {
      response = &Response{StatusCode: res.StatusCode, ContentLength: res.ContentLength, Header: res.Header, Body: &Body{reader: res.Body}}
    }

    return response, &Error{timeout: timeout, Err: err}
  }

  if r.Compression != nil && strings.Contains(res.Header.Get("Content-Encoding"), r.Compression.ContentEncoding) {
    compressedReader, err := r.Compression.reader(res.Body)
    if err != nil {
      return nil, &Error{Err: err}
    }

    return &Response{StatusCode: res.StatusCode, ContentLength: res.ContentLength, Header: res.Header, Body: &Body{reader: res.Body, compressedReader: compressedReader}}, nil
  } else {
    return &Response{StatusCode: res.StatusCode, ContentLength: res.ContentLength, Header: res.Header, Body: &Body{reader: res.Body}}, nil
  }
}

// When value is empty return fallbackValue argument as a
// fallback value.
func fallbackValue(value, fallbackValue string) string {
  if value != "" {
    return value
  }

  return fallbackValue
}
