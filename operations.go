// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package talq

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Operation is one operation of a TALQ API: its OpenAPI operationId, HTTP
// method, path template ("/devices/{deviceAddress}") and the response
// codes the definition lists for it.
type Operation struct {
	ID     string
	Method string
	Path   string
	Codes  []int
}

// Writes reports whether the operation changes something, so that its
// request carries a talqRequestId.
func (o Operation) Writes() bool { return o.Method != http.MethodGet }

// Accepts reports whether the definition lists status code for the
// operation.
func (o Operation) Accepts(code int) bool {
	for _, c := range o.Codes {
		if c == code {
			return true
		}
	}
	return false
}

// Params lists the path parameter names of the operation, in order.
func (o Operation) Params() []string {
	var out []string
	for _, seg := range strings.Split(o.Path, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			out = append(out, seg[1:len(seg)-1])
		}
	}
	return out
}

// Expand fills the path template with params, escaping each value (an
// address may hold "/" or other reserved characters). A missing parameter
// is an error.
func (o Operation) Expand(params map[string]string) (string, error) {
	segs := strings.Split(o.Path, "/")
	for i, seg := range segs {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			name := seg[1 : len(seg)-1]
			v, ok := params[name]
			if !ok || v == "" {
				return "", fmt.Errorf("talq: %s needs the path parameter %s", o.ID, name)
			}
			segs[i] = url.PathEscape(v)
		}
	}
	return strings.Join(segs, "/"), nil
}

// FindOperation returns the operation of ops with the given operationId.
func FindOperation(ops []Operation, id string) (Operation, bool) {
	for _, o := range ops {
		if o.ID == id {
			return o, true
		}
	}
	return Operation{}, false
}

// Match finds the operation of ops that serves method on escapedPath (the
// request path after the API base, as r.URL.EscapedPath gives it) and
// returns its unescaped path parameters. Literal segments win over
// parameters, so "/devices/count" is the count and not a device.
func Match(ops []Operation, method, escapedPath string) (Operation, map[string]string, bool) {
	got := strings.Split(strings.Trim(escapedPath, "/"), "/")
	best, bestLit := -1, -1
	var bestParams map[string]string
	for i, o := range ops {
		if o.Method != method {
			continue
		}
		want := strings.Split(strings.Trim(o.Path, "/"), "/")
		if len(want) != len(got) {
			continue
		}
		params := map[string]string{}
		lit, ok := 0, true
		for j, w := range want {
			if strings.HasPrefix(w, "{") && strings.HasSuffix(w, "}") {
				v, err := url.PathUnescape(got[j])
				if err != nil || v == "" {
					ok = false
					break
				}
				params[w[1:len(w)-1]] = v
				continue
			}
			if w != got[j] {
				ok = false
				break
			}
			lit++
		}
		if ok && lit > bestLit {
			best, bestLit, bestParams = i, lit, params
		}
	}
	if best < 0 {
		return Operation{}, nil, false
	}
	return ops[best], bestParams, true
}

// Response is what Do read besides the body: the status code (a gateway
// may answer 202 and send the result later) and the headers.
type Response struct {
	Status int
	Header http.Header
}

// Do runs the operation id of the client's API (GatewayOperations unless
// Client.Ops is set) with path parameters, extra query parameters (for
// example offset, limit, deviceClasses, attributeScope or
// talqOriginRequestId), a JSON body in (unless nil) and the JSON answer
// read into out (unless nil). Errors are as for CallQ; a redirect is an
// error.
func (c *Client) Do(ctx context.Context, id string, params map[string]string, query url.Values, in, out any) (*Response, error) {
	ops := c.Ops
	if ops == nil {
		ops = GatewayOperations
	}
	op, ok := FindOperation(ops, id)
	if !ok {
		return nil, fmt.Errorf("talq: no operation %q", id)
	}
	path, err := op.Expand(params)
	if err != nil {
		return nil, err
	}
	return c.do(ctx, op.Method, path, query, in, out)
}

// Count runs a count operation (getDevicesCount, getGroupsCount...),
// whose answer is a bare integer in text/plain or JSON.
func (c *Client) Count(ctx context.Context, id string, params map[string]string, query url.Values) (int, error) {
	var raw json.RawMessage
	_, err := c.Do(ctx, id, params, query, nil, &raw)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0, fmt.Errorf("talq: %s answered %q, not a whole number", id, raw)
	}
	return n, nil
}

// Collections are the resources of the gateway API that hold a list of
// objects with the same operations: list, add (a list), update (a list),
// count, and get, update and delete by address. Groups add their member
// operations (AddGroupMembers, DeleteGroupMember, CountGroupMembers).
var Collections = []string{
	"groups", "calendars", "control-programs",
	"lamp-types", "luminaire-types", "driver-types", "controller-types", "bracket-types",
	"control-device-dali-part103-types", "control-gear-dali-part102-types",
	"luminaire-data-dali-part251-types", "energy-data-dali-part252-types",
	"diagnostics-and-maintenance-data-dali-part253-types",
	"luminaire-mounted-control-device-dali-part351-types",
}

// collectionOp finds the operation of the client's API for method on
// "/<collection>" (item false) or "/<collection>/{address}" (item true).
func (c *Client) collectionOp(collection, method, suffix string) (Operation, error) {
	ops := c.Ops
	if ops == nil {
		ops = GatewayOperations
	}
	for _, o := range ops {
		if o.Method != method {
			continue
		}
		rest, ok := strings.CutPrefix(o.Path, "/"+collection)
		if !ok {
			continue
		}
		switch {
		case suffix == "" && rest == "",
			suffix == "/count" && rest == "/count",
			suffix == "/{}" && strings.HasPrefix(rest, "/{") && strings.Count(rest, "/") == 1:
			return o, nil
		}
	}
	return Operation{}, fmt.Errorf("talq: the API has no %s /%s%s", method, collection, strings.ReplaceAll(suffix, "{}", "{address}"))
}

func (c *Client) runCollection(ctx context.Context, collection, method, suffix, address string, in, out any) (*Response, error) {
	op, err := c.collectionOp(collection, method, suffix)
	if err != nil {
		return nil, err
	}
	params := map[string]string{}
	if ps := op.Params(); len(ps) == 1 {
		params[ps[0]] = address
	}
	path, err := op.Expand(params)
	if err != nil {
		return nil, err
	}
	return c.do(ctx, op.Method, path, nil, in, out)
}

// List reads a whole collection (GET /<collection>) into out.
func (c *Client) List(ctx context.Context, collection string, out any) error {
	_, err := c.runCollection(ctx, collection, http.MethodGet, "", "", nil, out)
	return err
}

// CountOf counts a collection (GET /<collection>/count).
func (c *Client) CountOf(ctx context.Context, collection string) (int, error) {
	op, err := c.collectionOp(collection, http.MethodGet, "/count")
	if err != nil {
		return 0, err
	}
	return c.Count(ctx, op.ID, nil, nil)
}

// Get reads one object of a collection (GET /<collection>/{address}).
func (c *Client) Get(ctx context.Context, collection, address string, out any) error {
	_, err := c.runCollection(ctx, collection, http.MethodGet, "/{}", address, nil, out)
	return err
}

// Add creates objects (POST /<collection> with a list); out, unless nil,
// receives the gateway's answer.
func (c *Client) Add(ctx context.Context, collection string, items, out any) (*Response, error) {
	return c.runCollection(ctx, collection, http.MethodPost, "", "", items, out)
}

// UpdateAll replaces objects (PUT /<collection> with a list).
func (c *Client) UpdateAll(ctx context.Context, collection string, items, out any) (*Response, error) {
	return c.runCollection(ctx, collection, http.MethodPut, "", "", items, out)
}

// Update replaces one object (PUT /<collection>/{address}).
func (c *Client) Update(ctx context.Context, collection, address string, item, out any) (*Response, error) {
	return c.runCollection(ctx, collection, http.MethodPut, "/{}", address, item, out)
}

// Delete deletes one object (DELETE /<collection>/{address}).
func (c *Client) Delete(ctx context.Context, collection, address string) (*Response, error) {
	return c.runCollection(ctx, collection, http.MethodDelete, "/{}", address, nil, nil)
}

// Page is the paging a gateway reported for one GET /devices answer.
// Paged is false when it answered without the pagination headers (it does
// not page, and the answer is the whole list).
type Page struct {
	Paged                                   bool
	Count, Offset, TotalCount, DefaultLimit int
}

// PageOf reads the pagination-* headers of an answer.
func PageOf(h http.Header) Page {
	var p Page
	p.TotalCount, p.Paged = headerInt(h, "pagination-totalCount")
	p.Count, _ = headerInt(h, "pagination-count")
	p.Offset, _ = headerInt(h, "pagination-offset")
	p.DefaultLimit, _ = headerInt(h, "pagination-defaultLimit")
	return p
}

// DevicesPage reads one page of GET /devices: offset and limit are sent
// when not negative (limit 0 is sent as given). A gateway that does not
// page answers the whole list and Page.Paged is false.
func (c *Client) DevicesPage(ctx context.Context, deviceClasses []string, offset, limit int) ([]Device, Page, error) {
	q := url.Values{}
	for _, dc := range deviceClasses {
		q.Add("deviceClasses", dc)
	}
	if offset >= 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	if limit >= 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var devs []Device
	h, err := c.CallQ(ctx, http.MethodGet, "/devices", q, nil, &devs)
	if err != nil {
		return nil, Page{}, err
	}
	return devs, PageOf(h), nil
}

// CountDevices counts the devices of the classes (all when empty).
func (c *Client) CountDevices(ctx context.Context, deviceClasses []string) (int, error) {
	q := url.Values{}
	for _, dc := range deviceClasses {
		q.Add("deviceClasses", dc)
	}
	return c.Count(ctx, "getDevicesCount", nil, q)
}

// GetDevice reads one device.
func (c *Client) GetDevice(ctx context.Context, address string, out any) error {
	_, err := c.Do(ctx, "getDevice", map[string]string{"deviceAddress": address}, nil, nil, out)
	return err
}

// GetFunction reads one function of a device; scope, unless empty, keeps
// only the attributes of that attributeScope.
func (c *Client) GetFunction(ctx context.Context, device, function, scope string, out any) error {
	var q url.Values
	if scope != "" {
		q = url.Values{"attributeScope": {scope}}
	}
	_, err := c.Do(ctx, "getDeviceFunction", map[string]string{"deviceAddress": device, "functionId": function}, q, nil, out)
	return err
}

// GetAttribute reads one attribute of a device's function.
func (c *Client) GetAttribute(ctx context.Context, device, function, attribute string, out any) error {
	_, err := c.Do(ctx, "getDeviceAttribute", map[string]string{"deviceAddress": device, "functionId": function, "attributeName": attribute}, nil, nil, out)
	return err
}

// PatchAttribute changes one attribute (a writable configuration
// attribute); the gateway may answer 202 and report the result later.
func (c *Client) PatchAttribute(ctx context.Context, device, function, attribute string, value, out any) (*Response, error) {
	return c.Do(ctx, "partialUpdateAttribute", map[string]string{"deviceAddress": device, "functionId": function, "attributeName": attribute}, nil, value, out)
}

// PatchFunction changes attributes of one function.
func (c *Client) PatchFunction(ctx context.Context, device, function string, value, out any) (*Response, error) {
	return c.Do(ctx, "partialUpdateFunction", map[string]string{"deviceAddress": device, "functionId": function}, nil, value, out)
}

// PatchDevice changes one device (for example its cmsAddress after a
// gateway's bootstrap).
func (c *Client) PatchDevice(ctx context.Context, address string, value, out any) (*Response, error) {
	return c.Do(ctx, "partialUpdateDevice", map[string]string{"deviceAddress": address}, nil, value, out)
}

// Override sends an override command (POST /override-commands).
func (c *Client) Override(ctx context.Context, addresses []Ref, targetCommand map[string]any) (*Response, error) {
	return c.Do(ctx, "sendCommand", nil, nil, map[string]any{"addresses": addresses, "targetCommand": targetCommand}, nil)
}

// Assign assigns an entity (a calendar, for example) to devices or groups
// (POST /assign-commands).
func (c *Client) Assign(ctx context.Context, addresses []Ref, entity Ref, cmsRefID string) (*Response, error) {
	body := map[string]any{"addresses": addresses, "entity": entity}
	if cmsRefID != "" {
		body["cmsRefId"] = cmsRefID
	}
	return c.Do(ctx, "assignCommand", nil, nil, body, nil)
}

// NilUUID is the address a gateway sends for itself when it first
// registers with a CMS, before the CMS has given it one.
const NilUUID = "00000000-0000-0000-0000-000000000000"

// CompatibleVersion returns the lower of two TALQ API versions by
// major.minor (the side with the higher minor version adapts to the
// lower one); a version that does not parse loses.
func CompatibleVersion(a, b string) string {
	pa, oka := versionParts(a)
	pb, okb := versionParts(b)
	switch {
	case !oka:
		return b
	case !okb:
		return a
	case pa[0] != pb[0]:
		if pa[0] < pb[0] {
			return a
		}
		return b
	case pa[1] <= pb[1]:
		return a
	}
	return b
}

func versionParts(v string) ([2]int, bool) {
	maj, min, ok := strings.Cut(MajorMinor(v), ".")
	if !ok {
		return [2]int{}, false
	}
	a, err1 := strconv.Atoi(maj)
	b, err2 := strconv.Atoi(min)
	return [2]int{a, b}, err1 == nil && err2 == nil
}

// MTLSClient returns an HTTP client that presents cert to the peer and
// trusts roots (nil: the system roots), for the mutual TLS that TALQ
// expects between a CMS and a gateway. Put it in Client.HTTP.
func MTLSClient(cert tls.Certificate, roots *x509.CertPool) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}, RootCAs: roots}
	return &http.Client{Transport: tr}
}

// Error keys TALQ recommends for TALQErrorMessage.key.
const (
	KeyHeaderMissing              = "headerMissing"
	KeyHeaderValueNotValid        = "headerValueNotValid"
	KeyParameterMissing           = "parameterMissing"
	KeyParameterValueNotValid     = "parameterValueNotValid"
	KeyPayloadError               = "payloadError"
	KeyResourceNotFound           = "resourceNotFound"
	KeyResourceConflict           = "resourceConflict"
	KeyRelatedResourceNotFound    = "relatedResourceNotFound"
	KeyDeletingResourceIsReferred = "deletingResourceIsReferred"
	KeyServiceConstraintExceeded  = "serviceConstraintExceeded"
	KeyAssignmentEntityNotValid   = "assignmentEntityNotValid"
	KeyAssignmentAddressNotValid  = "assignmentAddressNotValid"
	KeyOverrideCommandNotValid    = "overrideCommandNotValid"
	KeyOverrideAddressNotValid    = "overrideAddressNotValid"
	KeyGenericError               = "genericError"
)

// Compatible reports the version two sides use: the lower minor version
// of the same major version (the side with the higher minor adapts
// down). Different major versions, or a version that does not parse,
// are not compatible.
func Compatible(a, b string) (string, bool) {
	pa, oka := versionParts(a)
	pb, okb := versionParts(b)
	if !oka || !okb || pa[0] != pb[0] {
		return "", false
	}
	if pa[1] <= pb[1] {
		return a, true
	}
	return b, true
}

// CheckGatewayRegistration checks the device list a gateway posts to a
// CMS's /devices when it starts its bootstrap: the gateway's own device
// must carry NilUUID as its address (the CMS assigns the real one). It
// returns 0 and nil when the list is not a registration (no device has
// class "gateway"), and 422 with an error when a gateway device has
// another address.
func CheckGatewayRegistration(devices []Device) (int, *ErrorMessage) {
	for _, d := range devices {
		if d.Class == "gateway" && d.Address != NilUUID {
			return http.StatusUnprocessableEntity, &ErrorMessage{Key: KeyParameterValueNotValid,
				Description: "a registering gateway must send the nil address, not " + d.Address,
				References:  []ErrorReference{{Address: Ref{Resource: "devices", Address: d.Address}}}}
		}
	}
	return 0, nil
}

// WriteBootstrapping answers a gateway's request, other than those of its
// bootstrap, while the bootstrap is not finished: 403.
func WriteBootstrapping(w http.ResponseWriter) {
	WriteError(w, http.StatusForbidden, KeyGenericError, "the bootstrap of this gateway is not finished")
}

// CheckRequest checks what every TALQ request must carry for op: the
// talq-api-version header, the clientAddress parameter and, for
// operations that write, talqRequestId (a UUID, as talqOriginRequestId
// must be when present). It returns nil or the error answer to give:
// status 400 and one TALQErrorMessage.
func CheckRequest(r *http.Request, op Operation) (int, *ErrorMessage) {
	q := r.URL.Query()
	switch {
	case len(r.TransferEncoding) > 0:
		return http.StatusLengthRequired, &ErrorMessage{Key: KeyHeaderMissing, Description: "Content-Length (chunked bodies are not allowed)"}
	case r.Header.Get("talq-api-version") == "":
		return http.StatusBadRequest, &ErrorMessage{Key: KeyHeaderMissing, Description: "talq-api-version"}
	case q.Get("clientAddress") == "":
		return http.StatusBadRequest, &ErrorMessage{Key: KeyParameterMissing, Description: "clientAddress"}
	case !IsUUID(q.Get("clientAddress")):
		return http.StatusBadRequest, &ErrorMessage{Key: KeyParameterValueNotValid, Description: "clientAddress"}
	case op.Writes() && q.Get("talqRequestId") == "":
		return http.StatusBadRequest, &ErrorMessage{Key: KeyParameterMissing, Description: "talqRequestId"}
	case op.Writes() && !IsUUID(q.Get("talqRequestId")):
		return http.StatusBadRequest, &ErrorMessage{Key: KeyParameterValueNotValid, Description: "talqRequestId"}
	case q.Get("talqOriginRequestId") != "" && !IsUUID(q.Get("talqOriginRequestId")):
		return http.StatusBadRequest, &ErrorMessage{Key: KeyParameterValueNotValid, Description: "talqOriginRequestId"}
	}
	return 0, nil
}

// RequestBody reads a request body, undoing a gzip or deflate
// Content-Encoding; another encoding is ErrUnsupportedEncoding (answer
// 415).
func RequestBody(r *http.Request, limit int64) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	var rd io.Reader = io.LimitReader(r.Body, limit)
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		zr, err := gzip.NewReader(rd)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		rd = zr
	case "deflate":
		fr := flate.NewReader(rd)
		defer fr.Close()
		rd = fr
	default:
		return nil, ErrUnsupportedEncoding
	}
	return io.ReadAll(io.LimitReader(rd, limit))
}

// ErrUnsupportedEncoding is RequestBody's error for a Content-Encoding
// other than gzip or deflate.
var ErrUnsupportedEncoding = errors.New("talq: unsupported Content-Encoding")

// Handler serves one matched operation: params are its path parameters
// and body the decoded request body (nil when empty).
type Handler func(w http.ResponseWriter, r *http.Request, op Operation, params map[string]string, body []byte)

// Serve routes the requests below prefix (for example "/talq") to h by the
// operations of ops (GatewayOperations to act as a gateway, CMSOperations
// to act as a CMS). Before calling h it answers, with a TALQ error body,
// 404 for an unknown path, 405 for a known path with another method, 400
// for what CheckRequest refuses and 415 for an unsupported
// Content-Encoding. Every answer carries Cache-Control: no-transform.
func Serve(prefix string, ops []Operation, h Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-transform")
		rest, ok := strings.CutPrefix(r.URL.EscapedPath(), prefix)
		if !ok {
			WriteError(w, http.StatusNotFound, KeyResourceNotFound, r.URL.Path)
			return
		}
		op, params, ok := Match(ops, r.Method, rest)
		if !ok {
			for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
				if _, _, other := Match(ops, m, rest); other {
					WriteError(w, http.StatusMethodNotAllowed, KeyGenericError, r.Method+" "+r.URL.Path)
					return
				}
			}
			WriteError(w, http.StatusNotFound, KeyResourceNotFound, r.URL.Path)
			return
		}
		if status, e := CheckRequest(r, op); e != nil {
			WriteError(w, status, e.Key, e.Description)
			return
		}
		body, err := RequestBody(r, 32<<20)
		if errors.Is(err, ErrUnsupportedEncoding) {
			WriteError(w, http.StatusUnsupportedMediaType, KeyHeaderValueNotValid, "Content-Encoding")
			return
		}
		if err != nil {
			WriteError(w, http.StatusBadRequest, KeyPayloadError, err.Error())
			return
		}
		if len(bytes.TrimSpace(body)) == 0 {
			body = nil
		} else if !json.Valid(body) {
			WriteError(w, http.StatusBadRequest, KeyPayloadError, "the body is not JSON")
			return
		}
		h(w, r, op, params, body)
	})
}
