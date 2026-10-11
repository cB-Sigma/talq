// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package talq

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const (
	testCMS = "6f1c0000-0000-4000-8000-0000000c0001"
	testDev = "6f1c0000-0000-4000-8000-0000000000a1"
)

// The tables hold every operation of the 2.7.1 definitions once, and each
// path template routes back to its own operation, with reserved
// characters in parameters surviving the round trip.
func TestOperationTables(t *testing.T) {
	if len(GatewayOperations) != 129 || len(CMSOperations) != 98 {
		t.Fatalf("operations: gateway %d, cms %d; want 129 and 98", len(GatewayOperations), len(CMSOperations))
	}
	for name, ops := range map[string][]Operation{"gateway": GatewayOperations, "cms": CMSOperations} {
		seen := map[string]bool{}
		for _, op := range ops {
			if seen[op.ID] {
				t.Errorf("%s: %s twice", name, op.ID)
			}
			seen[op.ID] = true
			params := map[string]string{}
			for i, p := range op.Params() {
				params[p] = "a/b c?" + strings.Repeat("x", i)
			}
			path, err := op.Expand(params)
			if err != nil {
				t.Fatal(err)
			}
			got, gotParams, ok := Match(ops, op.Method, path)
			if !ok || got.ID != op.ID {
				t.Errorf("%s: %s %s routes to %q", name, op.Method, path, got.ID)
				continue
			}
			for k, v := range params {
				if gotParams[k] != v {
					t.Errorf("%s: %s param %s = %q, want %q", name, op.ID, k, gotParams[k], v)
				}
			}
			if len(op.Codes) == 0 {
				t.Errorf("%s: %s has no response codes", name, op.ID)
			}
		}
	}
	if op, _, _ := Match(GatewayOperations, http.MethodGet, "/devices/count"); op.ID != "getDevicesCount" {
		t.Errorf("/devices/count routes to %s", op.ID)
	}
	if _, err := (Operation{ID: "x", Path: "/a/{b}"}).Expand(nil); err == nil {
		t.Error("a missing path parameter was accepted")
	}
}

// Every collection has the list, add, bulk update, count, get, update and
// delete operations the collection methods use.
func TestCollectionsHaveTheirOperations(t *testing.T) {
	c := &Client{}
	for _, col := range Collections {
		for _, k := range [][2]string{{"GET", ""}, {"POST", ""}, {"PUT", ""}, {"GET", "/count"}, {"GET", "/{}"}, {"PUT", "/{}"}, {"DELETE", "/{}"}} {
			if _, err := c.collectionOp(col, k[0], k[1]); err != nil {
				t.Error(err)
			}
		}
	}
}

type seen struct {
	method, path, query string
	header              http.Header
	body                []byte
}

func stub(t *testing.T, status int, header http.Header, body string, got *[]seen) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*got = append(*got, seen{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Clone(), b})
		for k, v := range header {
			w.Header()[k] = v
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Do sends the protocol's header and parameters: talq-api-version,
// clientAddress, a fresh talqRequestId UUID on writes only, the caller's
// extra parameters, and escaped path parameters.
func TestDoSendsProtocolParameters(t *testing.T) {
	var got []seen
	srv := stub(t, http.StatusAccepted, nil, "", &got)
	c := &Client{URL: srv.URL + "/talq", CMSAddress: testCMS}
	resp, err := c.Do(context.Background(), "partialUpdateAttribute",
		map[string]string{"deviceAddress": testDev, "functionId": "lamp/1", "attributeName": "dimLevel"},
		url.Values{"talqOriginRequestId": {"5e1b0c8a-7d4f-4a51-9c3e-2f6a8b9c0d1e"}}, map[string]any{"value": 40}, nil)
	if err != nil || resp.Status != http.StatusAccepted {
		t.Fatalf("status %v, err %v", resp, err)
	}
	if err := c.GetAttribute(context.Background(), testDev, "lampActuator", "actualLightState", nil); err != nil {
		t.Fatal(err)
	}
	w, g := got[0], got[1]
	if w.method != http.MethodPatch || w.path != "/talq/devices/"+testDev+"/lamp%2F1/dimLevel" {
		t.Errorf("write: %s %s", w.method, w.path)
	}
	q, _ := url.ParseQuery(w.query)
	if q.Get("clientAddress") != testCMS || !IsUUID(q.Get("talqRequestId")) || q.Get("talqOriginRequestId") == "" {
		t.Errorf("write query %q", w.query)
	}
	if w.header.Get("talq-api-version") != DefaultAPIVersion || w.header.Get("Content-Type") != "application/json" ||
		w.header.Get("Cache-Control") != "no-transform" {
		t.Errorf("write headers %v", w.header)
	}
	if string(w.body) != `{"value":40}` {
		t.Errorf("write body %s", w.body)
	}
	gq, _ := url.ParseQuery(g.query)
	if g.method != http.MethodGet || gq.Has("talqRequestId") || gq.Get("clientAddress") != testCMS {
		t.Errorf("read: %s %q", g.method, g.query)
	}
	if _, err := c.Do(context.Background(), "noSuchOperation", nil, nil, nil, nil); err == nil {
		t.Error("an unknown operation was sent")
	}
}

// Count reads a bare integer in text/plain or JSON.
func TestCount(t *testing.T) {
	for _, ct := range []string{"text/plain", "application/json"} {
		var got []seen
		srv := stub(t, 200, http.Header{"Content-Type": {ct}}, " 7\n", &got)
		c := &Client{URL: srv.URL, CMSAddress: testCMS}
		n, err := c.CountDevices(context.Background(), []string{"streetlightController"})
		if err != nil || n != 7 {
			t.Errorf("%s: %d %v", ct, n, err)
		}
		if got[0].path != "/devices/count" || !strings.Contains(got[0].query, "deviceClasses=streetlightController") {
			t.Errorf("%s: %s?%s", ct, got[0].path, got[0].query)
		}
		if n, err := c.CountOf(context.Background(), "calendars"); err != nil || n != 7 {
			t.Errorf("calendars: %d %v", n, err)
		}
	}
	var got []seen
	srv := stub(t, 200, nil, `"seven"`, &got)
	if _, err := (&Client{URL: srv.URL, CMSAddress: testCMS}).CountOf(context.Background(), "groups"); err == nil {
		t.Error("a count that is not a number was accepted")
	}
}

// Error answers are read with their keys and references; Refused and
// Unsupported tell refusals from outages.
func TestErrorAnswers(t *testing.T) {
	var got []seen
	body := `[{"key":"overrideAddressNotValid","description":"no such device","references":[{"address":{"resource":"devices","address":"` + testDev + `"},"tag":"addresses"}]}]`
	srv := stub(t, http.StatusUnprocessableEntity, nil, body, &got)
	c := &Client{URL: srv.URL, CMSAddress: testCMS}
	_, err := c.Override(context.Background(), []Ref{{Resource: "devices", Address: testDev}}, LevelCommand(10))
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 422 || len(ae.Errors) != 1 || ae.Errors[0].Key != KeyOverrideAddressNotValid ||
		len(ae.Errors[0].References) != 1 || ae.Errors[0].References[0].Address.Address != testDev {
		t.Fatalf("error %#v", err)
	}
	if !Refused(err) || !strings.Contains(err.Error(), "no such device") {
		t.Errorf("Refused(%v) false", err)
	}
	var b map[string]any
	_ = json.Unmarshal(got[0].body, &b)
	if b["addresses"] == nil || b["targetCommand"] == nil {
		t.Errorf("override body %s", got[0].body)
	}
	for status, refused := range map[int]bool{400: true, 404: true, 409: true, 422: true, 501: true, 401: false, 403: false, 429: false, 500: false, 503: false} {
		if Refused(&APIError{Status: status}) != refused {
			t.Errorf("Refused(%d) != %v", status, refused)
		}
	}
	if !Unsupported(&APIError{Status: 404}) || Unsupported(&APIError{Status: 409}) || Unsupported(errors.New("x")) {
		t.Error("Unsupported")
	}
}

// DevicesPage reports the paging headers, or that the gateway does not
// page; ListDevices follows the pages to the end.
func TestDevicesPaging(t *testing.T) {
	var got []seen
	h := http.Header{"Pagination-Count": {"1"}, "Pagination-Offset": {"1"}, "Pagination-Totalcount": {"3"}, "Pagination-Defaultlimit": {"50"}}
	srv := stub(t, 200, h, `[{"address":"`+testDev+`","name":"a","class":"c"}]`, &got)
	c := &Client{URL: srv.URL, CMSAddress: testCMS}
	devs, p, err := c.DevicesPage(context.Background(), nil, 1, 1)
	if err != nil || len(devs) != 1 || p != (Page{Paged: true, Count: 1, Offset: 1, TotalCount: 3, DefaultLimit: 50}) {
		t.Fatalf("%v %+v %v", devs, p, err)
	}
	all, err := c.ListDevices(context.Background(), nil, 1)
	if err != nil || len(all) != 3 {
		t.Errorf("ListDevices: %d %v", len(all), err)
	}
	var got2 []seen
	flat := stub(t, 200, nil, `[{"address":"a"},{"address":"b"}]`, &got2)
	c2 := &Client{URL: flat.URL, CMSAddress: testCMS}
	if devs, p, err := c2.DevicesPage(context.Background(), []string{"x"}, 0, 1); err != nil || p.Paged || len(devs) != 2 {
		t.Errorf("non-paging gateway: %d %+v %v", len(devs), p, err)
	}
	if all, err := c2.ListDevices(context.Background(), nil, 1); err != nil || len(all) != 2 || len(got2) != 2 {
		t.Errorf("non-paging ListDevices: %d requests, %v", len(got2), err)
	}
}

// The collection methods use each collection's own operations.
func TestCollectionMethods(t *testing.T) {
	var got []seen
	srv := stub(t, 200, nil, `[]`, &got)
	c := &Client{URL: srv.URL, CMSAddress: testCMS}
	ctx := context.Background()
	addr := "3d2f7c10-5b8e-4f61-8a2d-1c9e0b7a6f51"
	_ = c.List(ctx, "luminaire-types", nil)
	_, _ = c.Add(ctx, "control-gear-dali-part102-types", []any{}, nil)
	_, _ = c.UpdateAll(ctx, "groups", []any{}, nil)
	_ = c.Get(ctx, "calendars", addr, nil)
	_, _ = c.Update(ctx, "control-programs", addr, map[string]any{}, nil)
	_, _ = c.Delete(ctx, "bracket-types", addr)
	want := []string{"GET /luminaire-types", "POST /control-gear-dali-part102-types", "PUT /groups",
		"GET /calendars/" + addr, "PUT /control-programs/" + addr, "DELETE /bracket-types/" + addr}
	for i, w := range want {
		if i >= len(got) || got[i].method+" "+got[i].path != w {
			t.Errorf("request %d: want %s", i, w)
		}
	}
	if err := c.List(ctx, "no-such-things", nil); err == nil {
		t.Error("an unknown collection was sent")
	}
	cms := &Client{URL: srv.URL, CMSAddress: testCMS, Ops: CMSOperations}
	if _, err := cms.Do(ctx, "sendLogReport", nil, nil, map[string]any{}, nil); err != nil || got[len(got)-1].path != "/log-reports" {
		t.Errorf("CMS operation: %v", err)
	}
	if err := cms.List(ctx, "calendars", nil); err == nil {
		t.Error("the CMS API has no calendars, but List sent one")
	}
}

func serveFor(t *testing.T) (*httptest.Server, *[]string) {
	var calls []string
	h := Serve("/talq", GatewayOperations, func(w http.ResponseWriter, r *http.Request, op Operation, params map[string]string, body []byte) {
		calls = append(calls, op.ID+" "+params["deviceAddress"]+" "+string(body))
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, &calls
}

// Serve checks every request before the handler sees it.
func TestServeChecksRequests(t *testing.T) {
	srv, calls := serveFor(t)
	good := "?clientAddress=" + testCMS + "&talqRequestId=5e1b0c8a-7d4f-4a51-9c3e-2f6a8b9c0d1e"
	cases := []struct {
		method, path, enc, version string
		body                       []byte
		status                     int
		key                        string
	}{
		{"GET", "/talq/nothing" + good, "", "2.7.1", nil, 404, KeyResourceNotFound},
		{"POST", "/talq/devices/count" + good, "", "2.7.1", nil, 405, KeyGenericError},
		{"GET", "/talq/devices" + good, "", "", nil, 400, KeyHeaderMissing},
		{"GET", "/talq/devices?clientAddress=cms", "", "2.7.1", nil, 400, KeyParameterValueNotValid},
		{"GET", "/talq/devices", "", "2.7.1", nil, 400, KeyParameterMissing},
		{"POST", "/talq/override-commands?clientAddress=" + testCMS, "", "2.7.1", []byte("{}"), 400, KeyParameterMissing},
		{"POST", "/talq/override-commands" + good, "br", "2.7.1", []byte("{}"), 415, KeyHeaderValueNotValid},
		{"POST", "/talq/override-commands" + good, "", "2.7.1", []byte("{"), 400, KeyPayloadError},
		{"GET", "/elsewhere" + good, "", "2.7.1", nil, 404, KeyResourceNotFound},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(tc.method, srv.URL+tc.path, bytes.NewReader(tc.body))
		if tc.version != "" {
			req.Header.Set("talq-api-version", tc.version)
		}
		if tc.enc != "" {
			req.Header.Set("Content-Encoding", tc.enc)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var msgs []ErrorMessage
		_ = json.NewDecoder(resp.Body).Decode(&msgs)
		resp.Body.Close()
		if resp.StatusCode != tc.status || len(msgs) != 1 || msgs[0].Key != tc.key || resp.Header.Get("Cache-Control") != "no-transform" {
			t.Errorf("%s %s: %d %v", tc.method, tc.path, resp.StatusCode, msgs)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("the handler saw refused requests: %v", *calls)
	}
	// A gzip body is accepted and decoded; path parameters arrive unescaped.
	var zb bytes.Buffer
	zw := gzip.NewWriter(&zb)
	_, _ = zw.Write([]byte(`{"a":1}`))
	_ = zw.Close()
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/talq/devices/a%2Fb"+good, &zb)
	req.Header.Set("talq-api-version", "2.7.1")
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || len(*calls) != 1 || (*calls)[0] != `partialUpdateDevice a/b {"a":1}` {
		t.Errorf("gzip PATCH: %d %v", resp.StatusCode, *calls)
	}
}

func TestCompatibleAndRegistration(t *testing.T) {
	if v, ok := Compatible("2.7.1", "2.6.3"); !ok || v != "2.6.3" {
		t.Errorf("Compatible 2.7.1/2.6.3 = %q %v", v, ok)
	}
	if _, ok := Compatible("3.0.0", "2.7.1"); ok {
		t.Error("different majors were compatible")
	}
	if s, e := CheckGatewayRegistration([]Device{{Address: NilUUID, Class: "gateway"}}); e != nil || s != 0 {
		t.Errorf("nil address refused: %v", e)
	}
	if s, e := CheckGatewayRegistration([]Device{{Address: testDev, Class: "gateway"}}); e == nil || s != 422 {
		t.Error("a registering gateway with an address was accepted")
	}
	rec := httptest.NewRecorder()
	WriteBootstrapping(rec)
	if rec.Code != 403 {
		t.Errorf("bootstrapping: %d", rec.Code)
	}
}

// A chunked body is refused (TALQ wants Content-Length), and a write
// that a redirect turned into a GET is an error, not a success.
func TestChunkedAndRedirectedWrites(t *testing.T) {
	srv, calls := serveFor(t)
	pr, pw := io.Pipe()
	go func() { _, _ = pw.Write([]byte("{}")); _ = pw.Close() }()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/talq/override-commands?clientAddress="+testCMS+"&talqRequestId=5e1b0c8a-7d4f-4a51-9c3e-2f6a8b9c0d1e", pr)
	req.Header.Set("talq-api-version", "2.7.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusLengthRequired || len(*calls) != 0 {
		t.Errorf("chunked: %d", resp.StatusCode)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/old/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/landing", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/landing", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	rs := httptest.NewServer(mux)
	defer rs.Close()
	c := &Client{URL: rs.URL + "/old", CMSAddress: testCMS}
	if _, err := c.Override(context.Background(), []Ref{{Resource: "devices", Address: testDev}}, LevelCommand(1)); err == nil || !strings.Contains(err.Error(), "nothing was written") {
		t.Errorf("redirected write: %v", err)
	}
}

func TestCompatibleVersion(t *testing.T) {
	for _, tc := range [][3]string{{"2.7.1", "2.6.3", "2.6.3"}, {"2.6", "2.7.0", "2.6"}, {"3.0.0", "2.7.1", "2.7.1"}, {"x", "2.7.1", "2.7.1"}, {"2.7.1", "", "2.7.1"}} {
		if got := CompatibleVersion(tc[0], tc[1]); got != tc[2] {
			t.Errorf("CompatibleVersion(%q, %q) = %q, want %q", tc[0], tc[1], got, tc[2])
		}
	}
}
