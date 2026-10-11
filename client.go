// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package talq

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// DefaultAPIVersion is the TALQ API version sent in talq-api-version.
const DefaultAPIVersion = "2.7.1"

// Client calls one gateway's API as a CMS.
type Client struct {
	// URL is the gateway API base ("https://gw.example/talq").
	URL string
	// CMSAddress is the CMS's TALQ address, sent as clientAddress.
	CMSAddress string
	// APIVersion is sent in talq-api-version; empty is DefaultAPIVersion.
	APIVersion string
	// BearerToken, or Username and Password, authenticate the CMS.
	BearerToken        string
	Username, Password string
	// HTTP is the client used; nil is http.DefaultClient. For the mutual
	// TLS TALQ expects, see MTLSClient.
	HTTP *http.Client
	// Ops are the operations Do and the collection methods look up: nil is
	// GatewayOperations (a CMS calling a gateway); a gateway calling a CMS
	// sets CMSOperations (CMSAddress is then the gateway's address).
	Ops []Operation
}

// APIError is a TALQ error answer.
type APIError struct {
	Status int
	Errors []ErrorMessage
}

// ErrorMessage is a TALQErrorMessage.
type ErrorMessage struct {
	Key         string `json:"key"`
	Description string `json:"description,omitempty"`
	// References name the resources the error is about.
	References []ErrorReference `json:"references,omitempty"`
}

// ErrorReference is one entry of TALQErrorMessage.references.
type ErrorReference struct {
	Address Ref    `json:"address"`
	Tag     string `json:"tag,omitempty"`
}

func (e *APIError) Error() string {
	s := fmt.Sprintf("the gateway answered %d", e.Status)
	for _, m := range e.Errors {
		s += "; " + m.Key
		if m.Description != "" {
			s += ": " + m.Description
		}
	}
	return s
}

// RandReader is the source of request IDs (tests may replace it).
var RandReader io.Reader = crand.Reader

// NewRequestID is a random (version 4) UUID for talqRequestId.
func NewRequestID() string {
	var b [16]byte
	_, _ = io.ReadFull(RandReader, b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Refused reports a gateway's answer that it does not support or accept a
// request (as opposed to an outage or a credentials problem).
func Refused(err error) bool {
	var ae *APIError
	if !errors.As(err, &ae) {
		return false
	}
	switch ae.Status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusConflict,
		http.StatusUnprocessableEntity, http.StatusNotImplemented:
		return true
	}
	return false
}

// Unsupported reports a gateway answer saying a collection is not served.
func Unsupported(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && (ae.Status == http.StatusNotFound || ae.Status == http.StatusBadRequest ||
		ae.Status == http.StatusMethodNotAllowed || ae.Status == http.StatusNotImplemented)
}

// Call sends a request with a JSON body (in, unless nil) and reads a JSON
// answer into out (unless nil).
func (c *Client) Call(ctx context.Context, method, path string, in, out any) error {
	_, err := c.CallQ(ctx, method, path, nil, in, out)
	return err
}

// CallQ is Call with extra query parameters; it returns the answer's
// headers. Requests other than GET carry a new talqRequestId. A redirect
// is an error (nothing was written); an error status is an *APIError.
func (c *Client) CallQ(ctx context.Context, method, path string, extra url.Values, in, out any) (http.Header, error) {
	resp, err := c.do(ctx, method, path, extra, in, out)
	if resp == nil {
		return nil, err
	}
	return resp.Header, err
}

func (c *Client) do(ctx context.Context, method, path string, extra url.Values, in, out any) (*Response, error) {
	q := url.Values{"clientAddress": {c.CMSAddress}}
	for k, v := range extra {
		q[k] = v
	}
	if method != http.MethodGet {
		q.Set("talqRequestId", NewRequestID())
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL+path+"?"+q.Encode(), body)
	if err != nil {
		return nil, err
	}
	version := c.APIVersion
	if version == "" {
		version = DefaultAPIVersion
	}
	req.Header.Set("talq-api-version", version)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-transform")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch {
	case c.BearerToken != "":
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	case c.Username != "":
		req.SetBasicAuth(c.Username, c.Password)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	r := &Response{Status: resp.StatusCode, Header: resp.Header}
	if resp.Request != nil && resp.Request.Method != method {
		return r, fmt.Errorf("the gateway redirected %s %s and the redirect turned it into %s %s (answered %d): nothing was written; set the gateway URL to where it moved",
			method, path, resp.Request.Method, resp.Request.URL.Path, resp.StatusCode)
	}
	if resp.StatusCode/100 == 3 {
		return r, fmt.Errorf("the gateway answered %s %d and moved to %q: nothing was written; set the gateway URL to where it moved", method, resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp.StatusCode/100 != 2 {
		ae := &APIError{Status: resp.StatusCode}
		_ = json.Unmarshal(b, &ae.Errors)
		return r, ae
	}
	if out != nil && len(bytes.TrimSpace(b)) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			return r, fmt.Errorf("the gateway's answer is not the expected JSON: %w", err)
		}
	}
	return r, nil
}

func headerInt(h http.Header, k string) (int, bool) {
	v := h.Get(k)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}

// ListDevices reads GET /devices of the device classes (all when empty)
// page by page (offset, limit) while the gateway pages (it answers
// pagination-totalCount); a gateway that does not page answers the whole
// list to the first request. A negative pageSize asks for the whole list
// at once.
func (c *Client) ListDevices(ctx context.Context, deviceClasses []string, pageSize int) ([]Device, error) {
	q := url.Values{}
	for _, dc := range deviceClasses {
		q.Add("deviceClasses", dc)
	}
	if pageSize < 0 {
		var devs []Device
		_, err := c.CallQ(ctx, http.MethodGet, "/devices", q, nil, &devs)
		return devs, err
	}
	var all []Device
	offset := 0
	for range 100000 {
		q.Set("offset", strconv.Itoa(offset))
		q.Set("limit", strconv.Itoa(pageSize))
		var devs []Device
		h, err := c.CallQ(ctx, http.MethodGet, "/devices", q, nil, &devs)
		if err != nil {
			return nil, err
		}
		all = append(all, devs...)
		total, paged := headerInt(h, "pagination-totalCount")
		if !paged {
			return all, nil
		}
		n := len(devs)
		if cnt, ok := headerInt(h, "pagination-count"); ok && cnt < n {
			n = cnt
		}
		offset += n
		if n == 0 || offset >= total {
			return all, nil
		}
	}
	return nil, errors.New("the gateway's device list did not end")
}
