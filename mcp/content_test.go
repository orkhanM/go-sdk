// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package mcp_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/orkhanm/go-sdk/mcp"
)

func TestContent(t *testing.T) {
	tests := []struct {
		in   mcp.Content
		want string // json serialization
	}{
		{
			&mcp.TextContent{Text: "hello"},
			`{"type":"text","text":"hello"}`,
		},
		{
			&mcp.TextContent{Text: ""},
			`{"type":"text","text":""}`,
		},
		{
			&mcp.TextContent{},
			`{"type":"text","text":""}`,
		},
		{
			&mcp.TextContent{
				Text:        "hello",
				Meta:        mcp.Meta{"key": "value"},
				Annotations: &mcp.Annotations{Priority: 1.0},
			},
			`{"type":"text","text":"hello","_meta":{"key":"value"},"annotations":{"priority":1}}`,
		},
		{
			&mcp.ImageContent{
				Data:     []byte("a1b2c3"),
				MIMEType: "image/png",
			},
			`{"type":"image","mimeType":"image/png","data":"YTFiMmMz"}`,
		},
		{
			&mcp.ImageContent{MIMEType: "image/png", Data: []byte{}},
			`{"type":"image","mimeType":"image/png","data":""}`,
		},
		{
			&mcp.ImageContent{Data: []byte("test")},
			`{"type":"image","mimeType":"","data":"dGVzdA=="}`,
		},
		{
			&mcp.ImageContent{
				Data:        []byte("a1b2c3"),
				MIMEType:    "image/png",
				Meta:        mcp.Meta{"key": "value"},
				Annotations: &mcp.Annotations{Priority: 1.0},
			},
			`{"type":"image","mimeType":"image/png","data":"YTFiMmMz","_meta":{"key":"value"},"annotations":{"priority":1}}`,
		},
		{
			&mcp.AudioContent{
				Data:     []byte("a1b2c3"),
				MIMEType: "audio/wav",
			},
			`{"type":"audio","mimeType":"audio/wav","data":"YTFiMmMz"}`,
		},
		{
			&mcp.AudioContent{MIMEType: "audio/wav", Data: []byte{}},
			`{"type":"audio","mimeType":"audio/wav","data":""}`,
		},
		{
			&mcp.AudioContent{Data: []byte("test")},
			`{"type":"audio","mimeType":"","data":"dGVzdA=="}`,
		},
		{
			&mcp.AudioContent{
				Data:        []byte("a1b2c3"),
				MIMEType:    "audio/wav",
				Meta:        mcp.Meta{"key": "value"},
				Annotations: &mcp.Annotations{Priority: 1.0},
			},
			`{"type":"audio","mimeType":"audio/wav","data":"YTFiMmMz","_meta":{"key":"value"},"annotations":{"priority":1}}`,
		},
		{
			&mcp.EmbeddedResource{
				Resource: &mcp.ResourceContents{URI: "file://foo", MIMEType: "text", Text: "abc"},
			},
			`{"type":"resource","resource":{"uri":"file://foo","mimeType":"text","text":"abc"}}`,
		},
		{
			&mcp.EmbeddedResource{
				Resource: &mcp.ResourceContents{URI: "file://foo", MIMEType: "image/png", Blob: []byte("a1b2c3")},
			},
			`{"type":"resource","resource":{"uri":"file://foo","mimeType":"image/png","blob":"YTFiMmMz"}}`,
		},
		{
			&mcp.EmbeddedResource{
				Resource:    &mcp.ResourceContents{URI: "file://foo", MIMEType: "text", Text: "abc"},
				Meta:        mcp.Meta{"key": "value"},
				Annotations: &mcp.Annotations{Priority: 1.0},
			},
			`{"type":"resource","resource":{"uri":"file://foo","mimeType":"text","text":"abc"},"_meta":{"key":"value"},"annotations":{"priority":1}}`,
		},
		{
			&mcp.ResourceLink{
				URI:  "file:///path/to/file.txt",
				Name: "file.txt",
			},
			`{"type":"resource_link","uri":"file:///path/to/file.txt","name":"file.txt"}`,
		},
		{
			&mcp.ResourceLink{
				URI:         "https://example.com/resource",
				Name:        "Example Resource",
				Title:       "A comprehensive example resource",
				Description: "This resource demonstrates all fields",
				MIMEType:    "text/plain",
				Meta:        mcp.Meta{"custom": "metadata"},
			},
			`{"type":"resource_link","mimeType":"text/plain","uri":"https://example.com/resource","name":"Example Resource","title":"A comprehensive example resource","description":"This resource demonstrates all fields","_meta":{"custom":"metadata"}}`,
		},
	}

	for _, test := range tests {
		got, err := json.Marshal(test.in)
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(test.want, string(got)); diff != "" {
			t.Errorf("json.Marshal(%v) mismatch (-want +got):\n%s", test.in, diff)
		}
		result := fmt.Sprintf(`{"content":[%s]}`, string(got))
		var out mcp.CallToolResult
		if err := json.Unmarshal([]byte(result), &out); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(test.in, out.Content[0]); diff != "" {
			t.Errorf("json.Unmarshal(%q) mismatch (-want +got):\n%s", string(got), diff)
		}
	}
}

func TestEmbeddedResource(t *testing.T) {
	for _, tt := range []struct {
		rc   *mcp.ResourceContents
		want string // marshaled JSON
	}{
		{
			&mcp.ResourceContents{URI: "u", Text: "t"},
			`{"uri":"u","text":"t"}`,
		},
		{
			&mcp.ResourceContents{URI: "u", MIMEType: "m", Text: "t", Meta: mcp.Meta{"key": "value"}},
			`{"uri":"u","mimeType":"m","text":"t","_meta":{"key":"value"}}`,
		},
		{
			&mcp.ResourceContents{URI: "u"},
			`{"uri":"u"}`,
		},
		{
			&mcp.ResourceContents{URI: "u", Blob: []byte{}},
			`{"uri":"u","blob":""}`,
		},
		{
			&mcp.ResourceContents{URI: "u", Blob: []byte{1}},
			`{"uri":"u","blob":"AQ=="}`,
		},
		{
			&mcp.ResourceContents{URI: "u", MIMEType: "m", Blob: []byte{1}, Meta: mcp.Meta{"key": "value"}},
			`{"uri":"u","mimeType":"m","blob":"AQ==","_meta":{"key":"value"}}`,
		},
	} {
		data, err := json.Marshal(tt.rc)
		if err != nil {
			t.Fatal(err)
		}
		if got := string(data); got != tt.want {
			t.Errorf("%#v:\ngot  %s\nwant %s", tt.rc, got, tt.want)
		}
		urc := new(mcp.ResourceContents)
		if err := json.Unmarshal(data, urc); err != nil {
			t.Fatal(err)
		}
		// Since Blob is omitempty, the empty slice changes to nil.
		if diff := cmp.Diff(tt.rc, urc); diff != "" {
			t.Errorf("mismatch (-want, +got):\n%s", diff)
		}
	}
}
